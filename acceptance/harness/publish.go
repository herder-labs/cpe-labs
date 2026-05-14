//go:build acceptance

package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// pahoClient is the subset of paho.Client the harness uses. Aliased
// for the lazy slot in Fixture so the import doesn't bleed into
// harness.go.
type pahoClient = paho.Client

// PublishUSP wraps msg in a USP Record (from = ControllerEID,
// to = AgentEID), encodes it, and publishes the bytes to
// usp/v1/agent/<AgentEID> on the fixture's broker. Returns the
// chosen msg_id so the test can correlate the response if needed.
//
// The publisher client is created lazily on first call and reused
// across subsequent publishes within the same fixture. Cleanup runs
// via t.Cleanup registered at creation time.
//
// On any broker/paho failure, t.Fatalf with the underlying error.
func (f *Fixture) PublishUSP(t *testing.T, msg *uspproto.Msg) string {
	t.Helper()
	if f.AgentEID == "" {
		t.Fatalf("PublishUSP: fixture missing AgentEID (not a USP fixture)")
	}

	if msg.GetHeader() == nil {
		msg.Header = &uspproto.Header{}
	}
	if msg.GetHeader().GetMsgId() == "" {
		msg.Header.MsgId = "acc-" + randomHex(8)
	}

	ctrl := f.ControllerEID
	if ctrl == "" {
		ctrl = "self::herder"
	}
	wire, err := codec.WrapMessage(msg, ctrl, f.AgentEID)
	if err != nil {
		t.Fatalf("PublishUSP: wrap: %v", err)
	}

	client := f.publisher(t)
	topic := "usp/v1/agent/" + f.AgentEID
	if token := client.Publish(topic, 1, false, wire); !token.WaitTimeout(2*time.Second) {
		t.Fatalf("PublishUSP: publish to %s timed out", topic)
	} else if err := token.Error(); err != nil {
		t.Fatalf("PublishUSP: publish to %s: %v", topic, err)
	}
	return msg.Header.MsgId
}

// publisher returns the lazy paho publisher, building it on first
// call. Goroutine-safe via Fixture.pubMu.
func (f *Fixture) publisher(t *testing.T) pahoClient {
	t.Helper()
	f.pubMu.Lock()
	defer f.pubMu.Unlock()
	if f.pubClient != nil {
		return f.pubClient
	}
	clientID := publisherClientID(t.Name())
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", f.BrokerHost, f.BrokerPort)).
		SetClientID(clientID).
		SetCleanSession(true).
		SetProtocolVersion(4).
		SetConnectTimeout(2 * time.Second)
	c := paho.NewClient(opts)
	if token := c.Connect(); !token.WaitTimeout(2*time.Second) || token.Error() != nil {
		t.Fatalf("PublishUSP: connect publisher: %v", token.Error())
	}
	f.pubClient = c
	t.Cleanup(func() { c.Disconnect(250) })
	return c
}

func publisherClientID(testName string) string {
	safe := testName
	if len(safe) > 12 {
		safe = safe[:12]
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("acc-pub-%s-%s", safe, hex.EncodeToString(b[:]))
}
