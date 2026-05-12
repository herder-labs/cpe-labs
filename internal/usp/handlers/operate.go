package handlers

import (
	"context"
	"errors"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

const CommandDeviceReboot = "Device.Reboot()"

type OperateHandler struct {
	Reboot func()
}

func NewOperate(reboot func()) *OperateHandler { return &OperateHandler{Reboot: reboot} }

func (h *OperateHandler) MsgType() uspproto.Header_MsgType { return uspproto.Header_OPERATE }

func (h *OperateHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetOperate()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.operate", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not Operate")}
	}

	cmd := body.GetCommand()
	result := &uspproto.OperateResp_OperationResult{ExecutedCommand: cmd}

	switch cmd {
	case CommandDeviceReboot:
		if h.Reboot != nil {
			go h.Reboot()
		}
		result.OperationResp = &uspproto.OperateResp_OperationResult_ReqOutputArgs{
			ReqOutputArgs: &uspproto.OperateResp_OperationResult_OutputArgs{},
		}
	default:
		result.OperationResp = &uspproto.OperateResp_OperationResult_CmdFailure{
			CmdFailure: &uspproto.OperateResp_OperationResult_CommandFailure{
				ErrCode: session.USPErrCommandFailure,
				ErrMsg:  "Command failed: unknown command " + cmd,
			},
		}
	}

	resp := newMsg(uspproto.Header_OPERATE_RESP)
	resp.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_OperateResp{
					OperateResp: &uspproto.OperateResp{
						OperationResults: []*uspproto.OperateResp_OperationResult{result},
					},
				},
			},
		},
	}
	return resp, nil
}
