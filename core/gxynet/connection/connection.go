package connection

import (
	"sync/atomic"
	"time"
)

/////////////////////////////////////////
// connection interface
/////////////////////////////////////////

type Connection interface {
	ID() uint32
	LocalAddr() string
	RemoteAddr() string
	incReadPkgNum()
	incWritePkgNum()
	// update conn's active time
	UpdateActive()
	// get conn's active time
	GetActive() time.Time
	readTimeout() time.Duration
	// SetReadTimeout sets deadline for the future read calls.
	SetReadTimeout(time.Duration)
	writeTimeout() time.Duration
	// SetWriteTimeout sets deadline for the future read calls.
	SetWriteTimeout(time.Duration)
	send(interface{}) (int, error)
	// don't distinguish between tcp connection and websocket connection. Because
	// gorilla/websocket/conn.go:(Conn)Close also invoke net.Conn.Close
	close(int)
}

type baseConn struct {
	id            uint32
	padding1      uint8
	padding2      uint16
	readBytes     uint32        // read bytes
	writeBytes    uint32        // write bytes
	readPkgNum    uint32        // send pkg number
	writePkgNum   uint32        // recv pkg number
	active        int64         // last active, in milliseconds
	rTimeout      time.Duration // gxynet current limiting
	wTimeout      time.Duration
	rLastDeadline time.Time // lastest gxynet read time
	wLastDeadline time.Time // lastest gxynet write time
	launchTime    time.Time // 启动时间
	local         string    // local address
	peer          string    // peer address
}

func (c *baseConn) ID() uint32 {
	return c.id
}

func (c *baseConn) LocalAddr() string {
	return c.local
}

func (c *baseConn) RemoteAddr() string {
	return c.peer
}

func (c *baseConn) incReadPkgNum() {
	atomic.AddUint32(&c.readPkgNum, 1)
}

func (c *baseConn) incWritePkgNum() {
	atomic.AddUint32(&c.writePkgNum, 1)
}

func (c *baseConn) UpdateActive() {
	atomic.StoreInt64(&(c.active), int64(time.Since(c.launchTime)))
}

func (c *baseConn) GetActive() time.Time {
	return c.launchTime.Add(time.Duration(atomic.LoadInt64(&(c.active))))
}

func (c *baseConn) send(interface{}) (int, error) {
	return 0, nil
}

func (c *baseConn) close(int) {}

func (c baseConn) readTimeout() time.Duration {
	return c.rTimeout
}

// Pls do not set read deadline for websocket connection. AlexStocks 20180310
// gorilla/websocket/conn.go:NextReader will always fail when got a timeout error.
//
// Pls do not set read deadline when using compression. AlexStocks 20180314.
func (c *baseConn) SetReadTimeout(rTimeout time.Duration) {
	if rTimeout < 1 {
		panic("@rTimeout < 1")
	}

	c.rTimeout = rTimeout
	if c.wTimeout == 0 {
		c.wTimeout = rTimeout
	}
}

func (c baseConn) writeTimeout() time.Duration {
	return c.wTimeout
}

// Pls do not set write deadline for websocket connection. AlexStocks 20180310
// gorilla/websocket/conn.go:NextWriter will always fail when got a timeout error.
//
// Pls do not set write deadline when using compression. AlexStocks 20180314.
func (c *baseConn) SetWriteTimeout(wTimeout time.Duration) {
	if wTimeout < 1 {
		panic("@wTimeout < 1")
	}

	c.wTimeout = wTimeout
	if c.rTimeout == 0 {
		c.rTimeout = wTimeout
	}
}
