package gxylocator

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

// locatorConfig 定义定位器的配置结构
type locatorConfig struct {
	RedisKeyPrefix string        `toml:"redis_key_prefix"` // Redis 键前缀
	Type           string        `toml:"type"`             // 定位器类型
	ExpireTime     time.Duration `toml:"expire_time"`      // 缓存过期时间
}

// Locator 是定位器模块的实现
type Locator struct {
	conf *locatorConfig
}

func NewNodeLocator(prefix string, timeout time.Duration) *Locator {
	return NewLocator(prefix, "node", timeout)
}

// NewLocator 创建并初始化一个新的定位器实例
func NewLocator(prefix string, ltype string, timeout time.Duration) *Locator {
	l := &Locator{
		conf: &locatorConfig{
			RedisKeyPrefix: prefix,
			Type:           ltype,
			ExpireTime:     timeout,
		},
	}
	return l
}

func (l *Locator) formatKey(key string) string {
	return fmt.Sprintf("%s:%s:%s", l.conf.RedisKeyPrefix, l.conf.Type, key)
}

// Locate 根据键查找对应的节点
func (l *Locator) Locate(ctx context.Context, key string) (string, error) {
	redisCli := gxyredis.GetRedis()
	redisKey := l.formatKey(key)
	result, err := redisCli.Get(ctx, redisKey)
	if err != nil {
		return "", gerror.Wrap(err, "Failed to locate key in Redis")
	}

	if result.IsEmpty() {
		return "", nil // 键不存在
	}

	return result.String(), nil
}

// Register 注册键和节点的映射关系
func (l *Locator) Register(ctx context.Context, key string, node string) error {
	redisCli := gxyredis.GetRedis()
	redisKey := l.formatKey(key)
	err := redisCli.SetEX(ctx, redisKey, node, int64(l.conf.ExpireTime.Seconds()))
	if err != nil {
		return gerror.Wrap(err, "Failed to register key in Redis")
	}

	glog.Debug(ctx, "Registered key-node mapping", "key", key, "node", node)
	return nil
}

func (l *Locator) Unregister(ctx context.Context, key string) error {
	redisCli := gxyredis.GetRedis()
	redisKey := l.formatKey(key)
	_, err := redisCli.Del(ctx, redisKey)
	if err != nil {
		return gerror.Wrap(err, "Failed to unregister key in Redis")
	}

	glog.Debug(ctx, "Unregistered key-node mapping", "key", key)
	return nil
}
