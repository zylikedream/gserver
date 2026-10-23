package gxyredis

import (
	"context"
	"sync"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/redis/go-redis/v9"
)

// redisConfig 是 Redis 连接配置。
//
// 只暴露拨号超时:读/写超时、连接池大小、PoolTimeout 沿用 go-redis 的默认值
// (读/写 3s、池 10×CPU、PoolTimeout 4s),需要调时再加字段——不预置没人会设的项。
type redisConfig struct {
	Addr     string        `toml:"addr"`
	DB       int           `toml:"db"`
	Timeout  time.Duration `toml:"dial_timeout"` // 仅拨号超时,不是命令超时
	Password string        `toml:"password"`
}

type Client redis.UniversalClient

type redisApp struct {
	gxyapp.App
	conf   *redisConfig
	client Client
}

var app *redisApp

// notInitOnce 让"未初始化"的错误只记一次:配置缺失时消费方可能反复取客户端,
// 每次刷一条日志没有信息增量。
var notInitOnce sync.Once

// Redis 返回全局客户端;未初始化(配置缺失)或已关闭时返回 nil。
//
// 返回 nil 只发生在启动期的配置缺失;消费方必须判空后再用(见 RedisMQ.Start)。
func Redis() Client {
	if app == nil || app.client == nil {
		notInitOnce.Do(func() {
			gxylog.Error(context.Background(), "redis not initialized: missing config or already closed")
		})
		return nil
	}
	return app.client
}

func NewRedisApp() *redisApp {
	app = &redisApp{}
	return app
}

func (r *redisApp) OnModInit(ctx context.Context) error {
	conf := &redisConfig{}
	// 判断配置文件是否存在
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "redis", conf); err != nil {
		return err
	}
	if conf.Addr == "" {
		return nil
	}
	r.conf = conf
	r.client = newClient(conf)
	r.client.AddHook(metricsHook{})
	return nil
}

// newClient 按配置构造客户端。
//
// 独立成函数是为了让两条选项决策可被测试固定(见 client_test.go),而不是散在
// 构造字面量里、被后人"顺手简化"掉。
func newClient(conf *redisConfig) Client {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:       []string{conf.Addr},
		Password:    conf.Password,
		DB:          conf.DB,
		DialTimeout: conf.Timeout,

		// 让调用方的 ctx 真正生效:默认(false)时 go-redis 会把 I/O 用的 ctx
		// 换成 context.Background(),调用方设的超时/取消在命令执行上完全无效,
		// 只剩读/写超时兜底(见 go-redis baseClient.context)。
		ContextTimeoutEnabled: true,
		// 关闭库的自动重试(默认 3 次):这里存的是游戏状态而非纯缓存,连接断开
		// 后的重放可能让非幂等命令(INCR 等)执行两次;而且默认重试会把"读超时"
		// 也放大成 4 次尝试。要重试的调用方自己决定。
		MaxRetries: -1,
	})
}

func (r *redisApp) OnModStart(ctx context.Context) error {
	if r.client == nil {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := r.client.Ping(timeoutCtx).Err(); err != nil {
		return err
	}
	gxylog.Info(ctx, "redis start success", gxylog.Str("addr", r.conf.Addr))
	return nil
}

func (r *redisApp) OnModStop(ctx context.Context) error {
	if r.client != nil {
		err := r.client.Close()
		// 清空引用:停机后再取客户端应得到"未初始化",而不是一个已关闭的客户端。
		r.client = nil
		if err != nil {
			return err
		}
	}
	gxylog.Info(ctx, "redis stop success")
	return nil
}
