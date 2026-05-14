//go:build acceptance

package harness

import (
	"fmt"
	"net"
	"regexp"
	"testing"
)

// PickFreePort returns a localhost TCP port that was open at the
// moment of return. Picks via net.Listen("tcp", "127.0.0.1:0") +
// Close. There is a TOCTOU race between this returning and cpe-sim
// binding to the port; in a normal test environment with no
// background port-grabbers the race is negligible.
//
// Used by CR-listener scenarios where the test needs to know the
// port out-of-band (cpe-sim publishes the URL to the tree before
// listener.Start binds, so the in-tree leaf is empty until that
// runtime bug is fixed).
func PickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("PickFreePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("PickFreePort close: %v", err)
	}
	return port
}

// ExtractCWMPParameter pulls a single parameter value out of a
// captured CWMP envelope (typically an Inform's ParameterList). Uses
// a conservative regex against the canonical SOAP encoder output:
//
//	<ParameterValueStruct>
//	  <Name>X</Name>
//	  <Value xsi:type="...">VALUE</Value>
//	</ParameterValueStruct>
//
// Returns the inner Value text. If the parameter is absent or the
// envelope is malformed, returns an error.
//
// Regex-based rather than full XML parsing because the SOAP encoder's
// output is canonical and the helper is test-only.
func ExtractCWMPParameter(envelope []byte, paramName string) (string, error) {
	// Build a regex anchored on <Name>paramName</Name> followed by the
	// next <Value>...</Value>. The Value tag carries an xsi:type
	// attribute; we don't care about its content for extraction.
	pattern := fmt.Sprintf(
		`<Name>%s</Name>\s*<Value[^>]*>([^<]*)</Value>`,
		regexp.QuoteMeta(paramName),
	)
	re := regexp.MustCompile(pattern)
	m := re.FindSubmatch(envelope)
	if m == nil {
		return "", fmt.Errorf("ExtractCWMPParameter: %s not found", paramName)
	}
	return string(m[1]), nil
}
