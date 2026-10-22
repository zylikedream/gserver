package peer

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cockroachdb/errors"

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

	// booted 在 OnBoot 时关闭:监听已绑定,Start 可以返回成功。
	// 它的另一用途是区分 gnet.Run 的返回原因——未关闭时返回即启动失败。
	booted   chan struct{}
	bootOnce sync.Once
}

type tcpServerConfig struct {
	Addr string `toml:"addr"`
}

// tcpServerStartTimeout 是等待监听到位的时间上限。
// 超过即视为启动失败,由模块树统一收尾,而不是让进程在后台"假装启动成功"。
const tcpServerStartTimeout = 10 * time.Second

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
	t.booted = make(chan struct{})
	startErr := make(chan error, 1)
	go func() {
		err := gnet.Run(t, t.conf.Addr,
			gnet.WithMulticore(true),
			gnet.WithLogger(logger.NewGnetLogger()), gnet.WithLogLevel(logging.DebugLevel),
			gnet.WithReuseAddr(true))
		select {
		case <-t.booted:
			// 已启动成功:这是运行期退出(主动 Stop 时 err 为 nil)。
			// 启动结果早已交回模块树,这里只能致命退出,由进程管理器处理。
			if err != nil {
				gxylog.Fatal(ctx, "gnet run failed", gxylog.Err(err))
			}
		default:
			// 尚未 boot:绑定失败,把错误交回 Start,由模块树统一收尾。
			if err == nil {
				err = errors.New("gnet run exited before boot")
			}
			startErr <- err
		}
	}()
	select {
	case err := <-startErr:
		return err
	case <-t.booted:
		return nil
	case <-time.After(tcpServerStartTimeout):
		return errors.Newf("gnet start timeout after %s, addr: %s", tcpServerStartTimeout, t.conf.Addr)
	}
}

func (t *TcpServer) Type() string {
	return PEER_TCP_SERVER
}

func (t *TcpServer) Stop(ctx context.Context) error {
	if t.engine == (gnet.Engine{}) {
		return nil
	}
	return t.engine.Stop(ctx)
}

func (t *TcpServer) OnBoot(eng gnet.Engine) gnet.Action {
	t.engine = eng
	// OnBoot 时监听已绑定(Run 先 Listen 再进入引擎循环),启动可以返回了。
	if t.booted != nil {
		t.bootOnce.Do(func() { close(t.booted) })
	}
	return gnet.None
}

func (t *TcpServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	gxymetrics.TcpConnections.WithLabelValues("server").Inc()
	endPoint := endpoint.NewTcpEndPoint(c, t.Processor)
	c.SetContext(endPoint)
	if err := t.Handler.OnOpen(endPoint); err != nil {
		gxylog.Error(ctx, "handler onOpen failed", gxylog.Err(err))
	}
	return nil, gnet.None
}

func (t *TcpServer) OnTraffic(c gnet.Conn) gnet.Action {
	endPoint := c.Context().(*endpoint.TcpEndpoint)
	data, err := c.Peek(-1)
	if err != nil {
		gxylog.Error(ctx, "get traffic data failed", gxylog.Str("addr", c.RemoteAddr().String()), gxylog.Err(err))
		return gnet.Close
	}
	dataLen := 0
	for {
		msg, pkgLen, err := endPoint.DecodeMsg(data)
		if err != nil {
			gxylog.Error(ctx, "decode msg failed",
				gxylog.Str("addr", c.RemoteAddr().String()),
				gxylog.Err(err), gxylog.Any("rawData", data))
			return gnet.Close
		}

		if msg == nil {
			break
		}
		if pkgLen <= 0 || pkgLen > len(data) {
			gxylog.Error(ctx, "invalid decoded package length",
				gxylog.Str("addr", c.RemoteAddr().String()),
				gxylog.Num("pkgLen", pkgLen),
				gxylog.Num("dataLen", len(data)),
				gxylog.Any("rawData", data))
			return gnet.Close
		}
		dataLen += pkgLen
		data = data[pkgLen:]
		if err := t.Handler.OnMessage(endPoint, msg); err != nil {
			gxylog.Error(ctx, "handler onMessage failed", gxylog.Err(err))
		}
	}
	if dataLen > 0 {
		if _, err = c.Discard(dataLen); err != nil {
			return gnet.Close
		}
	}
	return gnet.None
}

func (t *TcpServer) OnClose(c gnet.Conn, err error) gnet.Action {
	gxymetrics.TcpConnections.WithLabelValues("server").Dec()
	endPoint := c.Context().(*endpoint.TcpEndpoint)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Handler.OnClose(endPoint, err)
	} else {
		t.Handler.OnClose(endPoint, nil)
	}
	return gnet.None
}
