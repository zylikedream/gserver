package gxylocator

import (
	"context"
	"gserver/core/gxyredis"
	"gserver/core/gxyutil"
	"os"
	"path/filepath"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

var locateScript *redis.Script
var batchRegisterScript *redis.Script

func init() {
	dir := gxyutil.GetCurrenDir()
	locateScriptPath := filepath.Join(dir, "script", "locate.lua")
	locateScriptSrc, err := os.ReadFile(locateScriptPath)
	glog.Debugf(context.Background(), "common script path: %s", locateScriptPath)
	if err != nil {
		panic(err)
	}
	locateScript = redis.NewScript(string(locateScriptSrc))
	batchRegisterScript = redis.NewScript(`
		local keys = KEYS
		local ttl = tonumber(ARGV[1])
		for i = 1, #keys, 2 do
			redis.call('SETEX', keys[i], ttl, keys[i+1])
		end
		return #keys / 2
	`)

}

func ScriptUnregisterActorNode(rdb gxyredis.Client, id string, node string) (int64, error) {
	keys := []string{"func", "id", "node"}
	args := []string{"unregister_actor_node", id, node}
	return locateScript.Run(context.Background(), rdb, keys, args).Int64()
}

func ScriptRegisterActorNode(ctx context.Context, rdb gxyredis.Client, keys []string, ttl int64) (int64, error) {
	return batchRegisterScript.Run(ctx, rdb, keys, ttl).Int64()
}
