package handlers

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

const DefaultDataModelURI = "urn:broadband-forum-org:tr-181-2"

type GetSupportedDMHandler struct {
	Tree           *paramtree.Tree
	UniqueKeys     map[string][][]string
	DataModelURI   string
}

func NewGetSupportedDM(tree *paramtree.Tree, uniqueKeys map[string][][]string, dataModelURI string) *GetSupportedDMHandler {
	if dataModelURI == "" {
		dataModelURI = DefaultDataModelURI
	}
	return &GetSupportedDMHandler{Tree: tree, UniqueKeys: uniqueKeys, DataModelURI: dataModelURI}
}

func (h *GetSupportedDMHandler) MsgType() uspproto.Header_MsgType {
	return uspproto.Header_GET_SUPPORTED_DM
}

func (h *GetSupportedDMHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetGetSupportedDm()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.get_supported_dm", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not GetSupportedDM")}
	}

	results := make([]*uspproto.GetSupportedDMResp_RequestedObjectResult, 0, len(body.GetObjPaths()))
	for _, p := range body.GetObjPaths() {
		results = append(results, h.resolvePath(p, body))
	}

	resp := newMsg(uspproto.Header_GET_SUPPORTED_DM_RESP)
	resp.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_GetSupportedDmResp{
					GetSupportedDmResp: &uspproto.GetSupportedDMResp{ReqObjResults: results},
				},
			},
		},
	}
	return resp, nil
}

func (h *GetSupportedDMHandler) resolvePath(p string, req *uspproto.GetSupportedDM) *uspproto.GetSupportedDMResp_RequestedObjectResult {
	r := &uspproto.GetSupportedDMResp_RequestedObjectResult{
		ReqObjPath:       p,
		DataModelInstUri: h.DataModelURI,
	}
	if !strings.HasSuffix(p, ".") {
		r.ErrCode = session.USPErrInvalidPath
		r.ErrMsg = "Invalid path: GetSupportedDM paths must end with '.'"
		return r
	}
	prefix := strings.TrimSuffix(p, ".")

	// Group leaves by their parent object path. Walk depth 0 = unlimited.
	depth := 0
	if req.GetFirstLevelOnly() {
		depth = 2
	}
	byObject := make(map[string][]leafInfo)
	tableSeen := make(map[string]bool)
	err := h.Tree.Walk(prefix, depth, func(path string, v paramtree.Value) error {
		parent := parentObjectFromPath(path, p)
		byObject[parent] = append(byObject[parent], leafInfo{name: leafSegment(path), value: v})
		// Mark every multi-instance parent so we can flag is_multi_instance.
		markTables(parent, tableSeen, h.Tree)
		return nil
	})
	if err != nil {
		r.ErrCode = session.USPErrInvalidPath
		r.ErrMsg = "Invalid path: " + err.Error()
		return r
	}

	// Also include the requested-path object itself even when it has no
	// direct leaves (e.g. requesting Device.WiFi.SSID. when only
	// children below it carry leaves).
	if _, ok := byObject[p]; !ok {
		if _, err := h.Tree.Children(prefix); err == nil {
			byObject[p] = nil
			markTables(p, tableSeen, h.Tree)
		}
	}

	objPaths := make([]string, 0, len(byObject))
	for op := range byObject {
		objPaths = append(objPaths, op)
	}
	sort.Strings(objPaths)

	want := req.GetReturnParams() || (!req.GetReturnCommands() && !req.GetReturnEvents() && !req.GetReturnParams())
	for _, op := range objPaths {
		leaves := byObject[op]
		r.SupportedObjs = append(r.SupportedObjs, h.buildSupportedObject(op, leaves, tableSeen, want))
	}
	return r
}

func (h *GetSupportedDMHandler) buildSupportedObject(objPath string, leaves []leafInfo, tableSeen map[string]bool, returnParams bool) *uspproto.GetSupportedDMResp_SupportedObjectResult {
	access := uspproto.GetSupportedDMResp_OBJ_READ_ONLY
	isMulti := false
	if tableSeen[objPath] {
		isMulti = true
		access = uspproto.GetSupportedDMResp_OBJ_ADD_DELETE
	}

	so := &uspproto.GetSupportedDMResp_SupportedObjectResult{
		SupportedObjPath: objPath,
		Access:           access,
		IsMultiInstance:  isMulti,
	}

	if returnParams {
		sort.Slice(leaves, func(i, j int) bool { return leaves[i].name < leaves[j].name })
		for _, lf := range leaves {
			so.SupportedParams = append(so.SupportedParams, &uspproto.GetSupportedDMResp_SupportedParamResult{
				ParamName:   lf.name,
				Access:      paramAccess(lf.value.Writable),
				ValueType:   paramValueType(lf.value.Type),
				ValueChange: uspproto.GetSupportedDMResp_VALUE_CHANGE_ALLOWED,
			})
		}
	}

	if keySets, ok := h.UniqueKeys[objPath]; ok {
		for _, set := range keySets {
			so.UniqueKeySets = append(so.UniqueKeySets, &uspproto.GetSupportedDMResp_SupportedUniqueKeySet{
				KeyNames: append([]string(nil), set...),
			})
		}
	}

	return so
}

type leafInfo struct {
	name  string
	value paramtree.Value
}

// parentObjectFromPath returns the parent-object path (with trailing
// ".") for a fully-qualified leaf path, normalised so instance numbers
// in the path are collapsed to the table-template path. For example:
//
//	Device.WiFi.SSID.1.SSID  -> Device.WiFi.SSID.
//	Device.DeviceInfo.UpTime -> Device.DeviceInfo.
func parentObjectFromPath(leafPath, requestedPath string) string {
	idx := strings.LastIndex(leafPath, ".")
	if idx < 0 {
		return requestedPath
	}
	prefix := leafPath[:idx+1]
	// Collapse trailing instance number (e.g. ".1.") into the
	// template-shape path (".") so all instances of one table contribute
	// to the same SupportedObjectResult.
	segs := strings.Split(strings.TrimSuffix(prefix, "."), ".")
	for i, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil {
			segs = segs[:i]
			break
		}
	}
	return strings.Join(segs, ".") + "."
}

func leafSegment(leafPath string) string {
	idx := strings.LastIndex(leafPath, ".")
	if idx < 0 {
		return leafPath
	}
	return leafPath[idx+1:]
}

// markTables records every ancestor that is a multi-instance table.
// Walks from the requested-object path upward by stripping trailing
// segments; for each interior path that IsAddDeletable, mark its
// canonical template path.
func markTables(objPath string, seen map[string]bool, tree *paramtree.Tree) {
	trimmed := strings.TrimSuffix(objPath, ".")
	if trimmed == "" {
		return
	}
	if tree.IsAddDeletable(trimmed) {
		seen[objPath] = true
	}
}

func paramAccess(writable bool) uspproto.GetSupportedDMResp_ParamAccessType {
	if writable {
		return uspproto.GetSupportedDMResp_PARAM_READ_WRITE
	}
	return uspproto.GetSupportedDMResp_PARAM_READ_ONLY
}

func paramValueType(t paramtree.Type) uspproto.GetSupportedDMResp_ParamValueType {
	switch t {
	case paramtree.TypeString:
		return uspproto.GetSupportedDMResp_PARAM_STRING
	case paramtree.TypeInt:
		return uspproto.GetSupportedDMResp_PARAM_INT
	case paramtree.TypeUnsignedInt:
		return uspproto.GetSupportedDMResp_PARAM_UNSIGNED_INT
	case paramtree.TypeBoolean:
		return uspproto.GetSupportedDMResp_PARAM_BOOLEAN
	case paramtree.TypeDateTime:
		return uspproto.GetSupportedDMResp_PARAM_DATE_TIME
	case paramtree.TypeBase64:
		return uspproto.GetSupportedDMResp_PARAM_BASE_64
	default:
		return uspproto.GetSupportedDMResp_PARAM_UNKNOWN
	}
}
