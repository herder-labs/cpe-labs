package session

import (
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

const (
	USPErrMessageFailed       uint32 = 7000
	USPErrInvalidArguments    uint32 = 7008
	USPErrCommandFailure      uint32 = 7022
	USPErrInvalidPath         uint32 = 7026
	USPErrObjectDoesNotExist  uint32 = 7404
	USPErrRequiredParamFailed uint32 = 7800
)

func buildErrorMsg(reqMsgID string, code uint32, msg string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{
			MsgId:   reqMsgID,
			MsgType: uspproto.Header_ERROR,
		},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Error{
				Error: &uspproto.Error{
					ErrCode: code,
					ErrMsg:  msg,
				},
			},
		},
	}
}
