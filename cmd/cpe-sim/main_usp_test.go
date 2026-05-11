package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	brokerpkg "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/herder-labs/cpe-labs/internal/usp/codec"
)

// TestRunDaemonModeUSPOnBoardRequest is the foundation acceptance
// test for issue #7 in cpe-labs-context. cpe-sim, started with a
// USP-enabled profile pointed at an embedded MQTT broker, must
// publish exactly one OnBoardRequest Notify on usp/v1/controller
// with the expected oui, product_class, serial_number, and
// agent_supported_protocol_versions.
func TestRunDaemonModeUSPOnBoardRequest(t *testing.T) {
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

	subscriber := newSubscriberClient(t, brokerHost, brokerPort, "usp-it-controller-sub")
	defer subscriber.Disconnect(250)

	var notifyCount atomic.Int32
	gotCh := make(chan []byte, 4)
	if token := subscriber.Subscribe("usp/v1/controller", 1, func(_ paho.Client, msg paho.Message) {
		payload := make([]byte, len(msg.Payload()))
		copy(payload, msg.Payload())
		notifyCount.Add(1)
		select {
		case gotCh <- payload:
		default:
		}
	}); token.Wait() && token.Error() != nil {
		t.Fatalf("subscribe usp/v1/controller: %v", token.Error())
	}

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
    value: "USP-INTEGRATION-1"

fleet:
  count: 1
  serialPattern: "{base}"

usp:
  enable: true
  controllerEndpointID: "self::openacs"
  broker:
    address: %s
    port: %d
    protocolVersion: "3.1.1"
`, brokerHost, brokerPort)), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	args := []string{
		"--acs-url=" + acs.URL,
		"--profile=" + profile,
		"--log-level=error",
	}
	done := make(chan error, 1)
	go func() { done <- run(ctx, args, os.Stdout, os.Stderr) }()

	select {
	case payload := <-gotCh:
		record, msg, err := codec.UnwrapRecord(payload)
		if err != nil {
			t.Fatalf("UnwrapRecord: %v", err)
		}
		if record.GetFromId() != "os::AABBCCUSP-INTEGRATION-1" {
			t.Errorf("FromId=%q want os::AABBCCUSP-INTEGRATION-1", record.GetFromId())
		}
		if record.GetToId() != "self::openacs" {
			t.Errorf("ToId=%q want self::openacs", record.GetToId())
		}
		req := msg.GetBody().GetRequest().GetNotify().GetOnBoardReq()
		if req == nil {
			t.Fatalf("expected OnBoardRequest, got msg_type=%v", msg.GetHeader().GetMsgType())
		}
		if req.GetOui() != "AABBCC" {
			t.Errorf("Oui=%q", req.GetOui())
		}
		if req.GetProductClass() != "TestModel" {
			t.Errorf("ProductClass=%q", req.GetProductClass())
		}
		if req.GetSerialNumber() != "USP-INTEGRATION-1" {
			t.Errorf("SerialNumber=%q", req.GetSerialNumber())
		}
		if req.GetAgentSupportedProtocolVersions() != "1.5" {
			t.Errorf("AgentSupportedProtocolVersions=%q", req.GetAgentSupportedProtocolVersions())
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("timed out waiting for OnBoardRequest Notify on usp/v1/controller")
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

func startBrokerForTest(t *testing.T) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	srv := brokerpkg.New(&brokerpkg.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("AddHook: %v", err)
	}
	if err := srv.AddListener(listeners.NewTCP(listeners.Config{ID: "tcp-test", Address: fmt.Sprintf("%s:%d", host, port)})); err != nil {
		t.Fatalf("AddListener: %v", err)
	}
	go func() {
		if err := srv.Serve(); err != nil {
			t.Logf("broker serve: %v", err)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return host, port, func() {
		if err := srv.Close(); err != nil {
			t.Logf("broker close: %v", err)
		}
	}
}

func newSubscriberClient(t *testing.T, host string, port int, clientID string) paho.Client {
	t.Helper()
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", host, port)).
		SetClientID(clientID).
		SetCleanSession(true).
		SetProtocolVersion(4).
		SetConnectTimeout(2 * time.Second)
	c := paho.NewClient(opts)
	token := c.Connect()
	if !token.WaitTimeout(2 * time.Second) {
		t.Fatalf("paho connect timeout")
	}
	if err := token.Error(); err != nil {
		t.Fatalf("paho connect: %v", err)
	}
	return c
}
