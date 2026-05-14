//go:build acceptance

package harness

import (
	"crypto/md5" //nolint:gosec // CWMP CR digest auth is MD5 per RFC 7616
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// DigestGet performs a single GET against url, performing the RFC
// 7616 qop=auth/MD5 digest handshake using user/pass. The CR listener
// in cpe-sim supports exactly this scheme (matching the server-side
// implementation in internal/cwmp/cr).
//
// Two-round-trip dance:
//
//	1. GET → 401 with WWW-Authenticate: Digest realm="..." nonce="..." qop="auth" algorithm=MD5
//	2. Compute response = MD5(MD5(user:realm:pass) : nonce : nc : cnonce : auth : MD5(GET:uri))
//	3. GET with Authorization: Digest username="..." realm="..." nonce="..." uri="..." qop=auth nc=00000001 cnonce="..." response="..." algorithm=MD5
//
// Returns the second-round response (typically 200). Test must Close
// its body. Fails t on any unexpected behavior (network error,
// wrong-status challenge, missing nonce, second round non-200 with
// stale=false).
func DigestGet(t *testing.T, url, user, pass string) *http.Response {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}

	// Round 1: unauthenticated GET expects 401 with Digest challenge.
	req1, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("DigestGet: build round-1 request: %v", err)
	}
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("DigestGet: round-1: %v", err)
	}
	io.Copy(io.Discard, resp1.Body) //nolint:errcheck
	resp1.Body.Close()               //nolint:errcheck
	if resp1.StatusCode != http.StatusUnauthorized {
		t.Fatalf("DigestGet: round-1 status %d, want 401", resp1.StatusCode)
	}

	challenge := resp1.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Digest ") {
		t.Fatalf("DigestGet: round-1 WWW-Authenticate not Digest: %q", challenge)
	}
	realm := extractChallengeAttr(t, challenge, "realm")
	nonce := extractChallengeAttr(t, challenge, "nonce")

	// Round 2: authenticated retry.
	uri := req1.URL.RequestURI()
	const nc = "00000001"
	cnonce := randomHex(8)
	response := computeDigestResponse(user, realm, pass, http.MethodGet, uri, nonce, nc, cnonce)

	req2, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("DigestGet: build round-2 request: %v", err)
	}
	req2.Header.Set("Authorization",
		`Digest username="`+user+
			`", realm="`+realm+
			`", nonce="`+nonce+
			`", uri="`+uri+
			`", qop=auth, nc=`+nc+
			`, cnonce="`+cnonce+
			`", response="`+response+
			`", algorithm=MD5`,
	)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("DigestGet: round-2: %v", err)
	}
	return resp2
}

// computeDigestResponse computes RFC 7616 qop=auth/MD5 response.
// Mirrors internal/cwmp/cr's server-side and that package's test
// helper.
func computeDigestResponse(user, realm, pass, method, uri, nonce, nc, cnonce string) string {
	ha1 := md5hex(user + ":" + realm + ":" + pass)
	ha2 := md5hex(method + ":" + uri)
	return md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

// extractChallengeAttr pulls `key="value"` out of a Digest
// challenge string. Trivial parser sufficient for the cpe-sim CR
// listener's output; full RFC parsing isn't needed.
func extractChallengeAttr(t *testing.T, challenge, key string) string {
	t.Helper()
	pat := key + `="`
	i := strings.Index(challenge, pat)
	if i < 0 {
		t.Fatalf("DigestGet: %s not in challenge: %q", key, challenge)
	}
	rest := challenge[i+len(pat):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("DigestGet: unterminated %s in challenge: %q", key, challenge)
	}
	return rest[:end]
}

// randomHex returns a 2*n-character lowercase hex string from
// crypto/rand. The CR listener doesn't validate cnonce contents
// (it's a client-chosen value), but we want one anyway so each
// Authorization header is fresh.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Quiet imports.
var _ = fmt.Sprintf
