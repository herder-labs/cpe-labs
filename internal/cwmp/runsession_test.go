package cwmp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cwmp"
	"github.com/herder-labs/cpe-labs/internal/cwmp/inform"
	"github.com/herder-labs/cpe-labs/internal/cwmp/transfer"
	"github.com/herder-labs/cpe-labs/internal/cwmp/transport"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/testgolden"
)

const transferCompleteResponseEnvelope = `<?xml version="1.0"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:cwmp="urn:dslforum-org:cwmp-1-1">
  <soapenv:Header><cwmp:ID soapenv:mustUnderstand="1">test-id</cwmp:ID></soapenv:Header>
  <soapenv:Body>
    <cwmp:TransferCompleteResponse/>
  </soapenv:Body>
</soapenv:Envelope>`

// buildRunSessionScaffold returns a tracker, a built session, and the
// fakeACS so tests can assert on the bytes the ACS observes.
func buildRunSessionScaffold(t *testing.T, scripts []string, statuses []int, baseLists map[string][]string) (
	*cwmp.EventTracker, *cwmp.Session, *fakeACS, *transport.Transport,
) {
	t.Helper()
	acs := newFakeACS(scripts...)
	if len(statuses) > 0 {
		acs.statuses = statuses
	}
	t.Cleanup(acs.close)

	pool, err := transport.NewPool(transport.PoolOptions{Logger: silentLogger()})
	if err != nil {
		t.Fatal(err)
	}
	tt, err := transport.NewTransport(pool, transport.Config{ACSURL: acs.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	tree := buildTree(t)
	// Build a placeholder Inform builder; RunSession will swap it.
	placeholder, _ := inform.NewBuilder(tree, inform.BuilderOptions{
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	})
	s, err := cwmp.NewSession(cwmp.SessionOptions{
		Transport: tt,
		Inform:    placeholder,
		Logger:    silentLogger(),
		IDGenerator: func() string {
			return "test-id"
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return cwmp.NewEventTracker(baseLists), s, acs, tt
}

func TestRunSessionSuccessAcknowledges(t *testing.T) {
	t.Parallel()

	tr, s, _, _ := buildRunSessionScaffold(t,
		[]string{informResponseEnvelope, ""},
		[]int{200, http.StatusNoContent},
		nil,
	)

	tree := buildTree(t)
	tr.QueueMethodReboot("ops-1")
	tr.RecordValueChange("Device.WiFi.SSID")

	err := cwmp.RunSession(context.Background(), cwmp.RunSessionOptions{
		Tracker:       tr,
		Tree:          tree,
		Session:       s,
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	}, cwmp.TriggerStartup)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}

	// Acknowledge cleared value changes.
	if tr.HasPendingValueChanges() {
		t.Error("Acknowledge should have cleared pending value changes on success")
	}
	// Subsequent NextSessionEvents has no M-events queued.
	got := tr.NextSessionEvents(cwmp.TriggerPeriodic)
	if len(got) != 1 {
		t.Errorf("M-events should have been delivered + acknowledged; got %v", got)
	}
}

func TestRunSessionFailureRequeuesMEvents(t *testing.T) {
	t.Parallel()

	// ACS returns a fault on Inform → session fails.
	tr, s, _, _ := buildRunSessionScaffold(t,
		[]string{acsFaultEnvelope},
		nil,
		nil,
	)

	tree := buildTree(t)
	tr.QueueMethodReboot("ops-1")
	tr.QueueMethodDownload("ops-2")

	err := cwmp.RunSession(context.Background(), cwmp.RunSessionOptions{
		Tracker:       tr,
		Tree:          tree,
		Session:       s,
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	}, cwmp.TriggerPeriodic)
	if err == nil {
		t.Fatal("expected error from ACS fault")
	}

	// M-events should be re-queued for the next attempt.
	got := tr.NextSessionEvents(cwmp.TriggerPeriodic)
	if len(got) != 3 {
		t.Fatalf("post-failure events len = %d, want 3 (PERIODIC + Reboot + Download)", len(got))
	}
	mEvents := []string{got[1].EventCode, got[2].EventCode}
	wantSet := map[string]bool{
		inform.EventMethodReboot:   true,
		inform.EventMethodDownload: true,
	}
	for _, e := range mEvents {
		if !wantSet[e] {
			t.Errorf("unexpected re-queued M-event %q", e)
		}
	}
}

func TestRunSessionFailureKeepsValueChanges(t *testing.T) {
	t.Parallel()

	tr, s, _, _ := buildRunSessionScaffold(t,
		[]string{acsFaultEnvelope},
		nil,
		nil,
	)
	tree := buildTree(t)
	tr.RecordValueChange("Device.WiFi.SSID")

	_ = cwmp.RunSession(context.Background(), cwmp.RunSessionOptions{
		Tracker:       tr,
		Tree:          tree,
		Session:       s,
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	}, cwmp.TriggerValueChange)

	if !tr.HasPendingValueChanges() {
		t.Error("value-change paths should still be pending after failure")
	}
}

func TestRunSessionDrainsTransferCompletes(t *testing.T) {
	t.Parallel()

	// Three calls expected: Inform, TransferComplete, drain (204).
	tr, s, acs, _ := buildRunSessionScaffold(t,
		[]string{informResponseEnvelope, transferCompleteResponseEnvelope, ""},
		[]int{200, 200, http.StatusNoContent},
		nil,
	)
	tree := buildTree(t)
	tr.QueueMethodDownload("ops-1")
	tr.QueueTransferComplete(transfer.Complete{
		CommandKey:   "ops-1",
		FaultCode:    0,
		StartTime:    fixedTime,
		CompleteTime: fixedTime.Add(5 * time.Second),
	})

	err := cwmp.RunSession(context.Background(), cwmp.RunSessionOptions{
		Tracker:       tr,
		Tree:          tree,
		Session:       s,
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	}, cwmp.TriggerPeriodic)
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}

	if tr.HasPendingTransferCompletes() {
		t.Error("queue should be drained on session success")
	}
	if int(acs.callCount.Load()) != 3 {
		t.Errorf("ACS calls = %d, want 3 (Inform + TransferComplete + drain)", acs.callCount.Load())
	}
	if !strings.Contains(string(acs.bodies[1]), "<cwmp:TransferComplete>") {
		t.Errorf("second ACS request missing TransferComplete:\n%s", acs.bodies[1])
	}
}

func TestRunSessionFailureRequeuesTransferCompletes(t *testing.T) {
	t.Parallel()

	tr, s, _, _ := buildRunSessionScaffold(t,
		[]string{acsFaultEnvelope},
		nil,
		nil,
	)
	tree := buildTree(t)
	tr.QueueTransferComplete(transfer.Complete{
		CommandKey:   "ops-1",
		StartTime:    fixedTime,
		CompleteTime: fixedTime,
	})

	_ = cwmp.RunSession(context.Background(), cwmp.RunSessionOptions{
		Tracker:       tr,
		Tree:          tree,
		Session:       s,
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	}, cwmp.TriggerPeriodic)

	if !tr.HasPendingTransferCompletes() {
		t.Error("TransferComplete should be re-queued after session failure")
	}
}

func TestRunSessionMissingTrackerRejected(t *testing.T) {
	t.Parallel()

	err := cwmp.RunSession(context.Background(), cwmp.RunSessionOptions{}, cwmp.TriggerPeriodic)
	if err == nil {
		t.Fatal("expected error for missing tracker")
	}
}

func TestRunSessionThreeSessionFlow(t *testing.T) {
	t.Parallel()

	// Three sessions: Startup → Periodic → ValueChange. Each ACS reply
	// is just InformResponse + 204 to keep things simple.
	scripts := []string{
		informResponseEnvelope, "", // session 1: Inform + drain-close
		informResponseEnvelope, "", // session 2
		informResponseEnvelope, "", // session 3
	}
	statuses := []int{200, http.StatusNoContent, 200, http.StatusNoContent, 200, http.StatusNoContent}
	tr, s, acs, _ := buildRunSessionScaffold(t, scripts, statuses, map[string][]string{
		inform.EventPeriodic: {"Device.DeviceInfo.SerialNumber"},
	})
	tree := buildTree(t)
	opts := cwmp.RunSessionOptions{
		Tracker:       tr,
		Tree:          tree,
		Session:       s,
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	}

	// Session 1: Startup → BOOT + BOOTSTRAP
	if err := cwmp.RunSession(context.Background(), opts, cwmp.TriggerStartup); err != nil {
		t.Fatalf("session 1: %v", err)
	}
	body1 := string(acs.bodies[0])
	if !strings.Contains(body1, inform.EventBoot) || !strings.Contains(body1, inform.EventBootstrap) {
		t.Errorf("session 1 should include BOOT + BOOTSTRAP; body:\n%s", body1)
	}

	// Session 2: Periodic — no BOOTSTRAP this time.
	if err := cwmp.RunSession(context.Background(), opts, cwmp.TriggerPeriodic); err != nil {
		t.Fatalf("session 2: %v", err)
	}
	body2 := string(acs.bodies[2]) // index 2 because empty-POST drain at index 1
	if !strings.Contains(body2, inform.EventPeriodic) {
		t.Errorf("session 2 should be PERIODIC; body:\n%s", body2)
	}
	if strings.Contains(body2, inform.EventBootstrap) {
		t.Errorf("session 2 should NOT include BOOTSTRAP (already fired); body:\n%s", body2)
	}

	// Session 3: ValueChange after a path is recorded. We need the
	// path to exist in the tree, so add it before recording.
	if err := tree.Mount("Device.WiFi.SSID", paramtree.NewLeaf(paramtree.Value{
		Type: paramtree.TypeString, Raw: "home",
	})); err != nil {
		t.Fatal(err)
	}
	tr.RecordValueChange("Device.WiFi.SSID")
	if err := cwmp.RunSession(context.Background(), opts, cwmp.TriggerValueChange); err != nil {
		t.Fatalf("session 3: %v", err)
	}
	body3 := string(acs.bodies[4])
	if !strings.Contains(body3, inform.EventValueChange) {
		t.Errorf("session 3 should include VALUE CHANGE; body:\n%s", body3)
	}
}

// TestBootOnlyGolden generates and locks down the post-bootstrap
// startup Inform body — the same shape as inform_bootstrap.xml but
// without the 0 BOOTSTRAP event.
func TestBootOnlyGolden(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	// First call flips bootstrap.
	tr.NextSessionEvents(cwmp.TriggerStartup)
	// Second call returns BOOT only.
	events := tr.NextSessionEvents(cwmp.TriggerStartup)

	tree := buildTree(t)
	b, err := inform.NewBuilder(tree, inform.BuilderOptions{
		Clock:         func() time.Time { return fixedTime },
		DeviceIDPaths: testDeviceIDPaths,
	})
	if err != nil {
		t.Fatal(err)
	}
	inf, err := b.Build(events, 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := inform.Render(&buf, inf); err != nil {
		t.Fatal(err)
	}
	testgolden.Compare(t, "inform_boot_only.xml", []byte(buf.String()))
}
