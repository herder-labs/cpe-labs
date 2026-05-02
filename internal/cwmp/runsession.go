package cwmp

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/cwmp/inform"
	"github.com/herder-labs/cpe-labs/internal/cwmp/transfer"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

// RunSessionOptions configures one RunSession call. Tracker, Tree,
// and Session are required; DeviceIDPaths and Clock fall back to
// inform's defaults when zero-valued.
type RunSessionOptions struct {
	Tracker       *EventTracker
	Tree          *paramtree.Tree
	Session       *Session
	DeviceIDPaths inform.DeviceIDPaths
	Clock         func() time.Time
	MaxEnvelopes  uint
}

// RunSession runs one CWMP session: pulls events + parameter lists
// from tracker, builds a fresh inform.Builder bound to tree, runs
// session.Run, and Acknowledges on success. On failure, re-queues any
// drained M-events so they fire on the next session; pending
// value-change paths stay queued.
//
// Returns the session's error (nil on success).
func RunSession(ctx context.Context, opts RunSessionOptions, trigger Trigger) error {
	if opts.Tracker == nil {
		return cpeerr.Wrap("cwmp.RunSession", cpeerr.KindInvalidArgument,
			fmt.Errorf("tracker is required"))
	}
	if opts.Tree == nil {
		return cpeerr.Wrap("cwmp.RunSession", cpeerr.KindInvalidArgument,
			fmt.Errorf("tree is required"))
	}
	if opts.Session == nil {
		return cpeerr.Wrap("cwmp.RunSession", cpeerr.KindInvalidArgument,
			fmt.Errorf("session is required"))
	}

	events := opts.Tracker.NextSessionEvents(trigger)
	if len(events) == 0 {
		return cpeerr.Wrap("cwmp.RunSession", cpeerr.KindInvalidArgument,
			fmt.Errorf("tracker produced no events for trigger %d", trigger))
	}
	transferCompletes := opts.Tracker.DrainTransferCompletes()

	// Build a fresh inform.Builder with this session's parameter lists.
	builder, err := inform.NewBuilder(opts.Tree, inform.BuilderOptions{
		DeviceIDPaths:  opts.DeviceIDPaths,
		ParameterLists: opts.Tracker.SessionParameterLists(),
		Clock:          opts.Clock,
		MaxEnvelopes:   opts.MaxEnvelopes,
	})
	if err != nil {
		// Re-queue M-events and TransferCompletes so this attempt
		// doesn't lose them.
		requeueMEvents(opts.Tracker, events)
		requeueTransferCompletes(opts.Tracker, transferCompletes)
		return err
	}

	// Swap the Session's Builder for this one. Session is per-CPE; in
	// production, RunSession-per-trigger constructs a new Builder each
	// time. The Session struct holds a builder pointer that we
	// short-circuit by exposing setBuilder (test-friendly indirection).
	opts.Session.setBuilder(builder)
	opts.Session.setPendingCPERPCs(adaptTransferCompletes(transferCompletes))

	if err := opts.Session.Run(ctx, events); err != nil {
		requeueMEvents(opts.Tracker, events)
		requeueTransferCompletes(opts.Tracker, transferCompletes)
		return err
	}

	opts.Tracker.Acknowledge()
	return nil
}

// adaptTransferCompletes wraps each TransferComplete record in a
// CPEInitiatedRPC adapter so the session can send it generically.
func adaptTransferCompletes(recs []transfer.Complete) []CPEInitiatedRPC {
	if len(recs) == 0 {
		return nil
	}
	out := make([]CPEInitiatedRPC, len(recs))
	for i, r := range recs {
		out[i] = transferCompleteRPC{rec: r}
	}
	return out
}

// transferCompleteRPC adapts transfer.Complete to CPEInitiatedRPC.
type transferCompleteRPC struct {
	rec transfer.Complete
}

func (t transferCompleteRPC) Method() string { return "TransferComplete" }

func (t transferCompleteRPC) Body() ([]byte, error) {
	var buf bytes.Buffer
	if err := transfer.Render(&buf, &t.rec); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// requeueTransferCompletes re-enqueues records that were drained but
// not delivered (because the session failed). FIFO order is preserved.
func requeueTransferCompletes(t *EventTracker, recs []transfer.Complete) {
	for _, r := range recs {
		t.QueueTransferComplete(r)
	}
}

// requeueMEvents re-queues any M-events that were drained from the
// tracker via NextSessionEvents but not delivered (because the session
// failed). The trigger event itself is NOT re-queued (a periodic
// trigger lost to a transport error is not preserved; the next periodic
// timer will fire its own).
func requeueMEvents(t *EventTracker, events []inform.Event) {
	for _, e := range events {
		if !strings.HasPrefix(e.EventCode, "M ") {
			continue
		}
		switch e.EventCode {
		case inform.EventMethodReboot:
			t.QueueMethodReboot(e.CommandKey)
		case inform.EventMethodScheduleInform:
			t.QueueMethodScheduleInform(e.CommandKey)
		case inform.EventMethodDownload:
			t.QueueMethodDownload(e.CommandKey)
		case inform.EventMethodUpload:
			t.QueueMethodUpload(e.CommandKey)
		}
	}
}
