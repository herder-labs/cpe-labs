package mqtt_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"github.com/herder-labs/cpe-labs/internal/usp/mtp"
	"github.com/herder-labs/cpe-labs/internal/usp/mtp/mqtt"
)

func TestMQTTAdapterConnectSubscribePublishReceive(t *testing.T) {
	host, port, cleanup := startEmbeddedBroker(t)
	defer cleanup()

	const eid = "os::AABBCCDDEEFF"
	adapter, err := mqtt.New(mqtt.Options{
		BrokerHost:   host,
		BrokerPort:   port,
		EndpointID:   eid,
		CleanSession: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer adapter.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Inbound: publish to the agent inbox topic from a separate client,
	// expect adapter.Recv() to deliver it.
	other := newPahoClient(t, host, port, "test-controller-pub")
	defer other.Disconnect(250)
	want := []byte("controller-bound record bytes")
	if token := other.Publish(mtp.TopicAgentInbox(eid), 0, false, want); token.Wait() && token.Error() != nil {
		t.Fatalf("controller publish: %v", token.Error())
	}
	select {
	case got := <-adapter.Recv():
		if string(got) != string(want) {
			t.Fatalf("Recv got %q want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for inbound record")
	}

	// Outbound: adapter.Send to the controller inbox; verify by subscribing
	// from the other client.
	sub := newPahoClient(t, host, port, "test-controller-sub")
	defer sub.Disconnect(250)
	gotCh := make(chan []byte, 1)
	if token := sub.Subscribe(mtp.TopicControllerInbox, 1, func(_ paho.Client, msg paho.Message) {
		payload := make([]byte, len(msg.Payload()))
		copy(payload, msg.Payload())
		gotCh <- payload
	}); token.Wait() && token.Error() != nil {
		t.Fatalf("controller subscribe: %v", token.Error())
	}

	outbound := []byte("agent->controller record")
	if err := adapter.Send(ctx, outbound); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-gotCh:
		if string(got) != string(outbound) {
			t.Fatalf("controller received %q want %q", got, outbound)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for outbound record")
	}
}

func TestNewRejectsEmptyBrokerHost(t *testing.T) {
	_, err := mqtt.New(mqtt.Options{EndpointID: "os::A"})
	if err == nil {
		t.Fatal("expected error for empty BrokerHost")
	}
}

func TestNewRejectsEmptyEndpointID(t *testing.T) {
	_, err := mqtt.New(mqtt.Options{BrokerHost: "localhost"})
	if err == nil {
		t.Fatal("expected error for empty EndpointID")
	}
}

func TestSendBeforeConnectFails(t *testing.T) {
	adapter, err := mqtt.New(mqtt.Options{BrokerHost: "127.0.0.1", BrokerPort: 1883, EndpointID: "os::A"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer adapter.Close()
	if err := adapter.Send(context.Background(), []byte("x")); err == nil {
		t.Fatal("expected error sending before Connect")
	}
}

func startEmbeddedBroker(t *testing.T) (string, int, func()) {
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

	srv := broker.New(&broker.Options{InlineClient: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
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

	// Wait for the broker to bind.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cleanup := func() {
		if err := srv.Close(); err != nil {
			t.Logf("broker close: %v", err)
		}
	}
	return host, port, cleanup
}

func newPahoClient(t *testing.T, host string, port int, clientID string) paho.Client {
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

