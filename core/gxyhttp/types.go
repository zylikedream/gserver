package gxyhttp

import (
	"context"
	"gserver/core/gxyservice"
	"gserver/util"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gstructs"
)

type HttpService struct {
	msgHandler util.MsgHandler
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

func (h *HttpService) SetHandler(name string, handler any) {
	h.msgHandler.AddHandler(handler)
	server := httpSys.server
	server.Group("/"+name, func(group *ghttp.RouterGroup) {
		group.Bind(handler)
		group.Middleware(ghttp.MiddlewareHandlerResponse)
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
