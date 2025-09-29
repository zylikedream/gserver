package gxylocator

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

const (
	LocatorTypeNode = "node"
)

// locatorConfig 定义定位器的配置结构
type locatorConfig struct {
	RedisKeyPrefix string        `toml:"redis_key_prefix"` // Redis 键前缀
	ExpireTime     time.Duration `toml:"expire_time"`      // 缓存过期时间
}

// Locator 是定位器模块的实现
type Locator struct {
	conf *locatorConfig
}

// NewLocator 创建并初始化一个新的定位器实例
func NewLocator(prefix string, timeout time.Duration) *Locator {
	l := &Locator{
		conf: &locatorConfig{
			RedisKeyPrefix: prefix,
			ExpireTime:     timeout,
		},
	}
	return l
}

func (l *Locator) formatKey(t string, key string) string {
	return fmt.Sprintf("%s:locate:%s:%s", l.conf.RedisKeyPrefix, t, key)
}

// LocateNode 根据键查找对应的节点
func (l *Locator) LocateNode(ctx context.Context, key string) (string, error) {
	return l.Locate(ctx, LocatorTypeNode, key)
}

// Locate 根据键查找对应的节点
func (l *Locator) Locate(ctx context.Context, t string, key string) (string, error) {
	redisCli := gxyredis.GetRedis()
	redisKey := l.formatKey(t, key)
	result, err := redisCli.Get(ctx, redisKey)
	if err != nil {
		return "", gerror.Wrap(err, "Failed to locate key in Redis")
	}

	if result.IsEmpty() {
		return "", nil // 键不存在
	}

	return result.String(), nil
}

func (l *Locator) RegisterNode(ctx context.Context, key string, node string) error {
	return l.Register(ctx, LocatorTypeNode, key, node)
}

// Register 注册键和节点的映射关系
func (l *Locator) Register(ctx context.Context, t string, key string, node string) error {
	redisCli := gxyredis.GetRedis()
	redisKey := l.formatKey(t, key)
	succ, err := redisCli.SetNX(ctx, redisKey, node)
	if err != nil {
		return gerror.Wrap(err, "Failed to register key in Redis")
	}
	if !succ {
		return gerror.New("Key already registered")
	}
	redisCli.Expire(ctx, redisKey, int64(l.conf.ExpireTime.Seconds()))

	return nil
}

func (l *Locator) UnregisterNode(ctx context.Context, key string) error {
	return l.Unregister(ctx, LocatorTypeNode, key)
}

func (l *Locator) Unregister(ctx context.Context, t string, key string) error {
	redisCli := gxyredis.GetRedis()
	redisKey := l.formatKey(t, key)
	_, err := redisCli.Del(ctx, redisKey)
	if err != nil {
		return gerror.Wrap(err, "Failed to unregister key in Redis")
	}

	glog.Debug(ctx, "Unregistered key-node mapping", "key", key)
	return nil
}
