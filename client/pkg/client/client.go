package client

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
)

const requestTimeout = 10 * time.Second

type Config struct {
	Addr string
}

type pendingRequest struct {
	rspID string
	ch    chan proto.Message
}

type Client struct {
	cfg     Config
	conn    *Conn
	roleID  int64

	mu       sync.Mutex
	pendings map[string]*pendingRequest

	onMessage  func(msg proto.Message)
	onResponse func(msg proto.Message)
	onDisconnect func(reason error)
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:      cfg,
		pendings: make(map[string]*pendingRequest),
	}
}

func (c *Client) Connect() error {
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn = NewConn(c.handleMessage)
	return c.conn.Connect(c.cfg.Addr)
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) IsConnected() bool {
	return c.conn != nil && !c.conn.closed
}

func (c *Client) SetAddr(addr string) {
	c.cfg.Addr = addr
}

func (c *Client) RoleID() int64 {
	return c.roleID
}

func (c *Client) Handshake(gateToken string) (*pb.RspHandShake, error) {
	req := &pb.ReqHandShake{GateToken: gateToken}

	rspMsg, err := c.doRequest(req, c.conn.SendFirst)
	if err != nil {
		return nil, fmt.Errorf("handshake: %w", err)
	}

	rsp, ok := rspMsg.(*pb.RspHandShake)
	if !ok {
		return nil, fmt.Errorf("handshake: unexpected response type %T", rspMsg)
	}

	c.roleID = rsp.RoleId
	return rsp, nil
}

func (c *Client) Login() (*pb.RspAccountLogin, error) {
	req := &pb.ReqAccountLogin{
		RoleId:     c.roleID,
		ClientInfo: `{"client":"hy_client","platform":"console"}`,
	}

	rspMsg, err := c.doRequest(req, c.conn.Send)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	rsp, ok := rspMsg.(*pb.RspAccountLogin)
	if !ok {
		return nil, fmt.Errorf("login: unexpected response type %T", rspMsg)
	}

	return rsp, nil
}

func (c *Client) Send(msg proto.Message) error {
	return c.conn.Send(msg)
}

func (c *Client) Request(msg proto.Message) error {
	rsp, err := c.doRequest(msg, c.conn.Send)
	if err != nil {
		return err
	}
	// Check if response is an Ack error
	if ack, ok := rsp.(*pb.Ack); ok && ack.Code != 0 {
		if c.onMessage != nil {
			c.onMessage(ack)
		}
		return fmt.Errorf("ack error: code=%d id=%s reason=%s", ack.Code, ack.Id, ack.Reason)
	}
	if c.onResponse != nil {
		c.onResponse(rsp)
	}
	return nil
}

func (c *Client) RequestWithResponse(msg proto.Message) (proto.Message, error) {
	rsp, err := c.doRequest(msg, c.conn.Send)
	if err != nil {
		return nil, err
	}
	if ack, ok := rsp.(*pb.Ack); ok && ack.Code != 0 {
		if c.onMessage != nil {
			c.onMessage(ack)
		}
		return nil, fmt.Errorf("ack error: code=%d id=%s reason=%s", ack.Code, ack.Id, ack.Reason)
	}
	return rsp, nil
}

func (c *Client) OnMessage(handler func(msg proto.Message)) {
	c.onMessage = handler
}

func (c *Client) OnResponse(handler func(msg proto.Message)) {
	c.onResponse = handler
}

func (c *Client) OnDisconnect(handler func(reason error)) {
	c.onDisconnect = handler
}

type sendFunc func(proto.Message) error

func (c *Client) doRequest(req proto.Message, send sendFunc) (proto.Message, error) {
	rspID := responseIDFor(req)

	ch := make(chan proto.Message, 1)
	c.mu.Lock()
	c.pendings[rspID] = &pendingRequest{rspID: rspID, ch: ch}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendings, rspID)
		c.mu.Unlock()
	}()

	if err := send(req); err != nil {
		return nil, err
	}

	select {
	case rsp := <-ch:
		if rsp == nil {
			return nil, fmt.Errorf("connection closed while waiting for response")
		}
		return rsp, nil
	case <-time.After(requestTimeout):
		return nil, fmt.Errorf("timeout waiting for response %s", rspID)
	}
}

func (c *Client) handleMessage(msg *Message) {
	if msg == nil {
		// Connection lost — cancel all pending requests and notify
		c.mu.Lock()
		for _, p := range c.pendings {
			select {
			case p.ch <- nil:
			default:
			}
		}
		c.pendings = make(map[string]*pendingRequest)
		c.mu.Unlock()

		if c.onDisconnect != nil {
			c.onDisconnect(fmt.Errorf("connection closed by server"))
		}
		return
	}

	protoMsg := NewMessageByID(msg.Path)
	if protoMsg == nil {
		return
	}

	if err := proto.Unmarshal(msg.Payload, protoMsg); err != nil {
		return
	}

	// Try matching by response msg_id
	c.mu.Lock()
	pending, ok := c.pendings[msg.Path]
	c.mu.Unlock()

	if ok {
		pending.ch <- protoMsg
		return
	}

	// Check if this is an Ack error matching a pending request
	if ack, ok := protoMsg.(*pb.Ack); ok && ack.Code != 0 {
		reqID := ack.Id
		rspID := incrementID(reqID)
		if rspID != "" {
			c.mu.Lock()
			pending, ok = c.pendings[rspID]
			c.mu.Unlock()
			if ok {
				pending.ch <- protoMsg
				return
			}
		}
	}

	if c.onMessage != nil {
		c.onMessage(protoMsg)
	}
}

func incrementID(id string) string {
	n, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(n+1, 10)
}

func responseIDFor(req proto.Message) string {
	reqID := IDByMessage(req)
	id, err := strconv.ParseUint(reqID, 10, 32)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(id+1, 10)
}
