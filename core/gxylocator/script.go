package gxylocator

import (
	"context"
	"gserver/core/gxyredis"
	"gserver/util"
	"os"
	"path/filepath"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

var CommonScript *redis.Script

func init() {
	dir := util.GetCurrenDir()
	commonScriptPath := filepath.Join(dir, "script", "common.lua")
	commonScriptSrc, err := os.ReadFile(commonScriptPath)
	glog.Debugf(context.Background(), "common script path: %s", commonScriptPath)
	if err != nil {
		panic(err)
	}
	CommonScript = redis.NewScript(string(commonScriptSrc))
}

func UnregisterGrainNode(rdb gxyredis.Client, id string, node string) (int64, error) {
	keys := []string{"func", "id", "node"}
	args := []string{"unregister_grain_node", id, node}
	return CommonScript.Run(context.Background(), rdb, keys, args).Int64()
}
