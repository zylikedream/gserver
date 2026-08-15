package role

import (
	"context"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic"
	"gserver/src/lib"
	"gserver/src/lib/rolelib"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"
)

type roleApp struct {
	gxyapp.App
}

func NewRoleApp() *roleApp {
	return &roleApp{}
}

func onBroadcast(ctx context.Context, _topic string, msg *lib.BroadcastMsg) *lib.BroadcastMsg {
	switch msg.MsgType {
	case lib.BroadCastTypeSystemMsg:
		_ = rolelib.NotifyLocalAll(ctx, &pb.NotifyChatSystem{
			Message: &pb.PChatMsg{
				Content:   msg.Data,
				Timestamp: time.Now().Unix(),
			},
		})
	}
	return nil
}

func (r *roleApp) OnModInit(ctx context.Context) error {
	if err := r.AddModule(ctx, gameconfig.NewGameConfig()); err != nil {
		return err
	}
	logic.InitRoleSchema(ctx, gxypgx.DB())
	gxyservice.ServiceApp().LoadService(ctx, NewRoleActorService())

	if err := r.AddModule(ctx, rolelib.NewRoleNotify()); err != nil {
		return err
	}
	// 广播模块：订阅系统消息，持久化后推送给所有在线玩家
	if err := r.AddModule(ctx, lib.NewBroadcast("role", onBroadcast)); err != nil {
		return err
	}

	return nil
}

func (r *roleApp) OnModStop(ctx context.Context) error {
	return nil
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	return logic.GetRolePublic(ctx, deps.Deps{DB: gxypgx.DB(), Redis: gxyredis.Redis(), Cfg: gameconfig.Get()}, roleID)
}
