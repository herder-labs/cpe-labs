package subscription_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cwmp/scheduler"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/subscription"
)

const evaluatorProfile = `
parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "ACME"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "Generic"
  - path: Device.DeviceInfo.SerialNumber
    value: "SN1"
  - path: Device.WiFi.Radio.1.Channel
    type: xsd:unsignedInt
    value: "6"
    writable: true

usp:
  enable: true
  broker:
    address: nats
`

func TestEvaluatorPicksUpDefaultSeedBootEventSubscription(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	e.FireEvent("Device.Boot!")

	msg := adapter.awaitSend(t, 200*time.Millisecond)
	notif := msg.GetBody().GetRequest().GetNotify()
	if notif.GetSubscriptionId() != "default-boot-event-ACS" {
		t.Errorf("SubscriptionId=%q want default-boot-event-ACS", notif.GetSubscriptionId())
	}
	if notif.GetEvent().GetEventName() != "Boot!" {
		t.Errorf("EventName=%q want Boot!", notif.GetEvent().GetEventName())
	}
}

func TestEvaluatorValueChangeSubscriptionFires(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Override the default-seed row to watch a ValueChange path.
	setSubField(t, tree, 1, "NotifType", "ValueChange")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "11")

	msg := adapter.awaitSend(t, 200*time.Millisecond)
	vc := msg.GetBody().GetRequest().GetNotify().GetValueChange()
	if vc == nil {
		t.Fatalf("expected ValueChange Notify, got %+v", msg)
	}
	if vc.GetParamPath() != "Device.WiFi.Radio.1.Channel" || vc.GetParamValue() != "11" {
		t.Errorf("unexpected ValueChange payload: %+v", vc)
	}
}

func TestEvaluatorObjectCreationSuppressedWhenNoMatch(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Default seed is a Boot! Event sub, not ObjectCreation. Add on
	// Device.WiFi.SSID. should not fire any notify.
	e.NotifyObjectCreated("Device.WiFi.SSID.1.", map[string]string{"SSID": "x"})
	time.Sleep(60 * time.Millisecond)
	if got := adapter.snapshot(); len(got) != 0 {
		t.Errorf("expected no notifies (no matching subscription), got %d", len(got))
	}
}

func TestEvaluatorObjectCreationFiresWhenSubscriptionInstalled(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	setSubField(t, tree, 1, "NotifType", "ObjectCreation")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.SSID.")
	awaitRescan()

	e.NotifyObjectCreated("Device.WiFi.SSID.42.", map[string]string{"SSID": "Home"})
	msg := adapter.awaitSend(t, 200*time.Millisecond)
	oc := msg.GetBody().GetRequest().GetNotify().GetObjCreation()
	if oc == nil {
		t.Fatalf("expected ObjectCreation Notify")
	}
	if oc.GetObjPath() != "Device.WiFi.SSID.42." {
		t.Errorf("ObjPath=%q", oc.GetObjPath())
	}
}

func TestEvaluatorObjectDeletionFiresWhenSubscriptionInstalled(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	setSubField(t, tree, 1, "NotifType", "ObjectDeletion")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.SSID.")
	awaitRescan()

	e.NotifyObjectDeleted("Device.WiFi.SSID.42.")
	msg := adapter.awaitSend(t, 200*time.Millisecond)
	od := msg.GetBody().GetRequest().GetNotify().GetObjDeletion()
	if od == nil {
		t.Fatalf("expected ObjectDeletion Notify")
	}
}

// newPeriodicTestEvaluator returns an evaluator wired to a real
// scheduler.Scheduler so Periodic tests exercise the real
// cancel-and-rearm path. The scheduler is Start()ed and Stop()ped
// via t.Cleanup.
func TestEvaluatorRuntimeAddOnSubscriptionTable(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Add a new Subscription row at runtime (controller-driven Add).
	inst, err := tree.AddObject("Device.LocalAgent.Subscription")
	if err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	// Populate it before the rescan window closes.
	setSubField(t, tree, inst, "Enable", "true")
	setSubField(t, tree, inst, "ID", "runtime-sub-vc")
	setSubField(t, tree, inst, "NotifType", "ValueChange")
	setSubField(t, tree, inst, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	// The new row should now be active. A ValueChange on the watched
	// leaf must fire a Notify with the new SubscriptionID.
	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "11")
	msg := adapter.awaitSend(t, 200*time.Millisecond)
	if got := msg.GetBody().GetRequest().GetNotify().GetSubscriptionId(); got != "runtime-sub-vc" {
		t.Errorf("SubscriptionId = %q, want runtime-sub-vc", got)
	}
}

func TestEvaluatorRuntimeSetDisablesRow(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Reconfigure row 1 to ValueChange + watch a known leaf.
	setSubField(t, tree, 1, "NotifType", "ValueChange")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	// Confirm baseline: Notify fires.
	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "11")
	_ = adapter.awaitSend(t, 200*time.Millisecond)
	baselineCount := len(adapter.snapshot())

	// Disable the row.
	setSubField(t, tree, 1, "Enable", "false")
	awaitRescan()

	// Notify must not emit anything new.
	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "12")
	time.Sleep(80 * time.Millisecond)
	if got := len(adapter.snapshot()); got != baselineCount {
		t.Errorf("got %d notifies after disable, want %d (no new emits)", got, baselineCount)
	}
}

func TestEvaluatorRuntimeEnableToggle(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	setSubField(t, tree, 1, "NotifType", "ValueChange")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	for i, want := range []struct {
		enable string
		emits  bool
	}{
		{"true", true},
		{"false", false},
		{"true", true},
		{"false", false},
	} {
		setSubField(t, tree, 1, "Enable", want.enable)
		awaitRescan()

		before := len(adapter.snapshot())
		e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "11")
		time.Sleep(80 * time.Millisecond)
		after := len(adapter.snapshot())

		if want.emits {
			if after <= before {
				t.Errorf("toggle %d (enable=%s): expected emit, got %d → %d", i, want.enable, before, after)
			}
		} else {
			if after != before {
				t.Errorf("toggle %d (enable=%s): expected no emit, got %d → %d", i, want.enable, before, after)
			}
		}
	}
}

func TestEvaluatorRuntimeDeleteRow(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Add a runtime ValueChange row, confirm it works, then Delete.
	inst, err := tree.AddObject("Device.LocalAgent.Subscription")
	if err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	setSubField(t, tree, inst, "Enable", "true")
	setSubField(t, tree, inst, "ID", "to-delete")
	setSubField(t, tree, inst, "NotifType", "ValueChange")
	setSubField(t, tree, inst, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "11")
	_ = adapter.awaitSend(t, 200*time.Millisecond)
	baseline := len(adapter.snapshot())

	// Delete the row.
	if derr := tree.DeleteObject("Device.LocalAgent.Subscription." + itoa(inst)); derr != nil {
		t.Fatalf("DeleteObject: %v", derr)
	}
	awaitRescan()

	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "12")
	time.Sleep(80 * time.Millisecond)
	if got := len(adapter.snapshot()); got != baseline {
		t.Errorf("got %d notifies after Delete, want %d (no new emits)", got, baseline)
	}
}

// newPeriodicTestEvaluator returns an evaluator wired to a real
// scheduler.Scheduler so Periodic tests exercise the real
// cancel-and-rearm path. The scheduler is Start()ed and Stop()ped
// via t.Cleanup.
func newPeriodicTestEvaluator(t *testing.T, tree *paramtree.Tree, adapter *fakeAdapter) *subscription.Evaluator {
	t.Helper()
	sched := scheduler.NewScheduler(scheduler.Options{Logger: silentLogger()})
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sched.Stop(ctx)
	})
	return subscription.New(tree, adapter, "os::A", "self::herder", sched, "cpe-1", nil, silentLogger())
}

func TestEvaluatorRuntimeDeletePeriodicCancelsTimer(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := newPeriodicTestEvaluator(t, tree, adapter)
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Promote row 1 to Periodic with a 60s period (real time; we are
	// not waiting for a tick here, just asserting cancel semantics).
	setSubField(t, tree, 1, "NotifType", "Periodic")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	setSubField(t, tree, 1, "Period", "60")
	awaitRescan()

	armed := e.PeriodicArmedPeriods()
	if armed["default-boot-event-ACS"] != 60 {
		t.Fatalf("expected default-boot-event-ACS armed at 60s, got %+v", armed)
	}

	// Delete row 1. The OnWrite hook should trigger rescan; the
	// rescan should observe the row gone and cancel the timer.
	if err := tree.DeleteObject("Device.LocalAgent.Subscription.1"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	awaitRescan()

	if got := e.PeriodicArmedPeriods(); len(got) != 0 {
		t.Errorf("expected no armed periodics after Delete, got %+v", got)
	}
}

func TestEvaluatorRuntimePeriodChange(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := newPeriodicTestEvaluator(t, tree, adapter)
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	setSubField(t, tree, 1, "NotifType", "Periodic")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	setSubField(t, tree, 1, "Period", "60")
	awaitRescan()
	if got := e.PeriodicArmedPeriods()["default-boot-event-ACS"]; got != 60 {
		t.Fatalf("initial period got %d, want 60", got)
	}

	// Change Period; rescan must cancel old timer and re-arm at new period.
	setSubField(t, tree, 1, "Period", "5")
	awaitRescan()
	if got := e.PeriodicArmedPeriods()["default-boot-event-ACS"]; got != 5 {
		t.Errorf("after Period change got %d, want 5", got)
	}

	// Idempotent: setting the same Period again must NOT re-arm.
	beforeRebuild := e.RebuildCount()
	setSubField(t, tree, 1, "Period", "5")
	awaitRescan()
	afterRebuild := e.RebuildCount()
	if afterRebuild <= beforeRebuild {
		t.Errorf("expected at least one rebuild from the write, got delta %d", afterRebuild-beforeRebuild)
	}
	if got := e.PeriodicArmedPeriods()["default-boot-event-ACS"]; got != 5 {
		t.Errorf("after idempotent re-set got %d, want 5", got)
	}
}

func TestEvaluatorRuntimeNotifTypeChange(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := newPeriodicTestEvaluator(t, tree, adapter)
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	setSubField(t, tree, 1, "NotifType", "Periodic")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	setSubField(t, tree, 1, "Period", "60")
	awaitRescan()
	if got := e.PeriodicArmedPeriods()["default-boot-event-ACS"]; got != 60 {
		t.Fatalf("expected periodic armed, got %+v", e.PeriodicArmedPeriods())
	}

	// Flip to ValueChange. Periodic timer must cancel; ValueChange
	// index must include the row.
	setSubField(t, tree, 1, "NotifType", "ValueChange")
	awaitRescan()

	if got := e.PeriodicArmedPeriods(); len(got) != 0 {
		t.Errorf("expected no armed periodics after NotifType change, got %+v", got)
	}

	// Now a ValueChange on the watched leaf should emit a Notify.
	e.NotifyValueChange("Device.WiFi.Radio.1.Channel", "11")
	msg := adapter.awaitSend(t, 200*time.Millisecond)
	if msg.GetBody().GetRequest().GetNotify().GetValueChange() == nil {
		t.Errorf("expected ValueChange Notify after NotifType flip")
	}
}

func TestEvaluatorOnWriteHookNotDuplicatedOnRestart(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	if err := e.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	defer e.Stop(context.Background())

	// Baseline: two rebuilds from the two Starts.
	baseline := e.RebuildCount()
	if baseline < 2 {
		t.Fatalf("expected at least 2 rebuilds (one per Start), got %d", baseline)
	}

	// One synthetic write to a Subscription leaf should produce
	// exactly one additional rebuild. If the hook is registered twice,
	// we'd see two debounced rescans collapsed to one (the rescanCh is
	// buffered length 1, so duplicates coalesce). The functional check
	// is therefore "exactly one more rebuild", which holds whether the
	// hook is registered once or many times — but the cost guarantee
	// (no callback-slice growth across Stop/Start) is what we care
	// about. Verify via the rebuild count: one more, not more.
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	delta := e.RebuildCount() - baseline
	if delta != 1 {
		t.Errorf("expected exactly 1 rebuild after one write, got %d (baseline=%d)", delta, baseline)
	}
}

func TestEvaluatorStopDisablesHook(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First write while enabled: should rebuild.
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()
	afterFirst := e.RebuildCount()

	// Stop the evaluator. Hook stays registered (no unregister API)
	// but the enabled gate should suppress its body.
	if err := e.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Second write while disabled: should NOT rebuild.
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.SSID.1.SSID")
	awaitRescan()

	if got := e.RebuildCount(); got != afterFirst {
		t.Errorf("rebuild count moved while stopped: was %d, now %d", afterFirst, got)
	}
}

func TestEvaluatorMalformedReferenceListLogged(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	var logBuf syncBuffer
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, capturingLogger(&logBuf, slog.LevelWarn))
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Default seed row 1 is Event NotifType. Clear its ReferenceList
	// and assert a warn-log fires on the next rescan.
	setSubField(t, tree, 1, "ReferenceList", "")
	awaitRescan()

	got := logBuf.String()
	if !strings.Contains(got, "usp Subscription row skipped: empty ReferenceList") {
		t.Errorf("expected warn log, got: %q", got)
	}
}

func TestEvaluatorReferenceListEmptyForPeriodicIsAllowed(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	var logBuf syncBuffer
	e := subscription.New(tree, adapter, "os::A", "self::herder", nil, "cpe-1", nil, capturingLogger(&logBuf, slog.LevelWarn))
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(context.Background())

	// Periodic + empty ReferenceList is valid (no value-change paths
	// to scan; the interval alone drives the cadence). No warn.
	setSubField(t, tree, 1, "NotifType", "Periodic")
	setSubField(t, tree, 1, "ReferenceList", "")
	awaitRescan()

	if got := logBuf.String(); strings.Contains(got, "empty ReferenceList") {
		t.Errorf("did not expect warn log for Periodic+empty RefList, got: %q", got)
	}
}

func loadEvaluatorTree(t *testing.T) *paramtree.Tree {
	t.Helper()
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(evaluatorProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	return p.Tree
}

func setSubField(t *testing.T, tree *paramtree.Tree, row int, field, value string) {
	t.Helper()
	path := "Device.LocalAgent.Subscription." + itoa(row) + "." + field
	v, _ := tree.Get(path)
	if err := tree.Set(path, paramtree.Value{Type: v.Type, Raw: value, Writable: true}); err != nil {
		t.Fatalf("Set %s: %v", path, err)
	}
}

func awaitRescan() {
	time.Sleep(100 * time.Millisecond)
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// syncBuffer is a goroutine-safe wrapper around bytes.Buffer for
// capturing slog output from the rescan goroutine while the test
// reads it. bytes.Buffer is NOT safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// capturingLogger returns a Logger writing to the supplied buffer at
// the given minimum level. Used by tests that assert specific
// warn-level messages were emitted.
func capturingLogger(buf *syncBuffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}))
}

type fakeAdapter struct {
	mu       sync.Mutex
	sent     []*uspproto.Msg
	sentCh   chan *uspproto.Msg
	isClosed bool
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{sentCh: make(chan *uspproto.Msg, 32)}
}

func (f *fakeAdapter) Connect(_ context.Context) error { return nil }

func (f *fakeAdapter) Send(_ context.Context, b []byte) error {
	_, msg, err := codec.UnwrapRecord(b)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	select {
	case f.sentCh <- msg:
	default:
	}
	return nil
}

func (f *fakeAdapter) Recv() <-chan []byte { return nil }

func (f *fakeAdapter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isClosed {
		return nil
	}
	f.isClosed = true
	close(f.sentCh)
	return nil
}

func (f *fakeAdapter) awaitSend(t *testing.T, d time.Duration) *uspproto.Msg {
	t.Helper()
	select {
	case msg := <-f.sentCh:
		return msg
	case <-time.After(d):
		t.Fatalf("timed out waiting for Send")
		return nil
	}
}

func (f *fakeAdapter) snapshot() []*uspproto.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*uspproto.Msg(nil), f.sent...)
}

func itoa(n int) string {
	const d = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = d[n%10]
		n /= 10
	}
	return string(buf[pos:])
}
