package handlers

import (
	"strings"

	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// parentObjectPath returns the parent-object form of a TR-181 path.
// "Device.DeviceInfo.SerialNumber" -> "Device.DeviceInfo." and
// "Device.DeviceInfo." -> "Device.DeviceInfo.". The trailing "." is
// preserved by convention; TR-369 paths are dot-terminated for
// objects.
func parentObjectPath(p string) string {
	trimmed := strings.TrimRight(p, ".")
	idx := strings.LastIndex(trimmed, ".")
	if idx < 0 {
		return ""
	}
	return trimmed[:idx+1]
}

// leafName returns the trailing leaf segment of a TR-181 path.
// "Device.DeviceInfo.SerialNumber" -> "SerialNumber".
func leafName(p string) string {
	trimmed := strings.TrimRight(p, ".")
	idx := strings.LastIndex(trimmed, ".")
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}

// isConcreteScalarPath reports whether the path looks like a concrete
// scalar leaf (no trailing dot, no wildcards, has at least one dot
// separating segments). v0 only supports these in Get.
func isConcreteScalarPath(p string) bool {
	if p == "" || strings.HasSuffix(p, ".") {
		return false
	}
	if strings.ContainsAny(p, "*?") {
		return false
	}
	if !strings.Contains(p, ".") {
		return false
	}
	return true
}

// newMsg constructs the response Msg envelope with the given type.
// The msg_id is left empty; the session dispatch wrapper fills it in
// from the request before sending.
func newMsg(msgType uspproto.Header_MsgType) *uspproto.Msg {
	return &uspproto.Msg{Header: &uspproto.Header{MsgType: msgType}}
}
