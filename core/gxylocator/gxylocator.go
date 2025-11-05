package gxylocator

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"
)

const (
	LocatorTypeNode = "node"
)

// locatorConfig 定义定位器的配置结构
type locatorConfig struct {
	RedisKeyPrefix string `toml:"redis_key_prefix"` // Redis 键前缀
}

// Locator 是定位器模块的实现
type Locator struct {
	conf *locatorConfig
}

// NewLocator 创建并初始化一个新的定位器实例
func NewLocator(prefix string) *Locator {
	l := &Locator{
		conf: &locatorConfig{
			RedisKeyPrefix: prefix,
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
	redisCli := gxyredis.Redis()
	redisKey := l.formatKey(t, key)
	result, err := redisCli.Get(ctx, redisKey).Result()
	if err != nil && err != redis.Nil {
		return "", gerror.Newf("Failed to locate key %s in Redis, error:%v", redisKey, err)
	}

	if result == "" {
		return "", nil // 键不存在
	}

	return result, nil
}

func (l *Locator) RegisterNode(ctx context.Context, key string, node string, expireTime time.Duration) error {
	return l.Register(ctx, LocatorTypeNode, key, node, expireTime)
}

// Register 注册键和节点的映射关系
func (l *Locator) Register(ctx context.Context, t string, key string, node string, expireTime time.Duration) error {
	redisCli := gxyredis.Redis()
	redisKey := l.formatKey(t, key)
	_, err := redisCli.Set(ctx, redisKey, node, expireTime).Result()
	if err != nil {
		return gerror.Wrap(err, "Failed to register key in Redis")
	}

	return nil
}

func (l *Locator) UnregisterNode(ctx context.Context, key string, node string) error {
	return l.Unregister(ctx, LocatorTypeNode, key, node)
}

func (l *Locator) Unregister(ctx context.Context, t string, key string, val string) error {
	// 验证节点是否匹配
	redisCli := gxyredis.Redis()
	redisKey := l.formatKey(t, key)
	_, err := UnregisterGrainNode(redisCli, redisKey, val)
	if err != nil {
		return gerror.Wrap(err, "Failed to unregister key in Redis")
	}
	return nil
}
