package gxylocator

import (
	"context"
	"fmt"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestLocate(t *testing.T) {
	// g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("../gxyredis/config/redis.test.toml")
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("game.toml")
	ctx := context.Background()
	redisApp := gxyredis.NewRedisApp()
	if err := redisApp.OnModStart(ctx); err != nil {
		t.Errorf("Failed to start redis app, error:%v", err)
		return
	}

	// 1. 模拟写入200w个pid
	const count = 2000000
	const prefix = "actor:test:actor:"

	t.Logf("开始写入 %d 个pid...", count)
	startTime := time.Now()

	locator := NewLocator("test")
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)
		id := fmt.Sprintf("node1@actor_%d", i)
		pidInfo := newPIDInfo("127.0.0.1", id)
		if err := locator.MustRegisterActor(ctx, key, string(pidInfo), time.Hour); err != nil {
			t.Errorf("Failed to register node %s, error:%v", key, err)
			return
		}
	}
	writeTime := time.Since(startTime)
	t.Logf("写入 %d 个pid完成，耗时: %v", count, writeTime)

	// 2. 测试写入新pid的时间
	newKey := prefix + "new_test"
	newPid := newPIDInfo("127.0.0.1", "node1@actor_new")

	newWriteStart := time.Now()
	locator.MustRegisterActor(ctx, newKey, newPid, time.Hour)
	newWriteTime := time.Since(newWriteStart)
	t.Logf("写入单个新pid耗时: %v", newWriteTime)

	// 3. 测试locate时间
	locateStart := time.Now()
	result, err := locator.LocateNode(ctx, newKey)
	if err != nil {
		t.Errorf("locate err %s", err)
		return
	}
	locateTime := time.Since(locateStart)
	t.Logf("locate时间: %v, 结果: %s", locateTime, result)

	// 4. 随机locate多个已存在的pid以测试平均时间
	const locateCount = 1000
	randomLocateStart := time.Now()
	successCount := 0
	for i := 0; i < locateCount; i++ {
		// 随机选择一个已写入的pid进行locate
		idx := i % count
		key := fmt.Sprintf("%s%d", prefix, idx)
		_, err := locator.LocateNode(ctx, key)
		if err == nil {
			successCount++
		}
	}
	randomLocateTime := time.Since(randomLocateStart)
	t.Logf("随机locate %d 个pid完成，成功 %d 个，总耗时: %v, 平均每个耗时: %v",
		locateCount, successCount, randomLocateTime, randomLocateTime/time.Duration(locateCount))

	// 5. 清理测试数据
	t.Logf("测试完成")
}

func newPIDInfo(address string, id string) string {
	pidInfo, _ := protojson.Marshal(&pb.ActorPid{
		Address: address,
		Id:      id,
	})
	return string(pidInfo)
}
