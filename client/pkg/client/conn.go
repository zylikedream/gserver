package client

import (
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
)

type MessageHandler func(msg *Message)

type Conn struct {
	addr    string
	conn    net.Conn
	codec   *LTIVCodec
	handler MessageHandler
	mu      sync.Mutex
	buf     []byte
	closed  bool
}

func NewConn(handler MessageHandler) *Conn {
	return &Conn{
		codec:   NewLTIVCodec(),
		handler: handler,
		buf:     make([]byte, 0, 4096),
	}
}

func (c *Conn) Connect(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}
	c.conn = conn
	c.addr = addr
	c.closed = false
	go c.readLoop()
	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Send sends a protobuf message as a data packet (Type=1)
func (c *Conn) Send(msg proto.Message) error {
	id := IDByMessage(msg)
	if id == "" {
		return fmt.Errorf("unknown message type: %T", msg)
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}
	return c.SendRaw(&Message{
		Type:    1,
		Path:    id,
		Payload: payload,
	})
}

// SendFirst sends a protobuf message as a first/handshake packet (Type=0)
func (c *Conn) SendFirst(msg proto.Message) error {
	id := IDByMessage(msg)
	if id == "" {
		return fmt.Errorf("unknown message type: %T", msg)
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}
	return c.SendRaw(&Message{
		Type:    0,
		Path:    id,
		Payload: payload,
	})
}

// SendRaw sends a raw Message with LTIV encoding
func (c *Conn) SendRaw(msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	data, err := c.codec.Encode(msg)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(data)
	return err
}

func (c *Conn) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

func (c *Conn) readLoop() {
	recvBuf := make([]byte, 4096)
	for {
		n, err := c.conn.Read(recvBuf)
		if err != nil {
			if !c.closed {
				c.mu.Lock()
				c.closed = true
				c.mu.Unlock()
				c.handler(nil) // nil signals disconnection
			}
			return
		}

		c.buf = append(c.buf, recvBuf[:n]...)

		for {
			consumed, msg, err := c.codec.Decode(c.buf)
			if err != nil || msg == nil {
				break
			}
			c.buf = c.buf[consumed:]
			c.handler(msg)
		}

		if len(c.buf) == 0 {
			c.buf = make([]byte, 0, 4096)
		}
	}
}
