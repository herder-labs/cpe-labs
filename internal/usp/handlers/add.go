package handlers

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

type AddHandler struct {
	Tree        *paramtree.Tree
	UniqueKeys  map[string][][]string
	ValueChange func(path string)
	OnCreate    func(objPath string, uniqueKeys map[string]string)
}

func NewAdd(tree *paramtree.Tree, uniqueKeys map[string][][]string, valueChange func(string), onCreate func(string, map[string]string)) *AddHandler {
	return &AddHandler{Tree: tree, UniqueKeys: uniqueKeys, ValueChange: valueChange, OnCreate: onCreate}
}

func (h *AddHandler) MsgType() uspproto.Header_MsgType { return uspproto.Header_ADD }

func (h *AddHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetAdd()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.add", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not Add")}
	}

	results := make([]*uspproto.AddResp_CreatedObjectResult, 0, len(body.GetCreateObjs()))
	for _, co := range body.GetCreateObjs() {
		r := h.handleCreateObject(co)
		results = append(results, r)
	}

	resp := newMsg(uspproto.Header_ADD_RESP)
	resp.Body = &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_AddResp{
					AddResp: &uspproto.AddResp{CreatedObjResults: results},
				},
			},
		},
	}
	return resp, nil
}

func (h *AddHandler) handleCreateObject(co *uspproto.Add_CreateObject) *uspproto.AddResp_CreatedObjectResult {
	objPath := co.GetObjPath()
	instance, err := h.Tree.AddObject(objPath)
	if err != nil {
		return addFailure(objPath, session.USPErrInvalidPath, err.Error())
	}
	instantiatedPath := objPath + strconv.Itoa(instance) + "."

	if len(co.GetParamSettings()) > 0 {
		setters := make([]paramtree.Setter, 0, len(co.GetParamSettings()))
		for _, ps := range co.GetParamSettings() {
			leaf, gerr := h.Tree.Get(instantiatedPath + ps.GetParam())
			if gerr != nil {
				_ = h.Tree.DeleteObject(instantiatedPath)
				return addFailure(objPath, session.USPErrInvalidPath, fmt.Sprintf("param %s: %s", ps.GetParam(), gerr.Error()))
			}
			setters = append(setters, paramtree.Setter{
				Path:  instantiatedPath + ps.GetParam(),
				Value: paramtree.Value{Type: leaf.Type, Raw: ps.GetValue(), Writable: leaf.Writable},
			})
		}
		results, serr := h.Tree.SetBatch(setters)
		if serr != nil {
			var sbe *paramtree.SetBatchError
			code := session.USPErrInvalidArguments
			msg := serr.Error()
			if errors.As(serr, &sbe) {
				code = mapSetBatchCode(sbe.Code)
				msg = sbe.Err.Error()
			}
			_ = h.Tree.DeleteObject(instantiatedPath)
			return addFailure(objPath, code, msg)
		}
		if h.ValueChange != nil {
			for _, br := range results {
				if br.Changed {
					h.ValueChange(br.Path)
				}
			}
		}
	}

	uniqueKeys := h.readUniqueKeys(objPath, instantiatedPath)

	result := addSuccess(objPath, instantiatedPath, uniqueKeys)
	if h.OnCreate != nil {
		go h.OnCreate(instantiatedPath, uniqueKeys)
	}
	return result
}

func (h *AddHandler) readUniqueKeys(objPath, instantiatedPath string) map[string]string {
	out := map[string]string{}
	keySets, ok := h.UniqueKeys[objPath]
	if !ok {
		return out
	}
	for _, set := range keySets {
		for _, paramName := range set {
			v, err := h.Tree.Get(instantiatedPath + paramName)
			if err != nil {
				continue
			}
			out[paramName] = v.Raw
		}
	}
	return out
}

func addSuccess(requestedPath, instantiatedPath string, uniqueKeys map[string]string) *uspproto.AddResp_CreatedObjectResult {
	return &uspproto.AddResp_CreatedObjectResult{
		RequestedPath: requestedPath,
		OperStatus: &uspproto.AddResp_CreatedObjectResult_OperationStatus{
			OperStatus: &uspproto.AddResp_CreatedObjectResult_OperationStatus_OperSuccess{
				OperSuccess: &uspproto.AddResp_CreatedObjectResult_OperationStatus_OperationSuccess{
					InstantiatedPath: instantiatedPath,
					UniqueKeys:       uniqueKeys,
				},
			},
		},
	}
}

func addFailure(requestedPath string, code uint32, msg string) *uspproto.AddResp_CreatedObjectResult {
	return &uspproto.AddResp_CreatedObjectResult{
		RequestedPath: requestedPath,
		OperStatus: &uspproto.AddResp_CreatedObjectResult_OperationStatus{
			OperStatus: &uspproto.AddResp_CreatedObjectResult_OperationStatus_OperFailure{
				OperFailure: &uspproto.AddResp_CreatedObjectResult_OperationStatus_OperationFailure{
					ErrCode: code,
					ErrMsg:  msg,
				},
			},
		},
	}
}
