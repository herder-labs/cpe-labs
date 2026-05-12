package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
)

const supportedDMSmallProfile = `
parameters:
  - path: Device.DeviceInfo.SerialNumber
    value: "SN123"
  - path: Device.DeviceInfo.Manufacturer
    value: "ACME"
  - path: Device.ManagementServer.URL
    value: "http://example/cwmp"
    writable: true

objects:
  - path: Device.WiFi.SSID
    instances: 2
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

func TestGetSupportedDMHandlerSmallProfile(t *testing.T) {
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(supportedDMSmallProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	h := handlers.NewGetSupportedDM(p.Tree, p.UniqueKeys, "")

	resp, err := h.Handle(context.Background(), buildGetSupportedDMRequest(false, "Device."))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetGetSupportedDmResp().GetReqObjResults()
	if len(results) != 1 {
		t.Fatalf("got %d req_obj_results, want 1", len(results))
	}
	r := results[0]
	if r.GetDataModelInstUri() != handlers.DefaultDataModelURI {
		t.Errorf("DataModelInstUri=%q", r.GetDataModelInstUri())
	}

	byPath := indexSupportedObjs(r.GetSupportedObjs())
	dev, ok := byPath["Device.DeviceInfo."]
	if !ok {
		t.Fatalf("missing Device.DeviceInfo.; have: %v", keysSorted(byPath))
	}
	if dev.GetIsMultiInstance() {
		t.Errorf("Device.DeviceInfo. should not be multi-instance")
	}
	if dev.GetAccess() != uspproto.GetSupportedDMResp_OBJ_READ_ONLY {
		t.Errorf("Device.DeviceInfo. access=%v want OBJ_READ_ONLY", dev.GetAccess())
	}

	ssid, ok := byPath["Device.WiFi.SSID."]
	if !ok {
		t.Fatalf("missing Device.WiFi.SSID.; have: %v", keysSorted(byPath))
	}
	if !ssid.GetIsMultiInstance() {
		t.Errorf("Device.WiFi.SSID. should be multi-instance")
	}
	if ssid.GetAccess() != uspproto.GetSupportedDMResp_OBJ_ADD_DELETE {
		t.Errorf("Device.WiFi.SSID. access=%v want OBJ_ADD_DELETE", ssid.GetAccess())
	}
	if len(ssid.GetUniqueKeySets()) != 1 || ssid.GetUniqueKeySets()[0].GetKeyNames()[0] != "SSID" {
		t.Errorf("UniqueKeySets=%+v", ssid.GetUniqueKeySets())
	}

	enableFound := false
	for _, pr := range ssid.GetSupportedParams() {
		if pr.GetParamName() == "Enable" {
			enableFound = true
			if pr.GetValueType() != uspproto.GetSupportedDMResp_PARAM_BOOLEAN {
				t.Errorf("SSID.Enable ValueType=%v want PARAM_BOOLEAN", pr.GetValueType())
			}
			if pr.GetAccess() != uspproto.GetSupportedDMResp_PARAM_READ_WRITE {
				t.Errorf("SSID.Enable Access=%v want PARAM_READ_WRITE", pr.GetAccess())
			}
		}
	}
	if !enableFound {
		t.Errorf("Enable param not in SSID supported_params")
	}
}

func TestGetSupportedDMHandlerRejectsMissingTrailingDot(t *testing.T) {
	tree := paramtree.New()
	if err := tree.Mount("Device.X", paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: "y"})); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewGetSupportedDM(tree, nil, "")
	resp, _ := h.Handle(context.Background(), buildGetSupportedDMRequest(false, "Device"))
	r := resp.GetBody().GetResponse().GetGetSupportedDmResp().GetReqObjResults()[0]
	if r.GetErrCode() == 0 {
		t.Fatalf("expected non-zero err_code")
	}
}

func TestGetSupportedDMHandlerReferenceProfileScale(t *testing.T) {
	p, err := paramtree.LoadProfile("../../../profiles/example-tr181-gateway")
	if err != nil {
		t.Skipf("reference profile load failed (acceptable when run outside repo root): %v", err)
	}
	h := handlers.NewGetSupportedDM(p.Tree, p.UniqueKeys, "")

	resp, err := h.Handle(context.Background(), buildGetSupportedDMRequest(false, "Device."))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	r := resp.GetBody().GetResponse().GetGetSupportedDmResp().GetReqObjResults()[0]
	totalParams := 0
	for _, so := range r.GetSupportedObjs() {
		totalParams += len(so.GetSupportedParams())
	}
	if totalParams < 100 {
		t.Errorf("totalParams=%d want ≥100 against the reference TR-181 profile", totalParams)
	}
}

func buildGetSupportedDMRequest(firstLevelOnly bool, paths ...string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_GET_SUPPORTED_DM},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_GetSupportedDm{
						GetSupportedDm: &uspproto.GetSupportedDM{
							ObjPaths:       paths,
							FirstLevelOnly: firstLevelOnly,
							ReturnParams:   true,
						},
					},
				},
			},
		},
	}
}

func indexSupportedObjs(objs []*uspproto.GetSupportedDMResp_SupportedObjectResult) map[string]*uspproto.GetSupportedDMResp_SupportedObjectResult {
	out := make(map[string]*uspproto.GetSupportedDMResp_SupportedObjectResult, len(objs))
	for _, o := range objs {
		out[o.GetSupportedObjPath()] = o
	}
	return out
}

func keysSorted(m map[string]*uspproto.GetSupportedDMResp_SupportedObjectResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
