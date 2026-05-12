package notify

import (
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

type ObjectCreationBuilder struct {
	SubscriptionID string
}

func (b *ObjectCreationBuilder) Build(objPath string, uniqueKeys map[string]string) (*uspproto.Msg, error) {
	msgID, err := newMsgID()
	if err != nil {
		return nil, err
	}
	keysCopy := make(map[string]string, len(uniqueKeys))
	for k, v := range uniqueKeys {
		keysCopy[k] = v
	}
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: msgID, MsgType: uspproto.Header_NOTIFY},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Notify{
						Notify: &uspproto.Notify{
							SubscriptionId: b.SubscriptionID,
							Notification: &uspproto.Notify_ObjCreation{
								ObjCreation: &uspproto.Notify_ObjectCreation{
									ObjPath:    objPath,
									UniqueKeys: keysCopy,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

type ObjectDeletionBuilder struct {
	SubscriptionID string
}

func (b *ObjectDeletionBuilder) Build(objPath string) (*uspproto.Msg, error) {
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
							Notification: &uspproto.Notify_ObjDeletion{
								ObjDeletion: &uspproto.Notify_ObjectDeletion{
									ObjPath: objPath,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}
