package subscription_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cwmp/scheduler"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/subscription"
)

// These integration tests drive the real Add / Set / Delete handlers
// against a real paramtree.Tree and the subscription evaluator. They
// exercise the seam under test (runtime table mutation → evaluator
// reconciliation → emission) without dragging in mochi-mqtt or paho;
// the broker round-trip is covered by cmd/cpe-sim's Bundle 1 / 2
// integration tests.

func setupIntegration(t *testing.T, withScheduler bool) (*paramtree.Tree, *subscription.Evaluator, *fakeAdapter, *handlers.AddHandler, *handlers.SetHandler, *handlers.DeleteHandler) {
	t.Helper()
	tree := loadEvaluatorTree(t)
	adapter := newFakeAdapter()

	var sched *scheduler.Scheduler
	if withScheduler {
		sched = scheduler.NewScheduler(scheduler.Options{Logger: silentLogger()})
		if err := sched.Start(context.Background()); err != nil {
			t.Fatalf("scheduler.Start: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = sched.Stop(ctx)
		})
	}

	e := subscription.New(tree, adapter, "os::A", "self::openacs", sched, "cpe-1", nil, silentLogger())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("evaluator.Start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })

	// USP handler wiring mirrors cmd/cpe-sim: a ValueChange callback
	// pipes Set-side writes into the evaluator, an OnCreate callback
	// pipes Add-side new rows in, an OnDelete callback pipes Delete-
	// side affected paths in.
	valueChange := func(path string) {
		if v, err := tree.Get(path); err == nil {
			e.NotifyValueChange(path, v.Raw)
		}
	}
	uniqueKeys := map[string][][]string{}

	add := handlers.NewAdd(tree, uniqueKeys, valueChange, func(p string, k map[string]string) { e.NotifyObjectCreated(p, k) })
	set := handlers.NewSet(tree, valueChange)
	del := handlers.NewDelete(tree, func(p string) { e.NotifyObjectDeleted(p) })

	return tree, e, adapter, add, set, del
}

func TestRuntimeAddSubscriptionEndToEnd(t *testing.T) {
	_, _, adapter, add, set, _ := setupIntegration(t, false)

	// Drive Add to create a Subscription row + populate four leaves.
	addReq := newMsg(uspproto.Header_ADD)
	addReq.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Request{
			Request: &uspproto.Request{
				ReqType: &uspproto.Request_Add{
					Add: &uspproto.Add{
						AllowPartial: false,
						CreateObjs: []*uspproto.Add_CreateObject{{
							ObjPath: "Device.LocalAgent.Subscription.",
							ParamSettings: []*uspproto.Add_CreateParamSetting{
								{Param: "Enable", Value: "true"},
								{Param: "ID", Value: "controller-sub-1"},
								{Param: "NotifType", Value: "ValueChange"},
								{Param: "ReferenceList", Value: "Device.WiFi.Radio.1.Channel"},
							},
						}},
					},
				},
			},
		},
	}
	addResp, err := add.Handle(context.Background(), addReq)
	if err != nil {
		t.Fatalf("add.Handle: %v", err)
	}
	results := addResp.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].GetOperStatus().GetOperFailure() != nil {
		t.Fatalf("Add failed: %v", results[0].GetOperStatus().GetOperFailure())
	}
	instPath := results[0].GetOperStatus().GetOperSuccess().GetInstantiatedPath()
	if !strings.HasPrefix(instPath, "Device.LocalAgent.Subscription.") {
		t.Fatalf("unexpected instantiated path: %q", instPath)
	}

	// Give the evaluator's debounced rescan time to pick it up.
	awaitRescan()

	// Now drive a Set on the watched leaf. The evaluator must emit a
	// ValueChange Notify carrying the new SubscriptionID.
	setReq := newMsg(uspproto.Header_SET)
	setReq.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Request{
			Request: &uspproto.Request{
				ReqType: &uspproto.Request_Set{
					Set: &uspproto.Set{
						AllowPartial: false,
						UpdateObjs: []*uspproto.Set_UpdateObject{{
							ObjPath: "Device.WiFi.Radio.1.",
							ParamSettings: []*uspproto.Set_UpdateParamSetting{
								{Param: "Channel", Value: "11"},
							},
						}},
					},
				},
			},
		},
	}
	if _, err := set.Handle(context.Background(), setReq); err != nil {
		t.Fatalf("set.Handle: %v", err)
	}

	msg := adapter.awaitSend(t, 200*time.Millisecond)
	notif := msg.GetBody().GetRequest().GetNotify()
	if notif.GetSubscriptionId() != "controller-sub-1" {
		t.Errorf("SubscriptionId = %q, want controller-sub-1", notif.GetSubscriptionId())
	}
}

func TestRuntimeSetEnableFalseStopsNotifies(t *testing.T) {
	tree, _, adapter, _, set, _ := setupIntegration(t, false)

	// Reconfigure row 1 (the default seed) to a ValueChange subscription.
	setSubField(t, tree, 1, "NotifType", "ValueChange")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	awaitRescan()

	// Baseline: a Set fires a Notify.
	doSet := func(val string) {
		setReq := newMsg(uspproto.Header_SET)
		setReq.Body = &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Set{
						Set: &uspproto.Set{
							AllowPartial: false,
							UpdateObjs: []*uspproto.Set_UpdateObject{{
								ObjPath: "Device.WiFi.Radio.1.",
								ParamSettings: []*uspproto.Set_UpdateParamSetting{
									{Param: "Channel", Value: val},
								},
							}},
						},
					},
				},
			},
		}
		if _, err := set.Handle(context.Background(), setReq); err != nil {
			t.Fatalf("set.Handle: %v", err)
		}
	}
	doSet("9")
	_ = adapter.awaitSend(t, 200*time.Millisecond)
	baseline := len(adapter.snapshot())

	// Disable the row.
	disableReq := newMsg(uspproto.Header_SET)
	disableReq.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Request{
			Request: &uspproto.Request{
				ReqType: &uspproto.Request_Set{
					Set: &uspproto.Set{
						AllowPartial: false,
						UpdateObjs: []*uspproto.Set_UpdateObject{{
							ObjPath: "Device.LocalAgent.Subscription.1.",
							ParamSettings: []*uspproto.Set_UpdateParamSetting{
								{Param: "Enable", Value: "false"},
							},
						}},
					},
				},
			},
		},
	}
	if _, err := set.Handle(context.Background(), disableReq); err != nil {
		t.Fatalf("set.Handle (disable): %v", err)
	}
	awaitRescan()

	// Second Set must NOT emit.
	doSet("11")
	time.Sleep(80 * time.Millisecond)
	if got := len(adapter.snapshot()); got != baseline {
		t.Errorf("got %d notifies after disable, want %d (no new emits)", got, baseline)
	}
}

func TestRuntimeDeletePeriodicNoLeak(t *testing.T) {
	tree, e, _, _, _, del := setupIntegration(t, true)

	// Pre-seed row 1 as Periodic at 60s (real time; we are asserting
	// armed-state, not waiting for ticks).
	setSubField(t, tree, 1, "NotifType", "Periodic")
	setSubField(t, tree, 1, "ReferenceList", "Device.WiFi.Radio.1.Channel")
	setSubField(t, tree, 1, "Period", "60")
	awaitRescan()
	if got := e.PeriodicArmedPeriods()["default-boot-event-ACS"]; got != 60 {
		t.Fatalf("expected armed at 60s, got %+v", e.PeriodicArmedPeriods())
	}

	// Drive Delete via the real handler.
	delReq := newMsg(uspproto.Header_DELETE)
	delReq.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Request{
			Request: &uspproto.Request{
				ReqType: &uspproto.Request_Delete{
					Delete: &uspproto.Delete{
						AllowPartial: false,
						ObjPaths:     []string{"Device.LocalAgent.Subscription.1."},
					},
				},
			},
		},
	}
	resp, err := del.Handle(context.Background(), delReq)
	if err != nil {
		t.Fatalf("del.Handle: %v", err)
	}
	if resp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()[0].GetOperStatus().GetOperFailure() != nil {
		t.Fatalf("Delete failed: %v", resp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()[0].GetOperStatus().GetOperFailure())
	}
	awaitRescan()

	if got := e.PeriodicArmedPeriods(); len(got) != 0 {
		t.Errorf("expected no armed periodics after Delete, got %+v", got)
	}
}

// newMsg returns a USP Msg with the given type pre-populated. Mirrors
// the package-private helper in internal/usp/handlers.
func newMsg(msgType uspproto.Header_MsgType) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{
			MsgId:   "test-msg-id",
			MsgType: msgType,
		},
	}
}
