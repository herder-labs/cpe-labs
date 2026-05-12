package session

import (
	"context"

	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

type Handler interface {
	MsgType() uspproto.Header_MsgType
	Handle(ctx context.Context, req *uspproto.Msg) (*uspproto.Msg, error)
}
