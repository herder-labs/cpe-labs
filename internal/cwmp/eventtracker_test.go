package cwmp_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cwmp"
	"github.com/herder-labs/cpe-labs/internal/cwmp/inform"
	"github.com/herder-labs/cpe-labs/internal/cwmp/transfer"
)

func TestNextSessionEventsStartupFirstFiresBootstrap(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	got := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (BOOT + BOOTSTRAP)", len(got))
	}
	if got[0].EventCode != inform.EventBoot {
		t.Errorf("first = %s, want %s", got[0].EventCode, inform.EventBoot)
	}
	if got[1].EventCode != inform.EventBootstrap {
		t.Errorf("second = %s, want %s", got[1].EventCode, inform.EventBootstrap)
	}
}

func TestNextSessionEventsStartupSecondNoBootstrap(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.NextSessionEvents(cwmp.TriggerStartup) // first call flips the flag
	got := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (BOOT only)", len(got))
	}
	if got[0].EventCode != inform.EventBoot {
		t.Errorf("first = %s, want %s", got[0].EventCode, inform.EventBoot)
	}
}

func TestNextSessionEventsPeriodic(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	got := tr.NextSessionEvents(cwmp.TriggerPeriodic)
	if len(got) != 1 || got[0].EventCode != inform.EventPeriodic {
		t.Errorf("got %v, want [2 PERIODIC]", got)
	}
}

func TestNextSessionEventsConnectionRequest(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	got := tr.NextSessionEvents(cwmp.TriggerConnectionRequest)
	if len(got) != 2 ||
		got[0].EventCode != inform.EventConnectionRequest ||
		got[1].EventCode != inform.EventPeriodic {
		t.Errorf("got %v, want [6 CR, 2 PERIODIC]", got)
	}
}

func TestNextSessionEventsValueChange(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	got := tr.NextSessionEvents(cwmp.TriggerValueChange)
	if len(got) != 1 || got[0].EventCode != inform.EventValueChange {
		t.Errorf("got %v, want [4 VALUE CHANGE]", got)
	}
}

func TestQueueMethodRebootDrains(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.QueueMethodReboot("ops-2026")
	got := tr.NextSessionEvents(cwmp.TriggerPeriodic)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (PERIODIC + M Reboot)", len(got))
	}
	if got[1].EventCode != inform.EventMethodReboot || got[1].CommandKey != "ops-2026" {
		t.Errorf("M Reboot = %+v, want EventCode=M Reboot, CommandKey=ops-2026", got[1])
	}
}

func TestQueueMethodMultipleEventsDrainInOrder(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.QueueMethodReboot("a")
	tr.QueueMethodDownload("b")
	tr.QueueMethodUpload("c")
	got := tr.NextSessionEvents(cwmp.TriggerPeriodic)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	want := []string{
		inform.EventPeriodic,
		inform.EventMethodReboot,
		inform.EventMethodDownload,
		inform.EventMethodUpload,
	}
	for i, w := range want {
		if got[i].EventCode != w {
			t.Errorf("got[%d] = %s, want %s", i, got[i].EventCode, w)
		}
	}
}

func TestNextSessionEventsDrainsMEventsOnce(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.QueueMethodReboot("a")
	first := tr.NextSessionEvents(cwmp.TriggerPeriodic)
	second := tr.NextSessionEvents(cwmp.TriggerPeriodic)

	if len(first) != 2 {
		t.Errorf("first len = %d, want 2", len(first))
	}
	if len(second) != 1 {
		t.Errorf("second len = %d, want 1 (M-event already drained)", len(second))
	}
}

func TestRecordValueChangeAccumulates(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.RecordValueChange("Device.WiFi.SSID")
	tr.RecordValueChange("Device.DeviceInfo.UpTime")

	if !tr.HasPendingValueChanges() {
		t.Error("HasPendingValueChanges = false, want true")
	}

	got := tr.SessionParameterLists()
	paths := got[inform.EventValueChange]
	if len(paths) != 2 || paths[0] != "Device.WiFi.SSID" || paths[1] != "Device.DeviceInfo.UpTime" {
		t.Errorf("EventValueChange paths = %v", paths)
	}
}

func TestRecordValueChangeIgnoresEmpty(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.RecordValueChange("")
	if tr.HasPendingValueChanges() {
		t.Error("empty path should not create pending change")
	}
}

func TestSessionParameterListsOverridesValueChange(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(map[string][]string{
		inform.EventValueChange: {"Device.BaseList.X"},
		inform.EventPeriodic:    {"Device.DeviceInfo.UpTime"},
	})
	tr.RecordValueChange("Device.WiFi.SSID")

	got := tr.SessionParameterLists()
	vc := got[inform.EventValueChange]
	if len(vc) != 1 || vc[0] != "Device.WiFi.SSID" {
		t.Errorf("EventValueChange should be overridden, got %v", vc)
	}
	periodic := got[inform.EventPeriodic]
	if len(periodic) != 1 || periodic[0] != "Device.DeviceInfo.UpTime" {
		t.Errorf("EventPeriodic should remain unchanged, got %v", periodic)
	}
}

func TestAcknowledgeClearsValueChanges(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.RecordValueChange("Device.WiFi.SSID")
	tr.Acknowledge()
	if tr.HasPendingValueChanges() {
		t.Error("HasPendingValueChanges should be false after Acknowledge")
	}
}

func TestAcknowledgeIsNoopForBootstrap(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.NextSessionEvents(cwmp.TriggerStartup) // flips bootstrap
	tr.Acknowledge()

	got := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(got) != 1 || got[0].EventCode != inform.EventBoot {
		t.Errorf("post-Ack startup should still be BOOT-only, got %v", got)
	}
}

func TestResetBootstrapReArmsBootstrap(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	first := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(first) != 2 {
		t.Fatalf("first startup len = %d, want 2", len(first))
	}
	// Second startup before reset: BOOT only.
	second := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(second) != 1 {
		t.Fatalf("second startup len = %d, want 1", len(second))
	}

	tr.ResetBootstrap()

	third := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(third) != 2 {
		t.Fatalf("post-reset startup len = %d, want 2", len(third))
	}
	if third[1].EventCode != inform.EventBootstrap {
		t.Errorf("post-reset second event = %s, want %s", third[1].EventCode, inform.EventBootstrap)
	}
}

func TestResetBootstrapBeforeFirstStartupIsNoop(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	tr.ResetBootstrap() // flag was already false; should be safe
	got := tr.NextSessionEvents(cwmp.TriggerStartup)
	if len(got) != 2 {
		t.Errorf("startup after early ResetBootstrap len = %d, want 2 (BOOT+BOOTSTRAP)", len(got))
	}
}

func TestQueueAndDrainTransferComplete(t *testing.T) {
	t.Parallel()

	tr := cwmp.NewEventTracker(nil)
	if tr.HasPendingTransferCompletes() {
		t.Fatal("fresh tracker should have no pending TCs")
	}

	now := time.Now().UTC()
	tr.QueueTransferComplete(transfer.Complete{CommandKey: "a", StartTime: now, CompleteTime: now})
	tr.QueueTransferComplete(transfer.Complete{CommandKey: "b", StartTime: now, CompleteTime: now})

	if !tr.HasPendingTransferCompletes() {
		t.Fatal("expected pending TCs after queueing")
	}
	got := tr.DrainTransferCompletes()
	if len(got) != 2 || got[0].CommandKey != "a" || got[1].CommandKey != "b" {
		t.Errorf("Drain got %+v, want [{a},{b}] FIFO", got)
	}
	if tr.HasPendingTransferCompletes() {
		t.Error("Drain should clear the queue")
	}
	if tr.DrainTransferCompletes() != nil {
		t.Error("second Drain should return nil")
	}
}

func TestNewEventTrackerCopiesInputMap(t *testing.T) {
	t.Parallel()

	base := map[string][]string{
		inform.EventPeriodic: {"Device.DeviceInfo.UpTime"},
	}
	tr := cwmp.NewEventTracker(base)
	// Mutate the caller's map post-construction.
	base[inform.EventPeriodic] = []string{"Device.Mutated"}

	got := tr.SessionParameterLists()
	if got[inform.EventPeriodic][0] != "Device.DeviceInfo.UpTime" {
		t.Errorf("tracker's view should be insulated from caller mutation; got %v", got[inform.EventPeriodic])
	}
}
