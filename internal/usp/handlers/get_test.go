package handlers_test

import (
	"context"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

func TestGetHandlerHappyPath(t *testing.T) {
	tree := buildDeviceInfoTree(t)
	h := handlers.NewGet(tree)

	req := buildGetRequest("Device.DeviceInfo.SerialNumber", "Device.DeviceInfo.Manufacturer")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	results := resp.GetBody().GetResponse().GetGetResp().GetReqPathResults()
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	if results[0].GetRequestedPath() != "Device.DeviceInfo.SerialNumber" {
		t.Errorf("[0] RequestedPath=%q", results[0].GetRequestedPath())
	}
	if results[0].GetErrCode() != 0 {
		t.Errorf("[0] ErrCode=%d", results[0].GetErrCode())
	}
	rp := results[0].GetResolvedPathResults()
	if len(rp) != 1 {
		t.Fatalf("[0] ResolvedPathResults len=%d", len(rp))
	}
	if rp[0].GetResolvedPath() != "Device.DeviceInfo." {
		t.Errorf("[0] ResolvedPath=%q", rp[0].GetResolvedPath())
	}
	if got := rp[0].GetResultParams()["SerialNumber"]; got != "SN123" {
		t.Errorf("[0] SerialNumber=%q want SN123", got)
	}

	if results[1].GetResolvedPathResults()[0].GetResultParams()["Manufacturer"] != "ACME" {
		t.Errorf("[1] Manufacturer mismatch")
	}
}

func TestGetHandlerPerPathErrorMix(t *testing.T) {
	tree := buildDeviceInfoTree(t)
	h := handlers.NewGet(tree)

	req := buildGetRequest("Device.DeviceInfo.SerialNumber", "Device.DoesNot.Exist")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetGetResp().GetReqPathResults()
	if results[0].GetErrCode() != 0 {
		t.Errorf("[0] should be success, got code %d", results[0].GetErrCode())
	}
	if results[1].GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("[1] ErrCode=%d want %d", results[1].GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestGetHandlerRejectsObjectPath(t *testing.T) {
	tree := buildDeviceInfoTree(t)
	h := handlers.NewGet(tree)

	req := buildGetRequest("Device.DeviceInfo.")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetGetResp().GetReqPathResults()
	if results[0].GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d want %d (non-leaf paths rejected in v0)", results[0].GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestGetHandlerRejectsWildcard(t *testing.T) {
	tree := buildDeviceInfoTree(t)
	h := handlers.NewGet(tree)

	req := buildGetRequest("Device.WiFi.SSID.*.SSID")
	resp, _ := h.Handle(context.Background(), req)
	results := resp.GetBody().GetResponse().GetGetResp().GetReqPathResults()
	if results[0].GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d want %d", results[0].GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestGetHandlerPreservesInputOrder(t *testing.T) {
	tree := buildDeviceInfoTree(t)
	h := handlers.NewGet(tree)

	req := buildGetRequest("Device.DeviceInfo.Manufacturer", "Device.DeviceInfo.SerialNumber", "Device.DeviceInfo.ManufacturerOUI")
	resp, _ := h.Handle(context.Background(), req)
	results := resp.GetBody().GetResponse().GetGetResp().GetReqPathResults()
	want := []string{"Device.DeviceInfo.Manufacturer", "Device.DeviceInfo.SerialNumber", "Device.DeviceInfo.ManufacturerOUI"}
	for i, w := range want {
		if results[i].GetRequestedPath() != w {
			t.Errorf("[%d] = %q want %q", i, results[i].GetRequestedPath(), w)
		}
	}
}

func buildDeviceInfoTree(t *testing.T) *paramtree.Tree {
	t.Helper()
	tree := paramtree.New()
	mount := func(p, v string) {
		if err := tree.Mount(p, paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: v, Writable: false})); err != nil {
			t.Fatalf("Mount %s: %v", p, err)
		}
	}
	mount("Device.DeviceInfo.Manufacturer", "ACME")
	mount("Device.DeviceInfo.ManufacturerOUI", "AABBCC")
	mount("Device.DeviceInfo.ProductClass", "TestModel")
	mount("Device.DeviceInfo.SerialNumber", "SN123")
	return tree
}

func buildGetRequest(paths ...string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_GET},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Get{
						Get: &uspproto.Get{ParamPaths: paths},
					},
				},
			},
		},
	}
}
