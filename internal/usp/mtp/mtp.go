package mtp

import (
	"context"
	"net/url"
)

// TopicControllerInboxBase is the bare agent->controller MQTT topic
// without the reply-to suffix. NATS-MQTT bridges require the suffix
// (controllers subscribe to usp.v1.controller.>), so callers should
// use TopicControllerInbox(agentEID) for publish, not the bare value.
const TopicControllerInboxBase = "usp/v1/controller"

// TopicControllerInbox returns the agent->controller publish topic
// with the TR-369 reply-to convention encoded: the agent's response
// topic is url-encoded and appended as `/reply-to=<encoded>`. This
// is how obuspa publishes; the NATS-MQTT bridge converts slashes to
// dots so the controller's `usp.v1.controller.>` wildcard catches it
// and recovers the reply-to via the `/reply-to=` qualifier.
func TopicControllerInbox(agentEID string) string {
	if agentEID == "" {
		return TopicControllerInboxBase
	}
	respTopic := TopicAgentInbox(agentEID)
	return TopicControllerInboxBase + "/reply-to=" + url.QueryEscape(respTopic)
}

func TopicAgentInbox(eid string) string {
	return "usp/v1/agent/" + eid
}

// TopicAgentInboxWildcard returns the multi-level wildcard the agent
// subscribes to so it receives messages whose topic carries a
// /reply-to=... suffix or any other downstream convention.
func TopicAgentInboxWildcard(eid string) string {
	return TopicAgentInbox(eid) + "/#"
}

type Adapter interface {
	Connect(ctx context.Context) error
	Send(ctx context.Context, record []byte) error
	Recv() <-chan []byte
	Close() error
}
