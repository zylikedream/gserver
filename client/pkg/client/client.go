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
	Addr       string
	AccountUID string
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
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:      cfg,
		pendings: make(map[string]*pendingRequest),
	}
}

func (c *Client) Connect() error {
	c.conn = NewConn(c.handleMessage)
	return c.conn.Connect(c.cfg.Addr)
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) SetAccountUID(uid string) {
	c.cfg.AccountUID = uid
}

func (c *Client) RoleID() int64 {
	return c.roleID
}

func (c *Client) Handshake() (*pb.RspHandShake, error) {
	req := &pb.ReqHandShake{AccountUid: c.cfg.AccountUID}

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
	if c.onResponse != nil {
		c.onResponse(rsp)
	}
	return nil
}

func (c *Client) RequestWithResponse(msg proto.Message) (proto.Message, error) {
	return c.doRequest(msg, c.conn.Send)
}

func (c *Client) OnMessage(handler func(msg proto.Message)) {
	c.onMessage = handler
}

func (c *Client) OnResponse(handler func(msg proto.Message)) {
	c.onResponse = handler
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
		return rsp, nil
	case <-time.After(requestTimeout):
		return nil, fmt.Errorf("timeout waiting for response %s", rspID)
	}
}

func (c *Client) handleMessage(msg *Message) {
	if msg == nil {
		return
	}

	protoMsg := NewMessageByID(msg.Path)
	if protoMsg == nil {
		return
	}

	if err := proto.Unmarshal(msg.Payload, protoMsg); err != nil {
		return
	}

	c.mu.Lock()
	pending, ok := c.pendings[msg.Path]
	c.mu.Unlock()

	if ok {
		pending.ch <- protoMsg
		return
	}

	if c.onMessage != nil {
		c.onMessage(protoMsg)
	}
}

func responseIDFor(req proto.Message) string {
	reqID := IDByMessage(req)
	id, err := strconv.ParseUint(reqID, 10, 32)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(id+1, 10)
}
