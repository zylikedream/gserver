package peer

import (
	"context"

	"gserver/core/gxynet/endpoint"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/os/gcfg"
	"gserver/core/gxylog"
	"github.com/panjf2000/gnet/v2"
)

type TcpConnector struct {
	endpoint.CoreBundle
	conf *tcpConnectorConfig
	gnet.BuiltinEventEngine
	engine gnet.Engine
}

type tcpConnectorConfig struct {
	Addr string `toml:"addr"`
}

func newTcpConnector(cfg *gcfg.Config) (*TcpConnector, error) {
	server := &TcpConnector{}
	conf := &tcpConnectorConfig{}
	ctx := context.Background()
	if err := gxyutil.CfgUnmarshalKey(ctx, cfg, server.Type(), conf); err != nil {
		return nil, err
	}
	server.conf = conf
	if err := server.BindProc(cfg); err != nil {
		return nil, err
	}
	return server, nil
}

func (t *TcpConnector) Init() error {
	return nil
}

func (t *TcpConnector) Start(ctx context.Context, h endpoint.EventHandler) error {
	t.BindHandler(h)
	cli, err := gnet.NewClient(t)
	if err != nil {
		return err
	}
	if err = cli.Start(); err != nil {
		return err
	}
	_, err = cli.Dial("tcp", t.conf.Addr)
	if err != nil {
		return err
	}
	err = cli.Start()
	if err != nil {
		return err
	}
	return nil
}

func (t *TcpConnector) Type() string {
	return PEER_TCP_CONNECTOR
}

func (t *TcpConnector) Stop(ctx context.Context) error {
	return nil
}

func (t *TcpConnector) OnBoot(eng gnet.Engine) gnet.Action {
	t.engine = eng
	return gnet.None
}

func (t *TcpConnector) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	endPoint := endpoint.NewTcpEndPoint(c, t.Processor)
	c.SetContext(endPoint)
	t.Handler.OnOpen(endPoint)
	return nil, gnet.None
}

func (t *TcpConnector) OnTraffic(c gnet.Conn) gnet.Action {
	endPoint := c.Context().(*endpoint.TcpEndpoint)
	data, err := c.Next(-1)
	if err != nil {
		gxylog.Error(context.Background(), "get traffic data failed", gxylog.Err(err))
		return gnet.Close
	}
	for {
		msg, len, err := endPoint.DecodeMsg(data)
		if err != nil {
			gxylog.Error(context.Background(), "decode msg failed", gxylog.Err(err), gxylog.Any("rawData", data))
			return gnet.Close
		}
		if msg == nil {
			break
		}
		_, err = c.Discard(len)
		if err != nil {
			return gnet.Close
		}
		data = data[len:]
		t.Handler.OnMessage(endPoint, msg)
	}
	return gnet.None
}

func (t *TcpConnector) OnClose(c gnet.Conn, err error) gnet.Action {
	gxylog.Error(context.Background(), "conn close", gxylog.Str("addr", c.RemoteAddr().String()), gxylog.Err(err))
	return gnet.None
}
