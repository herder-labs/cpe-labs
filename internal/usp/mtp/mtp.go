package mtp

import "context"

// TopicControllerInboxBase is the prefix for the agent->controller
// MQTT publish topic. The agent's TR-369 endpoint ID is appended as
// the next segment (TopicControllerInbox), giving herder's auth
// callout and identity cross-check a stable per-device subject.
const TopicControllerInboxBase = "usp/v1/controller"

// TopicControllerInbox returns the agent->controller publish topic
// for the supplied agent EID: usp/v1/controller/<eid>. herder's pub
// ACL is anchored to usp.v1.controller.<eid> (exact + subtree); the
// identity cross-check pulls the EID from the subject's first token
// after usp.v1.controller. and rejects records whose from_id differs.
//
// TR-369 R-MQTT.24 permits an optional `/reply-to=<encoded-topic>`
// suffix; we omit it on the wire because the bare per-EID subject is
// sufficient for herder's bridge and keeps the published-topic
// payload narrow. If a future deployment needs sticky reply-to
// addressing, callers can append the suffix at the call site.
func TopicControllerInbox(agentEID string) string {
	if agentEID == "" {
		return TopicControllerInboxBase
	}
	return TopicControllerInboxBase + "/" + agentEID
}

func TopicAgentInbox(eid string) string {
	return "usp/v1/agent/" + eid
}

// TopicAgentInboxWildcard returns the multi-level wildcard the agent
// subscribes to so it receives messages whose topic carries any
// downstream qualifier suffix.
func TopicAgentInboxWildcard(eid string) string {
	return TopicAgentInbox(eid) + "/#"
}

type Adapter interface {
	Connect(ctx context.Context) error
	Send(ctx context.Context, record []byte) error
	Recv() <-chan []byte
	Close() error
}
