package peer

import (
	"context"
	"fmt"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/logger"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
)

type TcpServer struct {
	endpoint.CoreBundle
	conf *tcpServerConfig
	gnet.BuiltinEventEngine
	engine gnet.Engine
}

type tcpServerConfig struct {
	Addr string `toml:"addr"`
}

var ctx = gxylog.NewContext(context.Background(), "TcpServer")

func newTcpServer(c *gcfg.Config) (*TcpServer, error) {
	server := &TcpServer{}
	conf := &tcpServerConfig{}
	if err := gxyutil.CfgUnmarshalKey(ctx, c, server.Type(), conf); err != nil {
		return nil, err
	}
	conf.Addr = fmt.Sprintf("tcp://%s", conf.Addr)
	server.conf = conf
	if err := server.BindProc(c); err != nil {
		return nil, err
	}
	return server, nil
}

func (t *TcpServer) Init() error {
	return nil
}

func (t *TcpServer) Start(ctx context.Context, el endpoint.EventHandler) error {
	t.Handler = el
	go func() {
		if err := gnet.Run(t, t.conf.Addr,
			gnet.WithMulticore(true),
			gnet.WithLogger(logger.NewGnetLogger()), gnet.WithLogLevel(logging.DebugLevel),
			gnet.WithReuseAddr(true)); err != nil {
			gxylog.Fatal(ctx, "gnet run failed", gxylog.Err(err))
		}

	}()
	return nil
}

func (t *TcpServer) Type() string {
	return PEER_TCP_SERVER
}

func (t *TcpServer) Stop(ctx context.Context) error {
	return t.engine.Stop(ctx)
}

func (t *TcpServer) OnBoot(eng gnet.Engine) gnet.Action {
	t.engine = eng
	return gnet.None
}

func (t *TcpServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	gxymetrics.TcpConnections.WithLabelValues("server").Inc()
	endPoint := endpoint.NewTcpEndPoint(c, t.Processor)
	c.SetContext(endPoint)
	t.Handler.OnOpen(endPoint)
	return nil, gnet.None
}

func (t *TcpServer) OnTraffic(c gnet.Conn) gnet.Action {
	endPoint := c.Context().(*endpoint.TcpEndpoint)
	data, err := c.Peek(-1)
	if err != nil {
		gxylog.Error(ctx, "get traffic data failed", gxylog.Err(err))
		return gnet.Close
	}
	dataLen := 0
	for {
		msg, len, err := endPoint.DecodeMsg(data)
		if err != nil {
			gxylog.Error(context.Background(), "decode msg failed", gxylog.Err(err), gxylog.Any("rawData", data))
			return gnet.Close
		}

		if msg == nil {
			break
		}
		dataLen += len
		data = data[len:]
		t.Handler.OnMessage(endPoint, msg)
	}
	if dataLen > 0 {
		c.Discard(dataLen)
	}
	return gnet.None
}

func (t *TcpServer) OnClose(c gnet.Conn, err error) gnet.Action {
	gxymetrics.TcpConnections.WithLabelValues("server").Dec()
	endPoint := c.Context().(*endpoint.TcpEndpoint)
	t.Handler.OnClose(endPoint, err)
	if err != nil {
		gxylog.Error(ctx, "conn close", gxylog.Str("addr", c.RemoteAddr().String()), gxylog.Err(err))
	}
	return gnet.None
}
