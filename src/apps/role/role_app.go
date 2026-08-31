package role

import (
	"context"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxymetrics"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic"
	"gserver/src/lib"
	"gserver/src/lib/rolelib"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"

	"github.com/gogf/gf/v2/frame/g"
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
	// 严格解析限流配置: 失败必须在 Actor kind 注册之前终止启动。
	limitConfig, err := logic.LoadRoleLimitConfig(ctx, g.Cfg())
	if err != nil {
		return err
	}
	logic.SetRoleLimitConfig(limitConfig)
	setRoleLimitMetrics(limitConfig)
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

// setRoleLimitMetrics 根据启动时解析的不可变限流策略, 为每个业务模块设置 disabled 指标。
// 只在 Role 配置加载后调用一次; 不要在单个 Role Actor 内设置。
func setRoleLimitMetrics(config logic.RoleLimitConfig) {
	for module, policy := range config.Modules {
		disabled := 0.0
		if policy.Disabled {
			disabled = 1
		}
		gxymetrics.RoleModuleDisabled.WithLabelValues(module).Set(disabled)
	}
}

func GetRolePublic(ctx context.Context, roleID int64) *pb.PRolePublic {
	return logic.GetRolePublic(ctx, deps.Deps{DB: gxypgx.DB(), Redis: gxyredis.Redis(), Cfg: gameconfig.Get()}, roleID)
}
