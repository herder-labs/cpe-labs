package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

func TestSetHandlerHappyPath(t *testing.T) {
	tree := buildWritableTree(t)
	var changed []string
	h := handlers.NewSet(tree, func(p string) { changed = append(changed, p) })

	req := buildSetRequest(false,
		updateObj("Device.WiFi.SSID.1.", "SSID", "HomeNet"),
		updateObj("Device.WiFi.SSID.1.", "Enable", "true"),
	)
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for i, r := range results {
		if r.GetOperStatus().GetOperSuccess() == nil {
			t.Errorf("[%d] OperSuccess nil, OperStatus=%+v", i, r.GetOperStatus())
		}
	}

	v, _ := tree.Get("Device.WiFi.SSID.1.SSID")
	if v.Raw != "HomeNet" {
		t.Errorf("SSID raw=%q want HomeNet", v.Raw)
	}
	if len(changed) != 2 {
		t.Errorf("valueChange calls=%d want 2", len(changed))
	}
}

func TestSetHandlerRollsBackOnInvalidValue(t *testing.T) {
	tree := buildWritableTree(t)
	h := handlers.NewSet(tree, nil)

	req := buildSetRequest(false,
		updateObj("Device.WiFi.SSID.1.", "SSID", "HomeNet"),
		updateObj("Device.WiFi.SSID.1.", "Channel", "not-a-number"),
	)
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 OperFailure result, got %d", len(results))
	}
	fail := results[0].GetOperStatus().GetOperFailure()
	if fail == nil {
		t.Fatalf("OperFailure nil")
	}
	if fail.GetErrCode() != session.USPErrInvalidArguments {
		t.Errorf("ErrCode=%d want %d", fail.GetErrCode(), session.USPErrInvalidArguments)
	}
	if len(fail.GetUpdatedInstFailures()) == 0 {
		t.Fatalf("UpdatedInstFailures empty")
	}
	pe := fail.GetUpdatedInstFailures()[0].GetParamErrs()
	if len(pe) == 0 || pe[0].GetParam() != "Channel" {
		t.Errorf("expected ParameterError for Channel, got %+v", pe)
	}

	v, _ := tree.Get("Device.WiFi.SSID.1.SSID")
	if v.Raw != "OriginalSSID" {
		t.Errorf("rollback failed: SSID raw=%q want OriginalSSID", v.Raw)
	}
}

func TestSetHandlerInvalidPath(t *testing.T) {
	tree := buildWritableTree(t)
	h := handlers.NewSet(tree, nil)

	req := buildSetRequest(false, updateObj("Device.Bogus.", "X", "Y"))
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()
	if len(results) != 1 || results[0].GetOperStatus().GetOperFailure() == nil {
		t.Fatalf("expected OperFailure, got %+v", results)
	}
	if results[0].GetOperStatus().GetOperFailure().GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d want %d", results[0].GetOperStatus().GetOperFailure().GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestSetHandlerRejectsAllowPartialTrue(t *testing.T) {
	tree := buildWritableTree(t)
	h := handlers.NewSet(tree, nil)

	req := buildSetRequest(true, updateObj("Device.WiFi.SSID.1.", "SSID", "X"))
	_, err := h.Handle(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for allow_partial=true")
	}
	var ce *cpeerr.Error
	if !errors.As(err, &ce) || ce.FaultCode != int(session.USPErrInvalidArguments) {
		t.Errorf("expected FaultCode 7008, got %v", err)
	}
}

func TestSetHandlerEmptyRequest(t *testing.T) {
	tree := buildWritableTree(t)
	h := handlers.NewSet(tree, nil)
	resp, err := h.Handle(context.Background(), buildSetRequest(false))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()) != 0 {
		t.Errorf("expected empty results")
	}
}

func TestSetHandlerNonWritableLeafRejects(t *testing.T) {
	tree := buildWritableTree(t)
	h := handlers.NewSet(tree, nil)
	req := buildSetRequest(false, updateObj("Device.DeviceInfo.", "SerialNumber", "NEWSN"))
	resp, _ := h.Handle(context.Background(), req)
	results := resp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()
	if results[0].GetOperStatus().GetOperFailure() == nil {
		t.Fatalf("expected OperFailure for non-writable leaf")
	}
	if results[0].GetOperStatus().GetOperFailure().GetErrCode() != session.USPErrInvalidArguments {
		t.Errorf("ErrCode=%d want %d", results[0].GetOperStatus().GetOperFailure().GetErrCode(), session.USPErrInvalidArguments)
	}
}

func buildWritableTree(t *testing.T) *paramtree.Tree {
	t.Helper()
	tree := paramtree.New()
	mount := func(p string, typ paramtree.Type, raw string, writable bool) {
		if err := tree.Mount(p, paramtree.NewLeaf(paramtree.Value{Type: typ, Raw: raw, Writable: writable})); err != nil {
			t.Fatalf("Mount %s: %v", p, err)
		}
	}
	mount("Device.DeviceInfo.SerialNumber", paramtree.TypeString, "ORIG", false)
	mount("Device.WiFi.SSID.1.SSID", paramtree.TypeString, "OriginalSSID", true)
	mount("Device.WiFi.SSID.1.Enable", paramtree.TypeBoolean, "false", true)
	mount("Device.WiFi.SSID.1.Channel", paramtree.TypeUnsignedInt, "6", true)
	return tree
}

func buildSetRequest(allowPartial bool, updateObjs ...*uspproto.Set_UpdateObject) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_SET},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Set{
						Set: &uspproto.Set{AllowPartial: allowPartial, UpdateObjs: updateObjs},
					},
				},
			},
		},
	}
}

func updateObj(objPath string, paramVals ...string) *uspproto.Set_UpdateObject {
	uo := &uspproto.Set_UpdateObject{ObjPath: objPath}
	for i := 0; i < len(paramVals); i += 2 {
		uo.ParamSettings = append(uo.ParamSettings, &uspproto.Set_UpdateParamSetting{
			Param: paramVals[i], Value: paramVals[i+1], Required: true,
		})
	}
	return uo
}
