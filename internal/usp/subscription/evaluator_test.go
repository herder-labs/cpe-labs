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
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, silentLogger())
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
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, silentLogger())
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
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, silentLogger())
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
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, silentLogger())
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
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, silentLogger())
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

func TestEvaluatorMalformedReferenceListLogged(t *testing.T) {
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()
	var logBuf bytes.Buffer
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, capturingLogger(&logBuf, slog.LevelWarn))
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
	var logBuf bytes.Buffer
	e := subscription.New(tree, adapter, "os::A", "self::openacs", nil, "cpe-1", nil, capturingLogger(&logBuf, slog.LevelWarn))
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

// capturingLogger returns a Logger writing to the supplied buffer at
// the given minimum level. Used by tests that assert specific
// warn-level messages were emitted.
func capturingLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
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
