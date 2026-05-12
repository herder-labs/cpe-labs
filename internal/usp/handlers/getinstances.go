package handlers

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

type GetInstancesHandler struct {
	Tree       *paramtree.Tree
	UniqueKeys map[string][][]string
}

func NewGetInstances(tree *paramtree.Tree, uniqueKeys map[string][][]string) *GetInstancesHandler {
	return &GetInstancesHandler{Tree: tree, UniqueKeys: uniqueKeys}
}

func (h *GetInstancesHandler) MsgType() uspproto.Header_MsgType {
	return uspproto.Header_GET_INSTANCES
}

func (h *GetInstancesHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetGetInstances()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.get_instances", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not GetInstances")}
	}

	results := make([]*uspproto.GetInstancesResp_RequestedPathResult, 0, len(body.GetObjPaths()))
	for _, p := range body.GetObjPaths() {
		results = append(results, h.resolvePath(p, body.GetFirstLevelOnly()))
	}

	resp := newMsg(uspproto.Header_GET_INSTANCES_RESP)
	resp.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_GetInstancesResp{
					GetInstancesResp: &uspproto.GetInstancesResp{ReqPathResults: results},
				},
			},
		},
	}
	return resp, nil
}

func (h *GetInstancesHandler) resolvePath(p string, firstLevelOnly bool) *uspproto.GetInstancesResp_RequestedPathResult {
	r := &uspproto.GetInstancesResp_RequestedPathResult{RequestedPath: p}
	if !strings.HasSuffix(p, ".") {
		r.ErrCode = session.USPErrInvalidPath
		r.ErrMsg = "Invalid path: GetInstances paths must end with '.'"
		return r
	}
	prefix := strings.TrimSuffix(p, ".")
	if _, err := h.Tree.Children(prefix); err != nil {
		r.ErrCode = session.USPErrInvalidPath
		r.ErrMsg = "Invalid path: " + err.Error()
		return r
	}
	r.CurrInsts = h.collectInstances(p, firstLevelOnly)
	return r
}

func (h *GetInstancesHandler) collectInstances(objPath string, firstLevelOnly bool) []*uspproto.GetInstancesResp_CurrInstance {
	insts := make([]*uspproto.GetInstancesResp_CurrInstance, 0)
	prefix := strings.TrimSuffix(objPath, ".")
	children, err := h.Tree.Children(prefix)
	if err != nil {
		return insts
	}
	for _, c := range children {
		if !strings.HasSuffix(c.Name, ".") {
			continue
		}
		// c.Name is the full path of the child (e.g.
		// "Device.WiFi.SSID.1."). Extract the trailing segment after
		// the parent prefix.
		segmentName := strings.TrimSuffix(strings.TrimPrefix(c.Name, objPath), ".")
		if _, perr := strconv.Atoi(segmentName); perr != nil {
			continue
		}
		instPath := c.Name
		insts = append(insts, &uspproto.GetInstancesResp_CurrInstance{
			InstantiatedObjPath: instPath,
			UniqueKeys:          h.readUniqueKeys(objPath, instPath),
		})
		if firstLevelOnly {
			continue
		}
		// Recurse into nested multi-instance children.
		nested := h.collectNestedTables(instPath)
		for _, np := range nested {
			insts = append(insts, h.collectInstances(np, false)...)
		}
	}
	return insts
}

// collectNestedTables walks one level into instPath looking for child
// object paths that are themselves multi-instance tables (registered
// via AddTable; IsAddDeletable reports true).
func (h *GetInstancesHandler) collectNestedTables(instPath string) []string {
	out := []string{}
	prefix := strings.TrimSuffix(instPath, ".")
	children, err := h.Tree.Children(prefix)
	if err != nil {
		return out
	}
	for _, c := range children {
		if !strings.HasSuffix(c.Name, ".") {
			continue
		}
		segmentName := strings.TrimSuffix(strings.TrimPrefix(c.Name, instPath), ".")
		if _, perr := strconv.Atoi(segmentName); perr == nil {
			// Skip instance segments (those are direct children of
			// instPath when the table sits inside).
			continue
		}
		if h.Tree.IsAddDeletable(strings.TrimSuffix(c.Name, ".")) {
			out = append(out, c.Name)
		}
	}
	return out
}

func (h *GetInstancesHandler) readUniqueKeys(objPath, instPath string) map[string]string {
	out := map[string]string{}
	keySets, ok := h.UniqueKeys[objPath]
	if !ok {
		return out
	}
	for _, set := range keySets {
		for _, paramName := range set {
			v, err := h.Tree.Get(instPath + paramName)
			if err != nil {
				continue
			}
			out[paramName] = v.Raw
		}
	}
	return out
}
