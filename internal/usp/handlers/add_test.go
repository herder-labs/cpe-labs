package handlers_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

const wifiSSIDProfile = `
objects:
  - path: Device.WiFi.SSID
    instances: 1
    uniqueKeys:
      - [SSID]
    parameters:
      - path: SSID
        value: "Default"
        writable: true
      - path: Enable
        type: xsd:boolean
        value: "false"
        writable: true
`

func TestAddHandlerHappyPath(t *testing.T) {
	tree, uniqueKeys := loadWifiSSID(t)

	var notifies atomic.Int32
	var lastPath atomic.Value
	var lastKeys atomic.Value
	h := handlers.NewAdd(tree, uniqueKeys, nil, func(path string, keys map[string]string) {
		notifies.Add(1)
		lastPath.Store(path)
		lastKeys.Store(keys)
	})

	req := buildAddRequest("Device.WiFi.SSID.", "SSID", "HomeNet")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	results := resp.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	success := results[0].GetOperStatus().GetOperSuccess()
	if success == nil {
		t.Fatalf("OperSuccess nil; full=%+v", results[0])
	}
	if !strings.HasPrefix(success.GetInstantiatedPath(), "Device.WiFi.SSID.") || !strings.HasSuffix(success.GetInstantiatedPath(), ".") {
		t.Errorf("InstantiatedPath=%q", success.GetInstantiatedPath())
	}
	if success.GetUniqueKeys()["SSID"] != "HomeNet" {
		t.Errorf("unique_keys[SSID]=%q want HomeNet", success.GetUniqueKeys()["SSID"])
	}

	// Tree state reflects the create.
	v, err := tree.Get(success.GetInstantiatedPath() + "SSID")
	if err != nil {
		t.Fatalf("Get after Add: %v", err)
	}
	if v.Raw != "HomeNet" {
		t.Errorf("SSID raw=%q want HomeNet", v.Raw)
	}

	for i := 0; i < 50; i++ {
		if notifies.Load() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if notifies.Load() != 1 {
		t.Errorf("ObjectCreation notifier calls=%d want 1", notifies.Load())
	}
	if got := lastKeys.Load().(map[string]string); got["SSID"] != "HomeNet" {
		t.Errorf("notifier keys[SSID]=%q", got["SSID"])
	}
}

func TestAddHandlerRollsBackOnInvalidValue(t *testing.T) {
	tree, uniqueKeys := loadWifiSSID(t)
	var notifies atomic.Int32
	h := handlers.NewAdd(tree, uniqueKeys, nil, func(p string, k map[string]string) { notifies.Add(1) })

	req := buildAddRequest("Device.WiFi.SSID.", "Enable", "not-a-bool")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	fail := resp.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()[0].GetOperStatus().GetOperFailure()
	if fail == nil {
		t.Fatalf("expected OperFailure")
	}

	// Rolled back: the just-created instance (.2., since the profile
	// pre-materializes .1.) is gone.
	if _, err := tree.Get("Device.WiFi.SSID.2.Enable"); err == nil {
		t.Errorf("instance leaked after rollback")
	}
	time.Sleep(20 * time.Millisecond)
	if notifies.Load() != 0 {
		t.Errorf("notifier should not fire on failure, got %d", notifies.Load())
	}
}

func TestAddHandlerNotATable(t *testing.T) {
	tree, uniqueKeys := loadWifiSSID(t)
	h := handlers.NewAdd(tree, uniqueKeys, nil, nil)
	req := buildAddRequest("Device.DoesNot.Exist.")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	fail := resp.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()[0].GetOperStatus().GetOperFailure()
	if fail == nil {
		t.Fatalf("expected OperFailure")
	}
	if fail.GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d", fail.GetErrCode())
	}
}

func TestAddHandlerMultipleCreates(t *testing.T) {
	tree, uniqueKeys := loadWifiSSID(t)
	h := handlers.NewAdd(tree, uniqueKeys, nil, nil)

	req := buildAddRequest("Device.WiFi.SSID.", "SSID", "A")
	req2 := buildAddRequest("Device.WiFi.SSID.", "SSID", "B")
	resp1, _ := h.Handle(context.Background(), req)
	resp2, _ := h.Handle(context.Background(), req2)
	p1 := resp1.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()[0].GetOperStatus().GetOperSuccess().GetInstantiatedPath()
	p2 := resp2.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()[0].GetOperStatus().GetOperSuccess().GetInstantiatedPath()
	if p1 == p2 {
		t.Fatalf("expected distinct instance paths, both = %q", p1)
	}
}

func TestAddHandlerNoUniqueKeysWhenProfileLacksThem(t *testing.T) {
	body := strings.Replace(wifiSSIDProfile, `    uniqueKeys:
      - [SSID]
`, "", 1)
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	h := handlers.NewAdd(p.Tree, p.UniqueKeys, nil, nil)
	req := buildAddRequest("Device.WiFi.SSID.", "SSID", "X")
	resp, _ := h.Handle(context.Background(), req)
	success := resp.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()[0].GetOperStatus().GetOperSuccess()
	if len(success.GetUniqueKeys()) != 0 {
		t.Errorf("expected empty unique_keys when profile lacks them, got %+v", success.GetUniqueKeys())
	}
}

func loadWifiSSID(t *testing.T) (*paramtree.Tree, map[string][][]string) {
	t.Helper()
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(wifiSSIDProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	return p.Tree, p.UniqueKeys
}

func buildAddRequest(objPath string, paramVals ...string) *uspproto.Msg {
	co := &uspproto.Add_CreateObject{ObjPath: objPath}
	for i := 0; i < len(paramVals); i += 2 {
		co.ParamSettings = append(co.ParamSettings, &uspproto.Add_CreateParamSetting{
			Param: paramVals[i], Value: paramVals[i+1], Required: true,
		})
	}
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_ADD},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Add{
						Add: &uspproto.Add{CreateObjs: []*uspproto.Add_CreateObject{co}},
					},
				},
			},
		},
	}
}
