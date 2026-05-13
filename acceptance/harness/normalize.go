//go:build acceptance

package harness

import (
	"regexp"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// NormalizeCWMP rewrites run-specific fields in a CWMP envelope into
// stable sentinels so two runs of the same scenario produce
// byte-identical output.
//
// Replacements:
//
//	<cwmp:ID ...>nnn</cwmp:ID>          → <cwmp:ID ...>{ID}</cwmp:ID>
//	<CurrentTime>YYYY-...</CurrentTime> → <CurrentTime>{TIMESTAMP}</CurrentTime>
//	<ConnectionRequestURL>http://...    → <ConnectionRequestURL>{CR_URL}
//
// RetryCount is deterministic (0 for fresh sessions, monotonic per
// recovery cycle); preserved verbatim so a future regression that
// changes the retry-counter shape breaks the golden.
//
// DeviceId, EventCode, ParameterList values, MaxEnvelopes are all
// preserved verbatim.
func NormalizeCWMP(envelope []byte) []byte {
	out := envelope
	out = reCWMPID.ReplaceAll(out, []byte("$1{ID}$2"))
	out = reCurrentTime.ReplaceAll(out, []byte("<CurrentTime>{TIMESTAMP}</CurrentTime>"))
	out = reCRURL.ReplaceAll(out, []byte("<ConnectionRequestURL>{CR_URL}</ConnectionRequestURL>"))
	return out
}

// regex anchors:
//
//	$1 = opening tag (with any attrs), $2 = closing tag verbatim
var (
	reCWMPID      = regexp.MustCompile(`(<cwmp:ID[^>]*>)[^<]*(</cwmp:ID>)`)
	reCurrentTime = regexp.MustCompile(`<CurrentTime>[^<]*</CurrentTime>`)
	reCRURL       = regexp.MustCompile(`<ConnectionRequestURL>[^<]*</ConnectionRequestURL>`)
)

// NormalizeUSPRecord unwraps a USP Record, rewrites the embedded Msg
// header's msg_id into a sentinel, and re-renders both as prototext
// so the golden is human-readable and diff-friendly.
//
// The outer Record's from_id / to_id / version / payload_security are
// preserved (those are the wire contract). The embedded Msg is fully
// re-rendered after Header.MsgId substitution.
//
// USP scenarios are deferred to follow-up sub-issues; the helper is
// included here so they have a stable seam to plug into.
func NormalizeUSPRecord(payload []byte) ([]byte, error) {
	record, msg, err := codec.UnwrapRecord(payload)
	if err != nil {
		return nil, err
	}
	msgClone := proto.Clone(msg).(*uspproto.Msg)
	if msgClone.GetHeader() != nil {
		msgClone.Header.MsgId = "{MSG_ID}"
	}

	textOpts := prototext.MarshalOptions{Multiline: true, Indent: "  "}

	recordText, err := textOpts.Marshal(record)
	if err != nil {
		return nil, err
	}
	msgText, err := textOpts.Marshal(msgClone)
	if err != nil {
		return nil, err
	}

	out := []byte("# Record\n")
	out = append(out, recordText...)
	out = append(out, "\n# Msg\n"...)
	out = append(out, msgText...)
	return out, nil
}
