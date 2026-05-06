package gxyhttp

import (
	"context"
	"gserver/core/gxyservice"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/glog"
)

type HttpService struct {
	gxyservice.Service
	Svr *ghttp.Server
}

func (h *HttpService) Host() string {
	return h.Svr.GetListenedAddress()
}

// 响应结构
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func SetHandler(server *ghttp.Server, ctx context.Context, name string, handler any) {
	if server.Status() == ghttp.ServerStatusRunning {
		glog.Warningf(ctx, "http server is running, can not set handler")
		return
	}
	server.Group("/"+name, func(group *ghttp.RouterGroup) {
		// 先注册中间件，再绑定处理器，确保中间件生效
		group.Middleware(ghttp.MiddlewareHandlerResponse)
		group.Bind(handler)
	})
}

func NewErrCode(code int, msg string) *ErrCode {
	return &ErrCode{ErrCode: code, Msg: msg}
}

// ErrCode 错误代码
type ErrCode struct {
	ErrCode int
	Msg     string
}

func (e *ErrCode) Error() string {
	return e.Msg
}

func (e *ErrCode) Code() int {
	return e.ErrCode
}

func (e *ErrCode) Message() string {
	return e.Msg
}

func (e *ErrCode) Detail() string {
	return e.Msg
}
