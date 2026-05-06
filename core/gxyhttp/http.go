package gxyhttp

import (
	"context"
	"fmt"
	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"net/http"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

var app *httpApp

func HttpSystem() *httpApp {
	return app
}

type httpApp struct {
	gxyapp.App
}

func NewHttpApp() *httpApp {
	return &httpApp{}
}

func (h *httpApp) NewHttpServer(host string) *ghttp.Server {
	svr := g.Server()
	svr.SetAddr(fmt.Sprintf("%s:%d", host, 0))
	svr.SetLogger(gxylog.GetLogger().Clone())
	svr.SetLogLevel("debug")
	g.Client()
	return svr
}

func (h *httpApp) OnModStart(ctx context.Context) error {
	return nil
}

func (h *httpApp) PostService(ctx context.Context, service string, uri string, msg ...any) (*Response, error) {
	info := gxyservice.ServiceApp().GetServiceInfo(ctx, service, "", gxyregistery.RoundRobinSelector())
	if info == nil {
		return nil, gerror.Newf("service(%s) not found", service)
	}
	url := fmt.Sprintf("http://%s/%s/%s", info.NodeHost, service, uri)
	return h.Post(ctx, url, msg...)

}

func (h *httpApp) Post(ctx context.Context, url string, msg ...any) (*Response, error) {
	rsp, err := g.Client().Post(ctx, url, msg...)
	if err != nil {
		return nil, gerror.Newf("post error: %v ", err)
	}
	if rsp.StatusCode != http.StatusOK {
		return nil, gerror.Newf("post error status, code(%d) msg(%s)", rsp.StatusCode, rsp.Status)
	}
	result := &Response{}
	data := rsp.ReadAll()
	if err := gjson.Unmarshal(data, result); err != nil {
		return nil, gerror.Wrapf(err, "unmarsharl error, url: %s, raw: %s", url, string(data))
	}
	if result.Code != 0 {
		return nil, gerror.Newf("post error code(%d) msg(%s)", result.Code, result.Message)
	}
	return result, nil
}

func (h *httpApp) GetService(ctx context.Context, service string, msg ...any) (*Response, error) {
	info := gxyservice.ServiceApp().GetServiceInfo(ctx, service, "", gxyregistery.RoundRobinSelector())
	if info == nil {
		return nil, gerror.Newf("service(%s) not found", service)
	}
	url := fmt.Sprintf("http://%s", info.NodeHost)
	return h.Get(ctx, url, msg...)

}

func (h *httpApp) Get(ctx context.Context, url string, msg ...any) (*Response, error) {
	rsp, err := g.Client().Get(ctx, url, msg...)
	if err != nil {
		return nil, gerror.Newf("get error: %v ", err)
	}
	if rsp.StatusCode != http.StatusOK {
		return nil, gerror.Newf("get error status, code(%d) msg(%s)", rsp.StatusCode, rsp.Status)
	}
	result := &Response{}
	data := rsp.ReadAll()
	if err := gjson.Unmarshal(data, result); err != nil {
		return nil, gerror.Wrapf(err, "unmarsharl error, url: %s, raw: %s", url, string(data))
	}
	if result.Code != 0 {
		return nil, gerror.Newf("get error code(%d) msg(%s)", result.Code, result.Message)
	}
	return result, nil
}
