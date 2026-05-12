package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

type SetHandler struct {
	Tree        *paramtree.Tree
	ValueChange func(path string)
}

func NewSet(tree *paramtree.Tree, valueChange func(string)) *SetHandler {
	return &SetHandler{Tree: tree, ValueChange: valueChange}
}

func (h *SetHandler) MsgType() uspproto.Header_MsgType { return uspproto.Header_SET }

func (h *SetHandler) Handle(_ context.Context, req *uspproto.Msg) (*uspproto.Msg, error) {
	body := req.GetBody().GetRequest().GetSet()
	if body == nil {
		return nil, &cpeerr.Error{Op: "usp.set", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments), Err: errors.New("body is not Set")}
	}

	if body.GetAllowPartial() {
		return nil, &cpeerr.Error{
			Op: "usp.set", Kind: cpeerr.KindInvalidArgument, FaultCode: int(session.USPErrInvalidArguments),
			Err: errors.New("allow_partial=true is not supported in this build"),
		}
	}

	updateObjs := body.GetUpdateObjs()
	if len(updateObjs) == 0 {
		return buildSetRespEmpty(), nil
	}

	// Build a single atomic batch across every UpdateObject.
	setters := make([]paramtree.Setter, 0)
	owner := make(map[string]int) // canonical path -> index into updateObjs
	for i, uo := range updateObjs {
		objPath := uo.GetObjPath()
		for _, ps := range uo.GetParamSettings() {
			fullPath := objPath + ps.GetParam()
			leaf, gerr := h.Tree.Get(fullPath)
			if gerr != nil {
				return buildSetRespFailure(updateObjs, i, ps.GetParam(), session.USPErrInvalidPath, gerr.Error()), nil
			}
			setters = append(setters, paramtree.Setter{
				Path:  fullPath,
				Value: paramtree.Value{Type: leaf.Type, Raw: ps.GetValue(), Writable: leaf.Writable},
			})
			owner[fullPath] = i
		}
	}

	results, err := h.Tree.SetBatch(setters)
	if err != nil {
		var sbe *paramtree.SetBatchError
		if errors.As(err, &sbe) {
			objIdx := owner[sbe.Path]
			paramName := strings.TrimPrefix(sbe.Path, updateObjs[objIdx].GetObjPath())
			return buildSetRespFailure(updateObjs, objIdx, paramName, mapSetBatchCode(sbe.Code), sbe.Err.Error()), nil
		}
		return nil, &cpeerr.Error{Op: "usp.set", Kind: cpeerr.KindInternal, FaultCode: int(session.USPErrMessageFailed), Err: err}
	}

	if h.ValueChange != nil {
		for _, br := range results {
			if br.Changed {
				h.ValueChange(br.Path)
			}
		}
	}

	return buildSetRespSuccess(updateObjs), nil
}

func buildSetRespEmpty() *uspproto.Msg {
	resp := newMsg(uspproto.Header_SET_RESP)
	resp.Body = setRespBody(nil)
	return resp
}

func buildSetRespSuccess(updateObjs []*uspproto.Set_UpdateObject) *uspproto.Msg {
	results := make([]*uspproto.SetResp_UpdatedObjectResult, 0, len(updateObjs))
	for _, uo := range updateObjs {
		updatedParams := map[string]string{}
		for _, ps := range uo.GetParamSettings() {
			updatedParams[ps.GetParam()] = ps.GetValue()
		}
		results = append(results, &uspproto.SetResp_UpdatedObjectResult{
			RequestedPath: uo.GetObjPath(),
			OperStatus: &uspproto.SetResp_UpdatedObjectResult_OperationStatus{
				OperStatus: &uspproto.SetResp_UpdatedObjectResult_OperationStatus_OperSuccess{
					OperSuccess: &uspproto.SetResp_UpdatedObjectResult_OperationStatus_OperationSuccess{
						UpdatedInstResults: []*uspproto.SetResp_UpdatedInstanceResult{
							{
								AffectedPath:  uo.GetObjPath(),
								UpdatedParams: updatedParams,
							},
						},
					},
				},
			},
		})
	}
	resp := newMsg(uspproto.Header_SET_RESP)
	resp.Body = setRespBody(results)
	return resp
}

func buildSetRespFailure(updateObjs []*uspproto.Set_UpdateObject, failingObjIdx int, failingParam string, errCode uint32, errMsg string) *uspproto.Msg {
	failing := updateObjs[failingObjIdx]
	results := []*uspproto.SetResp_UpdatedObjectResult{
		{
			RequestedPath: failing.GetObjPath(),
			OperStatus: &uspproto.SetResp_UpdatedObjectResult_OperationStatus{
				OperStatus: &uspproto.SetResp_UpdatedObjectResult_OperationStatus_OperFailure{
					OperFailure: &uspproto.SetResp_UpdatedObjectResult_OperationStatus_OperationFailure{
						ErrCode: errCode,
						ErrMsg:  fmt.Sprintf("Set failed at %s: %s", failingParam, errMsg),
						UpdatedInstFailures: []*uspproto.SetResp_UpdatedInstanceFailure{
							{
								AffectedPath: failing.GetObjPath(),
								ParamErrs: []*uspproto.SetResp_ParameterError{
									{Param: failingParam, ErrCode: errCode, ErrMsg: errMsg},
								},
							},
						},
					},
				},
			},
		},
	}
	resp := newMsg(uspproto.Header_SET_RESP)
	resp.Body = setRespBody(results)
	return resp
}

func setRespBody(results []*uspproto.SetResp_UpdatedObjectResult) *uspproto.Body {
	return &uspproto.Body{
		MsgBody: &uspproto.Body_Response{
			Response: &uspproto.Response{
				RespType: &uspproto.Response_SetResp{
					SetResp: &uspproto.SetResp{UpdatedObjResults: results},
				},
			},
		},
	}
}

func mapSetBatchCode(c paramtree.SetBatchFailure) uint32 {
	switch c {
	case paramtree.FailureNotFound:
		return session.USPErrInvalidPath
	case paramtree.FailureNotWritable, paramtree.FailureTypeMismatch, paramtree.FailureInvalidValue:
		return session.USPErrInvalidArguments
	case paramtree.FailureDuplicatePath:
		return session.USPErrInvalidArguments
	default:
		return session.USPErrMessageFailed
	}
}
