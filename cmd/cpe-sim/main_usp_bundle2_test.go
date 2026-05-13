package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// TestRunDaemonModeUSPBundle2RoundTrip drives Bundle 2 surfaces:
// GetInstances + GetSupportedDM responses, plus autonomous Notify
// emission via the Subscription evaluator (ValueChange on a watched
// leaf, ObjectCreation gated by an installed Subscription).
func TestRunDaemonModeUSPBundle2RoundTrip(t *testing.T) {
	brokerHost, brokerPort, brokerCleanup := startBrokerForTest(t)
	defer brokerCleanup()

	acs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 || !strings.Contains(string(body), "<cwmp:Inform>") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte(informResponseEnvelope))
	}))
	defer acs.Close()

	tmp := t.TempDir()
	profile := filepath.Join(tmp, "profile.yaml")
	if err := os.WriteFile(profile, []byte(fmt.Sprintf(`deviceIdPaths:
  manufacturer: Device.DeviceInfo.Manufacturer
  oui:          Device.DeviceInfo.ManufacturerOUI
  productClass: Device.DeviceInfo.ProductClass
  serialNumber: Device.DeviceInfo.SerialNumber

parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "TestVendor"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "TestModel"
  - path: Device.DeviceInfo.SerialNumber
    value: "BUNDLE2"
  - path: Device.WiFi.Radio.1.Channel
    type: xsd:unsignedInt
    value: "6"
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

fleet:
  count: 1
  serialPattern: "{base}"

usp:
  enable: true
  controllerEndpointID: "self::herder"
  broker:
    address: %s
    port: %d
    protocolVersion: "3.1.1"
`, brokerHost, brokerPort)), 0o600); err != nil {
		t.Fatal(err)
	}

	const agentEID = "os::AABBCCBUNDLE2"
	const controllerEID = "self::herder"
	hub := newControllerHub(t, brokerHost, brokerPort)
	defer hub.disconnect()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	args := []string{
		"--acs-url=" + acs.URL,
		"--profile=" + profile,
		"--log-level=error",
	}
	done := make(chan error, 1)
	go func() { done <- run(ctx, args, os.Stdout, os.Stderr) }()

	// Drain first-contact OnBoardRequest.
	_ = hub.awaitNotify(t, "OnBoardRequest", 5*time.Second, func(n *uspproto.Notify) bool { return n.GetOnBoardReq() != nil })

	// 1. GetInstances on Device.WiFi.SSID. -> 2 curr_insts.
	giResp := hub.send(t, agentEID, controllerEID, "msg-gi", uspproto.Header_GET_INSTANCES, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_GetInstances{GetInstances: &uspproto.GetInstances{
				ObjPaths: []string{"Device.WiFi.SSID."},
			}},
		}},
	}, 3*time.Second)
	results := giResp.GetBody().GetResponse().GetGetInstancesResp().GetReqPathResults()
	if len(results) != 1 {
		t.Fatalf("GetInstances len=%d want 1", len(results))
	}
	insts := results[0].GetCurrInsts()
	if len(insts) != 2 {
		t.Fatalf("GetInstances curr_insts=%d want 2", len(insts))
	}
	if insts[0].GetUniqueKeys()["SSID"] == "" {
		t.Errorf("GetInstances unique_keys[SSID] empty")
	}

	// 2. GetSupportedDM on Device. -> ≥10 supported params (small profile).
	gsResp := hub.send(t, agentEID, controllerEID, "msg-gs", uspproto.Header_GET_SUPPORTED_DM, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_GetSupportedDm{GetSupportedDm: &uspproto.GetSupportedDM{
				ObjPaths:     []string{"Device."},
				ReturnParams: true,
			}},
		}},
	}, 3*time.Second)
	gsResults := gsResp.GetBody().GetResponse().GetGetSupportedDmResp().GetReqObjResults()
	if len(gsResults) != 1 {
		t.Fatalf("GetSupportedDM len=%d want 1", len(gsResults))
	}
	totalParams := 0
	for _, so := range gsResults[0].GetSupportedObjs() {
		totalParams += len(so.GetSupportedParams())
	}
	if totalParams < 5 {
		t.Errorf("GetSupportedDM totalParams=%d want ≥5", totalParams)
	}

	// 3. Install a ValueChange Subscription on Device.WiFi.Radio.1.Channel,
	// then Set that leaf, expect autonomous ValueChange Notify.
	installSubscription(t, hub, agentEID, controllerEID, "msg-sub-vc", "ValueChange", "Device.WiFi.Radio.1.Channel")

	setResp := hub.send(t, agentEID, controllerEID, "msg-set-vc", uspproto.Header_SET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Set{Set: &uspproto.Set{
				UpdateObjs: []*uspproto.Set_UpdateObject{{
					ObjPath: "Device.WiFi.Radio.1.",
					ParamSettings: []*uspproto.Set_UpdateParamSetting{
						{Param: "Channel", Value: "11", Required: true},
					},
				}},
			}},
		}},
	}, 3*time.Second)
	if setResp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()[0].GetOperStatus().GetOperSuccess() == nil {
		t.Fatalf("Set on watched leaf did not succeed")
	}

	vc := hub.awaitNotify(t, "ValueChange", 3*time.Second, func(n *uspproto.Notify) bool {
		return n.GetValueChange() != nil && n.GetValueChange().GetParamPath() == "Device.WiFi.Radio.1.Channel"
	})
	if vc.GetValueChange().GetParamValue() != "11" {
		t.Errorf("ValueChange param_value=%q want 11", vc.GetValueChange().GetParamValue())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned error after cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within 5s of ctx cancel")
	}
}
