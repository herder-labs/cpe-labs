package session

import (
	"context"
	"errors"
	"log/slog"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/mtp"
	"github.com/herder-labs/cpe-labs/internal/usp/notify"
)

const (
	RebootCausePath       = "Internal.Reboot.Cause"
	RebootCauseFactoryRst = "LocalFactoryReset"
	RebootCauseLocalBoot  = "LocalReboot"
)

type Options struct {
	Tree             *paramtree.Tree
	Adapter          mtp.Adapter
	AgentEID         string
	ControllerEID    string
	OnBoardBuilder   *notify.OnBoardRequestBuilder
	BootEventBuilder *notify.BootEventBuilder
	Logger           *slog.Logger
}

func Run(ctx context.Context, opts Options) error {
	if err := validateOptions(opts); err != nil {
		return err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	onBoardBuilder := opts.OnBoardBuilder
	if onBoardBuilder == nil {
		onBoardBuilder = &notify.OnBoardRequestBuilder{}
	}
	bootBuilder := opts.BootEventBuilder
	if bootBuilder == nil {
		bootBuilder = &notify.BootEventBuilder{}
	}

	if err := opts.Adapter.Connect(ctx); err != nil {
		return err
	}

	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		runRecv(ctx, opts.Adapter.Recv(), logger)
	}()

	if err := emitFirstContact(ctx, opts, onBoardBuilder, bootBuilder, logger); err != nil {
		closeAdapter(opts.Adapter, logger)
		<-recvDone
		return err
	}

	<-ctx.Done()
	closeAdapter(opts.Adapter, logger)
	<-recvDone
	return nil
}

func emitFirstContact(ctx context.Context, opts Options, onBoard *notify.OnBoardRequestBuilder, boot *notify.BootEventBuilder, logger *slog.Logger) error {
	cause, err := readRebootCause(opts.Tree)
	if err != nil {
		return err
	}

	var msg *uspproto.Msg
	var msgKind string
	if cause == RebootCauseFactoryRst {
		msg, err = onBoard.Build(opts.Tree)
		msgKind = "OnBoardRequest"
	} else {
		msg, err = boot.Build(opts.Tree)
		msgKind = "Event{Boot!}"
	}
	if err != nil {
		return err
	}

	wire, err := codec.WrapMessage(msg, opts.AgentEID, opts.ControllerEID)
	if err != nil {
		return err
	}
	if err := opts.Adapter.Send(ctx, wire); err != nil {
		return err
	}

	logger.Info("usp first-contact emitted",
		"kind", msgKind,
		"msg_id", msg.GetHeader().GetMsgId(),
		"from", opts.AgentEID,
		"to", opts.ControllerEID,
	)

	if cause == RebootCauseFactoryRst {
		if err := opts.Tree.SetSystem(RebootCausePath, RebootCauseLocalBoot); err != nil {
			return &cpeerr.Error{Op: "session.flipRebootCause", Kind: cpeerr.KindInternal, Err: err}
		}
	}
	return nil
}

func runRecv(ctx context.Context, ch <-chan []byte, logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			record, msg, err := codec.UnwrapRecord(b)
			if err != nil {
				logger.Warn("usp recv decode failed", "err", err, "bytes", len(b))
				continue
			}
			logger.Info("usp recv",
				"msg_id", msg.GetHeader().GetMsgId(),
				"msg_type", msg.GetHeader().GetMsgType().String(),
				"from_id", record.GetFromId(),
			)
		}
	}
}

func validateOptions(o Options) error {
	if o.Tree == nil {
		return &cpeerr.Error{Op: "session.Run", Kind: cpeerr.KindInvalidArgument, Err: errors.New("Tree is nil")}
	}
	if o.Adapter == nil {
		return &cpeerr.Error{Op: "session.Run", Kind: cpeerr.KindInvalidArgument, Err: errors.New("Adapter is nil")}
	}
	if o.AgentEID == "" {
		return &cpeerr.Error{Op: "session.Run", Kind: cpeerr.KindInvalidArgument, Err: errors.New("AgentEID is empty")}
	}
	if o.ControllerEID == "" {
		return &cpeerr.Error{Op: "session.Run", Kind: cpeerr.KindInvalidArgument, Err: errors.New("ControllerEID is empty")}
	}
	return nil
}

func readRebootCause(tree *paramtree.Tree) (string, error) {
	v, err := tree.Get(RebootCausePath)
	if err != nil {
		return "", &cpeerr.Error{Op: "session.readRebootCause", Kind: cpeerr.KindNotFound, Err: err}
	}
	return v.Raw, nil
}

func closeAdapter(a mtp.Adapter, logger *slog.Logger) {
	if err := a.Close(); err != nil {
		logger.Warn("usp adapter close", "err", err)
	}
}
