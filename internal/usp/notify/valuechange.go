package notify

import (
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

type ValueChangeBuilder struct {
	SubscriptionID string
}

func (b *ValueChangeBuilder) Build(paramPath, paramValue string) (*uspproto.Msg, error) {
	msgID, err := newMsgID()
	if err != nil {
		return nil, err
	}
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: msgID, MsgType: uspproto.Header_NOTIFY},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Notify{
						Notify: &uspproto.Notify{
							SubscriptionId: b.SubscriptionID,
							Notification: &uspproto.Notify_ValueChange_{
								ValueChange: &uspproto.Notify_ValueChange{
									ParamPath:  paramPath,
									ParamValue: paramValue,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}
