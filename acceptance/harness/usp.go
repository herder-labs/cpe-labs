//go:build acceptance

package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/herder-labs/cpe-labs/internal/usp/identity"
)

// USPFirstContact discriminates the profile variant the USP fixture
// uses. FactoryReset leaves Internal.Reboot.Cause at the simulator's
// installed default (LocalFactoryReset), so first contact emits
// Notify(OnBoardRequest). WarmBoot pre-declares the leaf as
// LocalReboot in the profile so first contact emits Notify(Event{Boot!}).
type USPFirstContact int

const (
	FactoryReset USPFirstContact = iota
	WarmBoot
)

// USPOptions tweaks StartUSPAcceptance.
type USPOptions struct {
	FirstContact USPFirstContact
}

// StartUSPAcceptance brings up an embedded mochi-mqtt broker, a paho
// subscriber on usp/v1/controller/#, and materializes the matching
// USP-enabled acceptance profile pointed at the broker into
// t.TempDir(). Returns a Fixture ready for LaunchSimDaemon.
//
// The paho subscriber appends every payload to fix.Capture.
// Disconnects automatically via t.Cleanup.
//
// cpe-sim's --acs-url is satisfied with a 204-everything httptest
// server; CWMP is not exercised but the flag is mandatory.
func StartUSPAcceptance(t *testing.T, opts USPOptions) *Fixture {
	t.Helper()

	host, port, brokerCleanup := StartBroker(t)
	t.Cleanup(brokerCleanup)

	cap := NewWireCapture()

	// Mock ACS: returns InformResponse for the bootstrap Inform POST
	// (otherwise cpe-sim aborts before USP first-contact fires) and
	// 204s everything else. CWMP traffic is incidental for USP
	// scenarios; we just need it to handshake cleanly.
	acs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "<cwmp:Inform>") {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(informResponse))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(acs.Close)

	// Subscribe before launching the simulator so the OnBoardRequest
	// publish hits a live subscriber. paho's Subscribe(...).Wait()
	// guarantees the broker acknowledged the SUBSCRIBE before return.
	clientID := uniqueSubClientID(t.Name())
	client := newAcceptanceSubscriber(t, host, port, clientID, cap)
	t.Cleanup(func() { client.Disconnect(250) })

	// Materialize the right profile variant.
	profileSrc := acceptanceProfileSrc(t, opts.FirstContact)
	profileDir := MaterializeProfile(t, profileSrc, map[string]string{
		"BROKER_ADDR": host,
		"BROKER_PORT": fmt.Sprintf("%d", port),
	})

	// Derive the agent EID the same way cpe-sim will from the profile.
	// Profile OUI = AABBCC, Serial = ACCEPTANCE-0001 → os::AABBCCACCEPTANCE-0001.
	agentEID := identity.EID("AABBCC", "ACCEPTANCE-0001")

	return &Fixture{
		ACS:         acs,
		Capture:     cap,
		ProfilePath: profileDir,
		BinPath:     SimBinary(t),
		BrokerHost:  host,
		BrokerPort:  port,
		AgentEID:    agentEID,
	}
}

// uniqueSubClientID returns an MQTT client ID built from the test
// name and a random suffix. Avoids broker-side collisions when
// multiple acceptance tests run in parallel.
func uniqueSubClientID(testName string) string {
	// MQTT 3.1.1 limits client IDs to 23 chars; recent brokers (mochi
	// included) accept longer, but stay friendly.
	safeName := strings.NewReplacer("/", "-", " ", "-").Replace(testName)
	if len(safeName) > 12 {
		safeName = safeName[:12]
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("acc-%s-%s", safeName, hex.EncodeToString(b[:]))
}

// newAcceptanceSubscriber returns a paho client connected to the
// broker and subscribed to usp/v1/controller/# at QoS 1. Every
// message payload is forwarded to cap.AppendUSPMessage.
func newAcceptanceSubscriber(t *testing.T, host string, port int, clientID string, cap *WireCapture) paho.Client {
	t.Helper()
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", host, port)).
		SetClientID(clientID).
		SetCleanSession(true).
		SetProtocolVersion(4).
		SetConnectTimeout(2 * time.Second)
	c := paho.NewClient(opts)
	if token := c.Connect(); !token.WaitTimeout(2 * time.Second) || token.Error() != nil {
		t.Fatalf("acceptance subscriber connect: %v", token.Error())
	}

	token := c.Subscribe("usp/v1/controller/#", 1, func(_ paho.Client, m paho.Message) {
		cap.AppendUSPMessage(m.Payload())
	})
	if !token.WaitTimeout(2 * time.Second) {
		t.Fatalf("acceptance subscriber subscribe: timeout")
	}
	if err := token.Error(); err != nil {
		t.Fatalf("acceptance subscriber subscribe: %v", err)
	}
	return c
}

// acceptanceProfileSrc returns the absolute path to the matching USP
// profile under acceptance/profiles/.
func acceptanceProfileSrc(t *testing.T, fc USPFirstContact) string {
	t.Helper()
	root := repoRoot()
	switch fc {
	case FactoryReset:
		return fmt.Sprintf("%s/acceptance/profiles/minimal-usp", root)
	case WarmBoot:
		return fmt.Sprintf("%s/acceptance/profiles/minimal-usp-warm-boot", root)
	default:
		t.Fatalf("StartUSPAcceptance: unknown USPFirstContact %d", fc)
		return ""
	}
}
