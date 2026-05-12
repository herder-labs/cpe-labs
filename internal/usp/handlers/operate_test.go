package handlers_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

func TestOperateHandlerDeviceReboot(t *testing.T) {
	var rebootCalls atomic.Int32
	h := handlers.NewOperate(func() { rebootCalls.Add(1) })

	req := buildOperateRequest("Device.Reboot()", "task-uuid-1")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	results := resp.GetBody().GetResponse().GetOperateResp().GetOperationResults()
	if len(results) != 1 {
		t.Fatalf("OperationResults len=%d want 1", len(results))
	}
	if results[0].GetExecutedCommand() != "Device.Reboot()" {
		t.Errorf("ExecutedCommand=%q", results[0].GetExecutedCommand())
	}
	if results[0].GetReqOutputArgs() == nil {
		t.Errorf("ReqOutputArgs nil, want empty success")
	}
	if results[0].GetCmdFailure() != nil {
		t.Errorf("CmdFailure non-nil, want empty success")
	}

	// Reboot callback runs in a goroutine; give it a beat.
	for i := 0; i < 50; i++ {
		if rebootCalls.Load() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if rebootCalls.Load() != 1 {
		t.Errorf("Reboot callback calls=%d want 1", rebootCalls.Load())
	}
}

func TestOperateHandlerUnknownCommand(t *testing.T) {
	h := handlers.NewOperate(func() { t.Fatalf("Reboot callback fired for unknown command") })

	req := buildOperateRequest("Device.UnknownOp()", "task-uuid-2")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	result := resp.GetBody().GetResponse().GetOperateResp().GetOperationResults()[0]
	if result.GetReqOutputArgs() != nil {
		t.Errorf("ReqOutputArgs non-nil for unknown command")
	}
	fail := result.GetCmdFailure()
	if fail == nil {
		t.Fatalf("CmdFailure nil, want present for unknown command")
	}
	if fail.GetErrCode() != session.USPErrCommandFailure {
		t.Errorf("ErrCode=%d want %d", fail.GetErrCode(), session.USPErrCommandFailure)
	}
	if result.GetExecutedCommand() != "Device.UnknownOp()" {
		t.Errorf("ExecutedCommand=%q", result.GetExecutedCommand())
	}
	time.Sleep(20 * time.Millisecond)
}

func TestOperateHandlerNilCallbackIsSafe(t *testing.T) {
	h := handlers.NewOperate(nil)
	req := buildOperateRequest("Device.Reboot()", "")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	result := resp.GetBody().GetResponse().GetOperateResp().GetOperationResults()[0]
	if result.GetReqOutputArgs() == nil {
		t.Errorf("expected success result")
	}
}

func buildOperateRequest(command, commandKey string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_OPERATE},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Operate{
						Operate: &uspproto.Operate{
							Command:    command,
							CommandKey: commandKey,
							SendResp:   true,
						},
					},
				},
			},
		},
	}
}
