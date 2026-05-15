package mqtt

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// DerivePassword returns the MQTT password a CPE presents when the
// broker is configured with the HMAC-PSK auth contract: every device
// derives its password from a shared platform secret and its own
// username (== TR-369 endpoint ID), so a fleet of N agents needs zero
// per-device credential provisioning. The broker side runs the same
// HMAC over the supplied username and constant-time compares.
//
// Encoding is base64url with padding stripped, matching the herder
// authservice known-vector test (TestComputeHMACPassword_KnownVector).
func DerivePassword(secret, username string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(username))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ConnectErrorReason buckets a paho Connect error into a low-
// cardinality label so the metric can show why a fleet of agents is
// failing to authenticate without per-CPE blowup. Returns "" for a
// nil error. Buckets are coarse on purpose: the full error message
// goes to the WARN log line alongside the EID.
func ConnectErrorReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "context"
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "not authorised"), strings.Contains(s, "not authorized"):
		return "not_authorized"
	case strings.Contains(s, "bad user name"), strings.Contains(s, "bad password"), strings.Contains(s, "bad user name or password"):
		return "bad_user_password"
	case strings.Contains(s, "identifier rejected"):
		return "id_rejected"
	case strings.Contains(s, "server unavailable"):
		return "server_unavailable"
	case strings.Contains(s, "unacceptable protocol"):
		return "bad_protocol_version"
	case strings.Contains(s, "connection refused"), strings.Contains(s, "no such host"), strings.Contains(s, "i/o timeout"):
		return "unreachable"
	case strings.Contains(s, "deadline exceeded"), strings.Contains(s, "timeout"):
		return "timeout"
	default:
		return "other"
	}
}
