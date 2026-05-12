package handlers

import (
	"context"
	"errors"
	"strings"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

type DeleteHandler struct {
	Tree     *paramtree.Tree
	OnDelete func(objPath string)
}

func NewDelete(tree *paramtree.Tree, onDelete func(string)) *DeleteHandler {
	return &DeleteHandler{Tree: tree, OnDelete: onDelete}
}

func (h *DeleteHandler) MsgType() uspproto.Header_MsgType { return uspproto.Header_DELETE }

func (h *DeleteHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetDelete()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.delete", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not Delete")}
	}

	results := make([]*uspproto.DeleteResp_DeletedObjectResult, 0, len(body.GetObjPaths()))
	for _, p := range body.GetObjPaths() {
		results = append(results, h.handleDeletePath(p))
	}

	resp := newMsg(uspproto.Header_DELETE_RESP)
	resp.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_DeleteResp{
					DeleteResp: &uspproto.DeleteResp{DeletedObjResults: results},
				},
			},
		},
	}
	return resp, nil
}

func (h *DeleteHandler) handleDeletePath(p string) *uspproto.DeleteResp_DeletedObjectResult {
	if !strings.HasSuffix(p, ".") {
		return deleteFailure(p, session.USPErrInvalidPath, "Invalid path: object paths must end with '.'")
	}

	if err := h.Tree.DeleteObject(strings.TrimSuffix(p, ".")); err != nil {
		var ce *cpeerr.Error
		if errors.As(err, &ce) && ce.Kind == cpeerr.KindNotFound {
			return deleteFailure(p, session.USPErrObjectDoesNotExist, err.Error())
		}
		return deleteFailure(p, session.USPErrCommandFailure, err.Error())
	}

	affected := []string{p}
	result := &uspproto.DeleteResp_DeletedObjectResult{
		RequestedPath: p,
		OperStatus: &uspproto.DeleteResp_DeletedObjectResult_OperationStatus{
			OperStatus: &uspproto.DeleteResp_DeletedObjectResult_OperationStatus_OperSuccess{
				OperSuccess: &uspproto.DeleteResp_DeletedObjectResult_OperationStatus_OperationSuccess{
					AffectedPaths: affected,
				},
			},
		},
	}
	if h.OnDelete != nil {
		go h.OnDelete(p)
	}
	return result
}

func deleteFailure(requestedPath string, code uint32, msg string) *uspproto.DeleteResp_DeletedObjectResult {
	return &uspproto.DeleteResp_DeletedObjectResult{
		RequestedPath: requestedPath,
		OperStatus: &uspproto.DeleteResp_DeletedObjectResult_OperationStatus{
			OperStatus: &uspproto.DeleteResp_DeletedObjectResult_OperationStatus_OperFailure{
				OperFailure: &uspproto.DeleteResp_DeletedObjectResult_OperationStatus_OperationFailure{
					ErrCode: code,
					ErrMsg:  msg,
				},
			},
		},
	}
}
