package gxyhttp

import (
	"context"
	"gserver/core/gxyservice"
	"gserver/util"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/gstructs"
)

type HttpService struct {
	gxyservice.PublicService
}

func (h *HttpService) Host() string {
	return httpSys.Address()
}

func (h *HttpService) OnModInit(ctx context.Context) error {
	return nil
}

// 响应结构
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (h *HttpService) SetHandler(ctx context.Context, name string, handler any) {
	server := httpSys.server
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

func (h *HttpService) GetReqUri(req any) string {
	fs, _ := gstructs.TagFields(req, []string{"path"})
	if len(fs) > 0 {
		return fs[0].TagValue
	}
	return util.GetObjectName(req)
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
