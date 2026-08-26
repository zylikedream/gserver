package peer

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
	"gserver/core/gxynet/processor"

	"github.com/panjf2000/gnet/v2"
)

// ---- fakes ----

// fakeConn 实现 gnet.Conn;仅 OnTraffic/OnOpen 用到的成员有行为。
type fakeConn struct {
	data      []byte
	ctx       any
	discarded int
}

func (f *fakeConn) Peek(n int) ([]byte, error) { return f.data, nil }
func (f *fakeConn) Discard(n int) (int, error) {
	f.discarded += n
	f.data = f.data[n:]
	return n, nil
}
func (f *fakeConn) Context() any     { return f.ctx }
func (f *fakeConn) SetContext(c any) { f.ctx = c }
func (f *fakeConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 25011}
}
func (f *fakeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10086}
}

// Reader
func (f *fakeConn) Read(p []byte) (int, error)      { return 0, io.EOF }
func (f *fakeConn) WriteTo(w io.Writer) (int64, error) { return 0, nil }
func (f *fakeConn) Next(n int) ([]byte, error)      { return nil, io.ErrShortBuffer }
func (f *fakeConn) InboundBuffered() int            { return len(f.data) }

// Writer
func (f *fakeConn) Write(p []byte) (int, error)        { return len(p), nil }
func (f *fakeConn) ReadFrom(r io.Reader) (int64, error) { return 0, nil }
func (f *fakeConn) SendTo(buf []byte, addr net.Addr) (int, error) {
	return len(buf), nil
}
func (f *fakeConn) Writev(bs [][]byte) (int, error)       { return 0, nil }
func (f *fakeConn) Flush() error                          { return nil }
func (f *fakeConn) OutboundBuffered() int                 { return 0 }
func (f *fakeConn) AsyncWrite(buf []byte, cb gnet.AsyncCallback) error {
	return nil
}
func (f *fakeConn) AsyncWritev(bs [][]byte, cb gnet.AsyncCallback) error {
	return nil
}

// Socket
func (f *fakeConn) Fd() int                        { return 0 }
func (f *fakeConn) Dup() (int, error)              { return 0, nil }
func (f *fakeConn) SetReadBuffer(size int) error   { return nil }
func (f *fakeConn) SetWriteBuffer(size int) error  { return nil }
func (f *fakeConn) SetLinger(secs int) error       { return nil }
func (f *fakeConn) SetKeepAlivePeriod(d time.Duration) error {
	return nil
}
func (f *fakeConn) SetKeepAlive(enabled bool, idle, intvl time.Duration, cnt int) error {
	return nil
}
func (f *fakeConn) SetNoDelay(noDelay bool) error { return nil }

// Conn
func (f *fakeConn) EventLoop() gnet.EventLoop { return nil }
func (f *fakeConn) Wake(cb gnet.AsyncCallback) error {
	return nil
}
func (f *fakeConn) CloseWithCallback(cb gnet.AsyncCallback) error {
	return nil
}
func (f *fakeConn) Close() error              { return nil }
func (f *fakeConn) SetDeadline(t time.Time) error {
	return nil
}
func (f *fakeConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (f *fakeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

var _ gnet.Conn = (*fakeConn)(nil)

// procResult 一次 Decode 的返回值;queue 为空时返回"无消息"。
type procResult struct {
	pkgLen uint64
	msg    *message.Message
	err    error
}

type fakeProcessor struct {
	results []procResult
}

func (f *fakeProcessor) Decode(data []byte) (uint64, *message.Message, error) {
	if len(f.results) == 0 {
		return 0, nil, nil
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r.pkgLen, r.msg, r.err
}

func (f *fakeProcessor) Encode(msg *message.Message) ([]byte, error) {
	return nil, nil
}

var _ processor.Processor = (*fakeProcessor)(nil)

type fakeHandler struct {
	opened   int
	messages []*message.Message
	closed   int
}

func (f *fakeHandler) OnOpen(ep endpoint.Endpoint) error {
	f.opened++
	return nil
}

func (f *fakeHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeHandler) OnClose(ep endpoint.Endpoint, err error) {
	f.closed++
}

func (f *fakeHandler) OnError(ep endpoint.Endpoint, err error) {
}

var _ endpoint.EventHandler = (*fakeHandler)(nil)

// ---- helpers ----

func newTestServer(proc processor.Processor, h endpoint.EventHandler) *TcpServer {
	s := &TcpServer{conf: &tcpServerConfig{Addr: "tcp://127.0.0.1:0"}}
	s.Processor = proc
	s.Handler = h
	return s
}

// trafficConn 返回绑定好 TcpEndpoint 的 fake conn。
func trafficConn(proc processor.Processor, data []byte) *fakeConn {
	c := &fakeConn{data: data}
	c.SetContext(endpoint.NewTcpEndPoint(nil, proc))
	return c
}

// ---- tests ----

func TestTcpServerOnTraffic_SingleMessage(t *testing.T) {
	proc := &fakeProcessor{results: []procResult{{pkgLen: 5, msg: &message.Message{Path: "test"}}}}
	h := &fakeHandler{}
	s := newTestServer(proc, h)

	if action := s.OnTraffic(trafficConn(proc, []byte("12345"))); action != gnet.None {
		t.Fatalf("action = %v, want None", action)
	}
	if len(h.messages) != 1 {
		t.Fatalf("OnMessage called %d times, want 1", len(h.messages))
	}
	if h.messages[0].Path != "test" {
		t.Errorf("msg path = %q, want test", h.messages[0].Path)
	}
}

func TestTcpServerOnTraffic_MultipleMessages(t *testing.T) {
	proc := &fakeProcessor{results: []procResult{
		{pkgLen: 3, msg: &message.Message{Path: "a"}},
		{pkgLen: 2, msg: &message.Message{Path: "b"}},
	}}
	h := &fakeHandler{}
	s := newTestServer(proc, h)
	c := trafficConn(proc, []byte("abcde"))

	if action := s.OnTraffic(c); action != gnet.None {
		t.Fatalf("action = %v, want None", action)
	}
	if len(h.messages) != 2 {
		t.Fatalf("OnMessage called %d times, want 2", len(h.messages))
	}
	if c.discarded != 5 {
		t.Errorf("discarded = %d, want 5", c.discarded)
	}
}

func TestTcpServerOnTraffic_PartialFrameWaits(t *testing.T) {
	// 半包:Decode 返回 nil 消息 → 停止拆包,不调用 OnMessage,不 Discard。
	proc := &fakeProcessor{results: []procResult{{pkgLen: 0, msg: nil}}}
	h := &fakeHandler{}
	s := newTestServer(proc, h)
	c := trafficConn(proc, []byte("ab"))

	if action := s.OnTraffic(c); action != gnet.None {
		t.Fatalf("action = %v, want None", action)
	}
	if len(h.messages) != 0 {
		t.Errorf("OnMessage called %d times for partial frame", len(h.messages))
	}
	if c.discarded != 0 {
		t.Errorf("discarded = %d, want 0 for partial frame", c.discarded)
	}
}

func TestTcpServerOnTraffic_DecodeErrorCloses(t *testing.T) {
	proc := &fakeProcessor{results: []procResult{{err: errors.New("decode boom")}}}
	h := &fakeHandler{}
	s := newTestServer(proc, h)

	if action := s.OnTraffic(trafficConn(proc, []byte("bad"))); action != gnet.Close {
		t.Fatalf("action = %v, want Close", action)
	}
	if len(h.messages) != 0 {
		t.Errorf("OnMessage called on decode error")
	}
}

func TestTcpServerOnTraffic_ZeroPkgLenCloses(t *testing.T) {
	proc := &fakeProcessor{results: []procResult{{pkgLen: 0, msg: &message.Message{}}}}
	h := &fakeHandler{}
	s := newTestServer(proc, h)

	if action := s.OnTraffic(trafficConn(proc, []byte("xx"))); action != gnet.Close {
		t.Fatalf("action = %v, want Close for zero pkgLen", action)
	}
}

func TestTcpServerOnTraffic_PkgLenExceedsDataCloses(t *testing.T) {
	proc := &fakeProcessor{results: []procResult{{pkgLen: 100, msg: &message.Message{}}}}
	h := &fakeHandler{}
	s := newTestServer(proc, h)

	if action := s.OnTraffic(trafficConn(proc, []byte("short"))); action != gnet.Close {
		t.Fatalf("action = %v, want Close for pkgLen > data", action)
	}
}

func TestTcpServerOnOpenBindsEndpoint(t *testing.T) {
	h := &fakeHandler{}
	s := newTestServer(&fakeProcessor{}, h)
	c := &fakeConn{}

	if _, action := s.OnOpen(c); action != gnet.None {
		t.Fatalf("action = %v, want None", action)
	}
	if h.opened != 1 {
		t.Errorf("handler opened = %d, want 1", h.opened)
	}
	if _, ok := c.Context().(*endpoint.TcpEndpoint); !ok {
		t.Errorf("conn context = %T, want *TcpEndpoint", c.Context())
	}
}

func TestTcpServerOnCloseNotifiesHandler(t *testing.T) {
	h := &fakeHandler{}
	s := newTestServer(&fakeProcessor{}, h)
	c := &fakeConn{}
	c.SetContext(endpoint.NewTcpEndPoint(nil, &fakeProcessor{}))

	s.OnClose(c, nil)
	if h.closed != 1 {
		t.Errorf("handler closed = %d, want 1", h.closed)
	}
}

func TestTcpConnectorType(t *testing.T) {
	connector := &TcpConnector{}
	if got := connector.Type(); got != PEER_TCP_CONNECTOR {
		t.Errorf("Type() = %q, want %q", got, PEER_TCP_CONNECTOR)
	}
}

func TestNewPeerUnknownType(t *testing.T) {
	if _, err := NewPeer("unknown", nil); err == nil {
		t.Fatal("NewPeer(unknown) = nil, want error")
	}
}
