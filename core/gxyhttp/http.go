package gxyhttp

import (
	"context"
	"fmt"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"net/http"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/gclient"
	"github.com/gogf/gf/v2/net/ghttp"
)

var httpSys *httpSystem

func HttpSystem() *httpSystem {
	return httpSys
}

type httpSystem struct {
	gxymodule.ModuleBase
	nodeName string
	host     string
	server   *ghttp.Server
	client   *gclient.Client
}

func NewHttpSystem(nodeName string, host string) *httpSystem {
	svr := g.Server()
	svr.SetAddr(fmt.Sprintf("%s:%d", host, 0))
	svr.SetLogger(gxylog.GetLogger().Clone())
	svr.SetLogLevel("debug")
	httpSys = &httpSystem{
		nodeName: nodeName,
		host:     host,
		server:   svr,
		client:   gclient.New(),
	}
	return httpSys
}

func (h *httpSystem) Address() string {
	return h.server.GetListenedAddress()
}

func (h *httpSystem) OnModStart(ctx context.Context) error {
	if err := h.server.Start(); err != nil {
		return err
	}
	return nil
}

func (h *httpSystem) RegisterObject(ctx context.Context, obj any) error {
	h.server.Shutdown()
	return nil
}

func (h *httpSystem) Server() *ghttp.Server {
	return h.server
}

func (h *httpSystem) PostService(ctx context.Context, service string, uri string, msg ...any) (*Response, error) {
	info := gxyservice.ServiceManager().GetServiceInfo(ctx, service, "", gxyregistery.RoundRobinSelector())
	if info == nil {
		return nil, gerror.Newf("service(%s) not found", service)
	}
	url := fmt.Sprintf("http://%s/%s/%s", info.NodeHost, service, uri)
	return h.Post(ctx, url, msg...)

}

func (h *httpSystem) Post(ctx context.Context, url string, msg ...any) (*Response, error) {
	rsp, err := h.client.Post(ctx, url, msg...)
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

func (h *httpSystem) GetService(ctx context.Context, service string, msg ...any) (*Response, error) {
	info := gxyservice.ServiceManager().GetServiceInfo(ctx, service, "", gxyregistery.RoundRobinSelector())
	if info == nil {
		return nil, gerror.Newf("service(%s) not found", service)
	}
	url := fmt.Sprintf("http://%s", info.NodeHost)
	return h.Get(ctx, url, msg...)

}

func (h *httpSystem) Get(ctx context.Context, url string, msg ...any) (*Response, error) {
	rsp, err := h.client.Get(ctx, url, msg...)
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
