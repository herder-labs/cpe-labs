package mtp

import "context"

const TopicControllerInbox = "usp/v1/controller"

func TopicAgentInbox(eid string) string {
	return "usp/v1/agent/" + eid
}

type Adapter interface {
	Connect(ctx context.Context) error
	Send(ctx context.Context, record []byte) error
	Recv() <-chan []byte
	Close() error
}
