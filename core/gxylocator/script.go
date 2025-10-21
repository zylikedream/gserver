package gxylocator

import (
	"context"
	"gserver/core/gxyredis"
	"os"

	"github.com/redis/go-redis/v9"
)

var CommonScript *redis.Script

func init() {
	commonScriptSrc, err := os.ReadFile("script/common.lua")
	if err != nil {
		panic(err)
	}
	CommonScript = redis.NewScript(string(commonScriptSrc))
}

func UnregisterGrainNode(rdb gxyredis.Client, id string, node string) (int64, error) {
	keys := []string{"id", "node"}
	args := []string{id, node}
	return CommonScript.Run(context.Background(), rdb, keys, args).Int64()
}
