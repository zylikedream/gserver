package endpoint

import (
	"net"

	"gserver/core/gxynet/message"
	"gserver/core/gxynet/processor"
)

type TcpEndpoint struct {
	conn net.Conn
	proc processor.Processor
	data any
}

func NewTcpEndPoint(conn net.Conn, proc processor.Processor) *TcpEndpoint {
	return &TcpEndpoint{
		proc: proc,
		conn: conn,
	}
}

func (t *TcpEndpoint) DecodeMsg(data []byte) (*message.Message, int, error) {
	pkgLen, msg, err := t.proc.Decode(data)
	if err != nil {
		return nil, 0, err
	}
	if msg == nil {
		return nil, 0, nil
	}
	return msg, int(pkgLen), nil
}

func (t *TcpEndpoint) SendData(data []byte, path string) error {
	msg := message.NewMessage(data, path)
	return t.sendRaw(msg)
}

func (t *TcpEndpoint) SendMsg(msg any) error {
	return t.sendRaw(&message.Message{
		Msg: msg,
	})
}

func (t *TcpEndpoint) sendRaw(msg *message.Message) error {
	payload, err := t.proc.Encode(msg)
	if err != nil {
		return err
	}
	if _, err = t.conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func (t *TcpEndpoint) Close() {
	t.conn.Close()
}

func (t *TcpEndpoint) Conn() net.Conn {
	return t.conn
}

func (t *TcpEndpoint) GetData() any {
	return t.data
}

func (t *TcpEndpoint) SetData(d any) {
	t.data = d
}
