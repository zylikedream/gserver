package gateway

import (
	"context"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"

	"github.com/cockroachdb/errors"
)

type GateHandler struct {
	endpoint.BaseEventHandler
}

func NewGateHandler() *GateHandler {
	return &GateHandler{}
}

func (gh *GateHandler) OnOpen(ep endpoint.Endpoint) error {
	ctx := context.Background()
	connID := ep.Conn().RemoteAddr().String()
	gxylog.Debug(ctx, "New connection", gxylog.Str("connID", connID))

	// 通过SessionManager创建Session Actor
	sessPid, err := spawnSession(ep)
	if err != nil {
		gxylog.Error(ctx, "Failed to create session for %s", gxylog.Str("connID", connID), gxylog.Err(err))
		_ = ep.Conn().Close()
		return err
	}
	ep.SetData(sessPid)
	gxylog.Debug(ctx, "Session Actor created with PID", gxylog.Any("pid", sessPid))
	return nil
}

func (gh *GateHandler) OnMessage(ep endpoint.Endpoint, msg *message.Message) error {
	sess, ok := ep.GetData().(gxyactor.PID)
	if !ok {
		gxylog.Error(context.Background(), "failed to get session from endpoint data")
		return nil
	}
	_ = gxyactor.Send(context.Background(), sess, msg)
	// 消息将直接由Session Actor处理
	return nil
}

func (gh *GateHandler) OnClose(ep endpoint.Endpoint, err error) {
	sessPid, ok := ep.GetData().(gxyactor.PID)
	if !ok || gxyactor.PIDIsZero(sessPid) {
		return
	}
	// err=nil 即客户端干净断开,这是最常见的一条下线路径。此处不编造原因:
	// 终止原因会一路上到运行时,非空的原因会让运行时把每次正常下线记成
	// "process terminated abnormally" 的 error(见 stopSession)。
	if err == nil {
		_ = stopSession(sessPid, nil)
		return
	}
	_ = stopSession(sessPid, errors.Wrap(err, "conn closed"))
}
