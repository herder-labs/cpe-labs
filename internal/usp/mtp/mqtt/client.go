package mqtt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/usp/mtp"
)

const (
	defaultKeepAlive       = 60 * time.Second
	defaultConnectTimeout  = 10 * time.Second
	defaultRecvBufferDepth = 32

	pahoProtocolVersion311 = 4
)

type Options struct {
	BrokerHost      string
	BrokerPort      int
	Username        string
	Password        string
	EndpointID      string
	KeepAlive       time.Duration
	CleanSession    bool
	TLSConfig       *tls.Config
	RecvBufferDepth int
	Logger          *slog.Logger
}

type Client struct {
	opts   Options
	client paho.Client
	recv   chan []byte
	logger *slog.Logger

	mu        sync.Mutex
	connected bool
	closed    bool
}

func New(opts Options) (*Client, error) {
	if opts.BrokerHost == "" {
		return nil, &cpeerr.Error{Op: "mqtt.New", Kind: cpeerr.KindInvalidArgument, Err: errors.New("BrokerHost is empty")}
	}
	if opts.BrokerPort == 0 {
		opts.BrokerPort = 1883
	}
	if opts.EndpointID == "" {
		return nil, &cpeerr.Error{Op: "mqtt.New", Kind: cpeerr.KindInvalidArgument, Err: errors.New("EndpointID is empty")}
	}
	if opts.KeepAlive == 0 {
		opts.KeepAlive = defaultKeepAlive
	}
	if opts.RecvBufferDepth == 0 {
		opts.RecvBufferDepth = defaultRecvBufferDepth
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		opts:   opts,
		recv:   make(chan []byte, opts.RecvBufferDepth),
		logger: opts.Logger,
	}, nil
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	if c.closed {
		c.mu.Unlock()
		return &cpeerr.Error{Op: "mqtt.Connect", Kind: cpeerr.KindInvalidArgument, Err: errors.New("client already closed")}
	}
	c.mu.Unlock()

	scheme := "tcp"
	if c.opts.TLSConfig != nil {
		scheme = "ssl"
	}
	brokerURL := fmt.Sprintf("%s://%s:%d", scheme, c.opts.BrokerHost, c.opts.BrokerPort)

	pahoOpts := paho.NewClientOptions().
		AddBroker(brokerURL).
		SetProtocolVersion(pahoProtocolVersion311).
		SetCleanSession(c.opts.CleanSession).
		SetKeepAlive(c.opts.KeepAlive).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(time.Second).
		SetMaxReconnectInterval(time.Minute).
		SetConnectTimeout(defaultConnectTimeout).
		SetOnConnectHandler(c.onConnect).
		SetConnectionLostHandler(c.onConnectionLost).
		SetTLSConfig(c.opts.TLSConfig)

	if c.opts.Username != "" {
		pahoOpts.SetUsername(c.opts.Username)
	}
	if c.opts.Password != "" {
		pahoOpts.SetPassword(c.opts.Password)
	}

	client := paho.NewClient(pahoOpts)
	token := client.Connect()

	select {
	case <-ctx.Done():
		return &cpeerr.Error{Op: "mqtt.Connect", Kind: cpeerr.KindInternal, Err: ctx.Err()}
	case <-tokenDone(token):
	}
	if err := token.Error(); err != nil {
		return &cpeerr.Error{Op: "mqtt.Connect", Kind: cpeerr.KindInternal, Err: err}
	}

	c.mu.Lock()
	c.client = client
	c.connected = true
	c.mu.Unlock()

	c.logger.Info("usp mqtt connected", "broker", brokerURL, "eid", c.opts.EndpointID)
	return nil
}

func (c *Client) Send(ctx context.Context, record []byte) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return &cpeerr.Error{Op: "mqtt.Send", Kind: cpeerr.KindInvalidArgument, Err: errors.New("Connect not called")}
	}
	token := client.Publish(mtp.TopicControllerInbox, 1, false, record)
	select {
	case <-ctx.Done():
		return &cpeerr.Error{Op: "mqtt.Send", Kind: cpeerr.KindInternal, Err: ctx.Err()}
	case <-tokenDone(token):
	}
	if err := token.Error(); err != nil {
		return &cpeerr.Error{Op: "mqtt.Send", Kind: cpeerr.KindInternal, Err: err}
	}
	return nil
}

func (c *Client) Recv() <-chan []byte { return c.recv }

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
	}
	close(c.recv)
	return nil
}

func (c *Client) onConnect(client paho.Client) {
	topic := mtp.TopicAgentInbox(c.opts.EndpointID)
	token := client.Subscribe(topic, 0, c.onMessage)
	token.Wait()
	if err := token.Error(); err != nil {
		c.logger.Warn("usp mqtt subscribe failed", "topic", topic, "err", err)
		return
	}
	c.logger.Info("usp mqtt subscribed", "topic", topic)
}

func (c *Client) onMessage(_ paho.Client, msg paho.Message) {
	payload := make([]byte, len(msg.Payload()))
	copy(payload, msg.Payload())
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	select {
	case c.recv <- payload:
	default:
		c.logger.Warn("usp mqtt recv buffer full, dropped message", "topic", msg.Topic(), "bytes", len(payload))
	}
}

func (c *Client) onConnectionLost(_ paho.Client, err error) {
	c.logger.Warn("usp mqtt connection lost", "eid", c.opts.EndpointID, "err", err)
}

func tokenDone(t paho.Token) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		t.Wait()
		close(ch)
	}()
	return ch
}
