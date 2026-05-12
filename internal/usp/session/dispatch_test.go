package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

func TestDispatchUnmappedMsgTypeReturnsError7000(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseLocalBoot)
	fa := newFakeAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.Run(ctx, sessionOpts(tree, fa)) }()
	_ = fa.awaitSend(t, 2*time.Second) // drain first-contact emission

	reqBytes := wrapRequest(t, "msg-A", uspproto.Header_GET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Get{Get: &uspproto.Get{ParamPaths: []string{"Device.Test"}}},
		}},
	})
	fa.recv <- reqBytes

	respBytes := fa.awaitSend(t, 2*time.Second)
	_, resp, err := codec.UnwrapRecord(respBytes)
	if err != nil {
		t.Fatalf("UnwrapRecord: %v", err)
	}
	if resp.GetHeader().GetMsgType() != uspproto.Header_ERROR {
		t.Fatalf("MsgType=%v want ERROR", resp.GetHeader().GetMsgType())
	}
	if resp.GetHeader().GetMsgId() != "msg-A" {
		t.Fatalf("MsgId=%q want msg-A", resp.GetHeader().GetMsgId())
	}
	errBody := resp.GetBody().GetError()
	if errBody == nil {
		t.Fatalf("Error body nil")
	}
	if errBody.GetErrCode() != session.USPErrMessageFailed {
		t.Fatalf("ErrCode=%d want %d", errBody.GetErrCode(), session.USPErrMessageFailed)
	}

	cancel()
	<-done
}

func TestDispatchInvokesHandlerByMsgType(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseLocalBoot)
	fa := newFakeAdapter()

	stub := &stubHandler{msgType: uspproto.Header_GET, responseMsg: &uspproto.Msg{
		Header: &uspproto.Header{MsgType: uspproto.Header_GET_RESP},
		Body: &uspproto.Body{MsgBody: &uspproto.Body_Response{Response: &uspproto.Response{
			RespType: &uspproto.Response_GetResp{GetResp: &uspproto.GetResp{}},
		}}},
	}}

	opts := sessionOpts(tree, fa)
	opts.Handlers = []session.Handler{stub}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.Run(ctx, opts) }()
	_ = fa.awaitSend(t, 2*time.Second) // first-contact

	reqBytes := wrapRequest(t, "msg-B", uspproto.Header_GET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Get{Get: &uspproto.Get{ParamPaths: []string{"x"}}},
		}},
	})
	fa.recv <- reqBytes

	respBytes := fa.awaitSend(t, 2*time.Second)
	_, resp, _ := codec.UnwrapRecord(respBytes)
	if resp.GetHeader().GetMsgType() != uspproto.Header_GET_RESP {
		t.Fatalf("MsgType=%v want GET_RESP", resp.GetHeader().GetMsgType())
	}
	if resp.GetHeader().GetMsgId() != "msg-B" {
		t.Fatalf("MsgId=%q want msg-B", resp.GetHeader().GetMsgId())
	}
	if stub.calls != 1 {
		t.Fatalf("stub.calls=%d want 1", stub.calls)
	}

	cancel()
	<-done
}

func TestDispatchHandlerCpeerrFaultCodeMapsToErrorCode(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseLocalBoot)
	fa := newFakeAdapter()

	stub := &stubHandler{
		msgType:     uspproto.Header_SET,
		responseErr: &cpeerr.Error{Op: "stub", Kind: cpeerr.KindInvalidArgument, FaultCode: 7008, Err: errors.New("test")},
	}
	opts := sessionOpts(tree, fa)
	opts.Handlers = []session.Handler{stub}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.Run(ctx, opts) }()
	_ = fa.awaitSend(t, 2*time.Second)

	reqBytes := wrapRequest(t, "msg-C", uspproto.Header_SET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Set{Set: &uspproto.Set{}},
		}},
	})
	fa.recv <- reqBytes

	respBytes := fa.awaitSend(t, 2*time.Second)
	_, resp, _ := codec.UnwrapRecord(respBytes)
	if resp.GetBody().GetError().GetErrCode() != 7008 {
		t.Fatalf("ErrCode=%d want 7008", resp.GetBody().GetError().GetErrCode())
	}
	if resp.GetHeader().GetMsgId() != "msg-C" {
		t.Fatalf("MsgId=%q want msg-C", resp.GetHeader().GetMsgId())
	}

	cancel()
	<-done
}

func TestDispatchHandlerGenericErrorMapsTo7000(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseLocalBoot)
	fa := newFakeAdapter()

	stub := &stubHandler{msgType: uspproto.Header_DELETE, responseErr: errors.New("boom")}
	opts := sessionOpts(tree, fa)
	opts.Handlers = []session.Handler{stub}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.Run(ctx, opts) }()
	_ = fa.awaitSend(t, 2*time.Second)

	reqBytes := wrapRequest(t, "msg-D", uspproto.Header_DELETE, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Delete{Delete: &uspproto.Delete{}},
		}},
	})
	fa.recv <- reqBytes

	respBytes := fa.awaitSend(t, 2*time.Second)
	_, resp, _ := codec.UnwrapRecord(respBytes)
	if resp.GetBody().GetError().GetErrCode() != session.USPErrMessageFailed {
		t.Fatalf("ErrCode=%d want %d", resp.GetBody().GetError().GetErrCode(), session.USPErrMessageFailed)
	}

	cancel()
	<-done
}

func TestDispatchResponseRecordCarriesAgentAndControllerEIDs(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseLocalBoot)
	fa := newFakeAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- session.Run(ctx, sessionOpts(tree, fa)) }()
	_ = fa.awaitSend(t, 2*time.Second)

	reqBytes := wrapRequest(t, "msg-E", uspproto.Header_GET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Get{Get: &uspproto.Get{}},
		}},
	})
	fa.recv <- reqBytes

	respBytes := fa.awaitSend(t, 2*time.Second)
	record, _, _ := codec.UnwrapRecord(respBytes)
	if record.GetFromId() != "os::001122SN123" {
		t.Fatalf("FromId=%q want os::001122SN123", record.GetFromId())
	}
	if record.GetToId() != "self::openacs" {
		t.Fatalf("ToId=%q want self::openacs", record.GetToId())
	}

	cancel()
	<-done
}

// wrapRequest builds a Record wrapping a Request Msg with the given
// msg_id, msg_type, and body, originating from the controller EID.
func wrapRequest(t *testing.T, msgID string, msgType uspproto.Header_MsgType, body *uspproto.Body) []byte {
	t.Helper()
	msg := &uspproto.Msg{
		Header: &uspproto.Header{MsgId: msgID, MsgType: msgType},
		Body:   body,
	}
	bytes, err := codec.WrapMessage(msg, "self::openacs", "os::001122SN123")
	if err != nil {
		t.Fatalf("wrapRequest: %v", err)
	}
	return bytes
}

type stubHandler struct {
	msgType     uspproto.Header_MsgType
	responseMsg *uspproto.Msg
	responseErr error
	calls       int
}

func (s *stubHandler) MsgType() uspproto.Header_MsgType { return s.msgType }

func (s *stubHandler) Handle(ctx context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	s.calls++
	if s.responseErr != nil {
		return nil, s.responseErr
	}
	if s.responseMsg == nil {
		return nil, nil
	}
	// Caller can leave responseMsg.Header.MsgId blank; dispatch echoes
	// req's msg_id automatically.
	return s.responseMsg, nil
}

// suppress paramtree unused-import warnings in this file.
var _ = paramtree.New
