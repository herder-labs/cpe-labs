package notify

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

const (
	DefaultOUIPath          = "Device.DeviceInfo.ManufacturerOUI"
	DefaultProductClassPath = "Device.DeviceInfo.ProductClass"
	DefaultSerialPath       = "Device.DeviceInfo.SerialNumber"
	DefaultAgentVersion     = "1.5"

	DefaultBootSubscriptionID = "default-boot-event-ACS"
	BootEventObjPath          = "Device."
	BootEventName             = "Boot!"
)

type OnBoardRequestBuilder struct {
	OUIPath          string
	ProductClassPath string
	SerialPath       string
	AgentVersion     string
}

func (b *OnBoardRequestBuilder) Build(tree *paramtree.Tree) (*uspproto.Msg, error) {
	ouiPath := orDefault(b.OUIPath, DefaultOUIPath)
	productClassPath := orDefault(b.ProductClassPath, DefaultProductClassPath)
	serialPath := orDefault(b.SerialPath, DefaultSerialPath)
	agentVersion := orDefault(b.AgentVersion, DefaultAgentVersion)

	oui, err := readLeaf(tree, ouiPath)
	if err != nil {
		return nil, err
	}
	productClass, err := readLeaf(tree, productClassPath)
	if err != nil {
		return nil, err
	}
	serial, err := readLeaf(tree, serialPath)
	if err != nil {
		return nil, err
	}

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
							Notification: &uspproto.Notify_OnBoardReq{
								OnBoardReq: &uspproto.Notify_OnBoardRequest{
									Oui:                            oui,
									ProductClass:                   productClass,
									SerialNumber:                   serial,
									AgentSupportedProtocolVersions: agentVersion,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

type BootEventBuilder struct {
	SubscriptionID string
}

func (b *BootEventBuilder) Build(tree *paramtree.Tree) (*uspproto.Msg, error) {
	_ = tree
	subID := orDefault(b.SubscriptionID, DefaultBootSubscriptionID)
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
							SubscriptionId: subID,
							Notification: &uspproto.Notify_Event_{
								Event: &uspproto.Notify_Event{
									ObjPath:   BootEventObjPath,
									EventName: BootEventName,
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func readLeaf(tree *paramtree.Tree, path string) (string, error) {
	v, err := tree.Get(path)
	if err != nil {
		return "", &cpeerr.Error{Op: "notify.read", Kind: cpeerr.KindNotFound, Err: fmt.Errorf("path %s: %w", path, err)}
	}
	return v.Raw, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func newMsgID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", &cpeerr.Error{Op: "notify.newMsgID", Kind: cpeerr.KindInternal, Err: err}
	}
	return hex.EncodeToString(b[:]), nil
}
