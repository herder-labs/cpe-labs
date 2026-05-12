package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

const getInstancesProfile = `
objects:
  - path: Device.WiFi.SSID
    instances: 3
    uniqueKeys:
      - [SSID]
    parameters:
      - path: SSID
        value: "ssid-{i}"
        writable: true
      - path: Enable
        type: xsd:boolean
        value: "true"
        writable: true
`

func TestGetInstancesHandlerHappyPath(t *testing.T) {
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(getInstancesProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	h := handlers.NewGetInstances(p.Tree, p.UniqueKeys)

	resp, err := h.Handle(context.Background(), buildGetInstancesRequest(false, "Device.WiFi.SSID."))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetGetInstancesResp().GetReqPathResults()
	if len(results) != 1 {
		t.Fatalf("got %d req_path_results", len(results))
	}
	insts := results[0].GetCurrInsts()
	if len(insts) != 3 {
		t.Fatalf("got %d insts, want 3", len(insts))
	}
	for _, inst := range insts {
		if !strings.HasPrefix(inst.GetInstantiatedObjPath(), "Device.WiFi.SSID.") {
			t.Errorf("InstantiatedObjPath=%q", inst.GetInstantiatedObjPath())
		}
		if inst.GetUniqueKeys()["SSID"] == "" {
			t.Errorf("UniqueKeys[SSID] empty: %+v", inst.GetUniqueKeys())
		}
	}
}

func TestGetInstancesHandlerRejectsMissingTrailingDot(t *testing.T) {
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(getInstancesProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	h := handlers.NewGetInstances(p.Tree, p.UniqueKeys)

	resp, _ := h.Handle(context.Background(), buildGetInstancesRequest(false, "Device.WiFi.SSID"))
	r := resp.GetBody().GetResponse().GetGetInstancesResp().GetReqPathResults()[0]
	if r.GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d want %d", r.GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestGetInstancesHandlerUnknownPath(t *testing.T) {
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(getInstancesProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	h := handlers.NewGetInstances(p.Tree, p.UniqueKeys)

	resp, _ := h.Handle(context.Background(), buildGetInstancesRequest(false, "Device.DoesNot."))
	r := resp.GetBody().GetResponse().GetGetInstancesResp().GetReqPathResults()[0]
	if r.GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d want %d", r.GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestGetInstancesHandlerNoInstances(t *testing.T) {
	tree := paramtree.New()
	if err := tree.AddTable("Device.WiFi.SSID", paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: "", Writable: true})); err != nil {
		t.Fatalf("AddTable: %v", err)
	}
	h := handlers.NewGetInstances(tree, nil)

	resp, _ := h.Handle(context.Background(), buildGetInstancesRequest(false, "Device.WiFi.SSID."))
	r := resp.GetBody().GetResponse().GetGetInstancesResp().GetReqPathResults()[0]
	if r.GetErrCode() != 0 {
		t.Fatalf("ErrCode=%d want 0 (empty table is success with no insts)", r.GetErrCode())
	}
	if len(r.GetCurrInsts()) != 0 {
		t.Errorf("expected 0 insts, got %d", len(r.GetCurrInsts()))
	}
}

func buildGetInstancesRequest(firstLevelOnly bool, paths ...string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_GET_INSTANCES},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_GetInstances{
						GetInstances: &uspproto.GetInstances{
							ObjPaths:       paths,
							FirstLevelOnly: firstLevelOnly,
						},
					},
				},
			},
		},
	}
}
