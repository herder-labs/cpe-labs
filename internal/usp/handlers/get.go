package handlers

import (
	"context"
	"errors"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

type GetHandler struct {
	Tree *paramtree.Tree
}

func NewGet(tree *paramtree.Tree) *GetHandler { return &GetHandler{Tree: tree} }

func (h *GetHandler) MsgType() uspproto.Header_MsgType { return uspproto.Header_GET }

func (h *GetHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetGet()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.get", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not Get")}
	}

	results := make([]*uspproto.GetResp_RequestedPathResult, 0, len(body.GetParamPaths()))
	for _, p := range body.GetParamPaths() {
		results = append(results, resolveGetPath(h.Tree, p))
	}

	resp := newMsg(uspproto.Header_GET_RESP)
	resp.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_GetResp{
					GetResp: &uspproto.GetResp{ReqPathResults: results},
				},
			},
		},
	}
	return resp, nil
}

func resolveGetPath(tree *paramtree.Tree, path string) *uspproto.GetResp_RequestedPathResult {
	r := &uspproto.GetResp_RequestedPathResult{RequestedPath: path}
	if !isConcreteScalarPath(path) {
		r.ErrCode = session.USPErrInvalidPath
		r.ErrMsg = "Invalid path: only concrete scalar paths are supported"
		return r
	}
	v, err := tree.Get(path)
	if err != nil {
		r.ErrCode = session.USPErrInvalidPath
		r.ErrMsg = "Invalid path: " + err.Error()
		return r
	}
	r.ResolvedPathResults = []*uspproto.GetResp_ResolvedPathResult{
		{
			ResolvedPath: parentObjectPath(path),
			ResultParams: map[string]string{leafName(path): v.Raw},
		},
	}
	return r
}
