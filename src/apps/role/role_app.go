package role

import (
	"context"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic"
	"gserver/src/lib"
	"gserver/src/pkg/gameconfig"
)

type roleApp struct {
	gxyapp.App
}

func NewRoleApp() *roleApp {
	return &roleApp{}
}

func (r *roleApp) OnModInit(ctx context.Context) error {
	r.AddModule(ctx, gameconfig.NewGameConfig())
	logic.InitRoleSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewRoleActorService())

	r.AddModule(ctx, lib.NewRoleNotify())
	// 广播模块：订阅系统消息，持久化后推送给所有在线玩家
	r.AddModule(ctx, lib.NewBroadcast("role", func(ctx context.Context, topic string, msg *lib.BroadcastMsg) *lib.BroadcastMsg {
		switch msg.MsgType {
		case lib.BroadCastTypeSystemMsg:
			lib.NotifyLocalAll(ctx, &pb.NotifyChatSystem{
				Message: &pb.PChatMsg{
					Content:   msg.Data,
					Timestamp: time.Now().Unix(),
				},
			})
		}
		return nil
	}))

	return nil
}

func (r *roleApp) OnModStop(ctx context.Context) error {
	return nil
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	return logic.GetRolePublic(ctx, roleID)
}

func GetRoleIDByAccount(ctx context.Context, account string) (int64, error) {
	return logic.GetRoleIDByAccount(ctx, account)
}
