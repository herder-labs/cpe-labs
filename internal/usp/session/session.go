package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/metrics"
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
	Handlers         []Handler
	Logger           *slog.Logger
	Metrics          *metrics.Registry
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

	dispatch := make(map[uspproto.Header_MsgType]Handler, len(opts.Handlers))
	for _, h := range opts.Handlers {
		dispatch[h.MsgType()] = h
	}

	if err := opts.Adapter.Connect(ctx); err != nil {
		return err
	}

	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		runRecv(ctx, opts, dispatch, logger)
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

	if opts.Metrics != nil {
		opts.Metrics.BootstrapsTotal.Inc()
	}

	if cause == RebootCauseFactoryRst {
		if err := opts.Tree.SetSystem(RebootCausePath, RebootCauseLocalBoot); err != nil {
			return &cpeerr.Error{Op: "session.flipRebootCause", Kind: cpeerr.KindInternal, Err: err}
		}
	}
	return nil
}

func runRecv(ctx context.Context, opts Options, dispatch map[uspproto.Header_MsgType]Handler, logger *slog.Logger) {
	ch := opts.Adapter.Recv()
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			handleInbound(ctx, opts, dispatch, b, logger)
		}
	}
}

func handleInbound(ctx context.Context, opts Options, dispatch map[uspproto.Header_MsgType]Handler, b []byte, logger *slog.Logger) {
	record, req, err := codec.UnwrapRecord(b)
	if err != nil {
		logger.Warn("usp recv decode failed", "err", err.Error(), "bytes", len(b))
		return
	}
	reqHeader := req.GetHeader()
	logger.Debug("usp recv",
		"msg_id", reqHeader.GetMsgId(),
		"msg_type", reqHeader.GetMsgType().String(),
		"from_id", record.GetFromId(),
	)

	msgTypeLabel := reqHeader.GetMsgType().String()
	start := time.Now()

	handler, ok := dispatch[reqHeader.GetMsgType()]
	if !ok {
		resp := buildErrorMsg(reqHeader.GetMsgId(), USPErrMessageFailed,
			fmt.Sprintf("Message failed: msg_type %s not supported", reqHeader.GetMsgType()))
		if opts.Metrics != nil {
			opts.Metrics.USPRequests.WithLabelValues(msgTypeLabel, "unsupported").Inc()
			opts.Metrics.USPRespRTT.WithLabelValues(msgTypeLabel).Observe(time.Since(start).Seconds())
		}
		sendResponse(ctx, opts, record, resp, logger)
		return
	}

	resp, herr := handler.Handle(ctx, req)
	result := "ok"
	if herr != nil {
		result = "error"
		code := uint32(USPErrMessageFailed)
		var ce *cpeerr.Error
		if errors.As(herr, &ce) && ce.FaultCode != 0 {
			code = uint32(ce.FaultCode)
		}
		resp = buildErrorMsg(reqHeader.GetMsgId(), code, herr.Error())
	}
	if opts.Metrics != nil {
		opts.Metrics.USPRequests.WithLabelValues(msgTypeLabel, result).Inc()
		opts.Metrics.USPRespRTT.WithLabelValues(msgTypeLabel).Observe(time.Since(start).Seconds())
	}
	if resp == nil {
		logger.Warn("usp handler returned nil response with no error", "msg_type", reqHeader.GetMsgType().String())
		return
	}
	if resp.GetHeader() == nil {
		resp.Header = &uspproto.Header{}
	}
	if resp.GetHeader().GetMsgId() == "" {
		resp.Header.MsgId = reqHeader.GetMsgId()
	}
	sendResponse(ctx, opts, record, resp, logger)
}

func sendResponse(ctx context.Context, opts Options, reqRecord *uspproto.Record, resp *uspproto.Msg, logger *slog.Logger) {
	wire, err := codec.WrapMessage(resp, opts.AgentEID, reqRecord.GetFromId())
	if err != nil {
		logger.Warn("usp wrap response failed", "err", err.Error())
		return
	}
	if err := opts.Adapter.Send(ctx, wire); err != nil {
		logger.Warn("usp send response failed", "err", err.Error())
		return
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
		logger.Warn("usp adapter close", "err", err.Error())
	}
}
