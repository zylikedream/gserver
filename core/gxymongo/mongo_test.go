package gxymongo

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestReplaceOne(t *testing.T) {
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("config/db.test.toml")
	client := NewMongoApp()
	if err := client.OnModInit(context.Background()); err != nil {
		t.Fatalf("OnModInit failed: %v", err)
	}
	if err := client.OnModStart(context.Background()); err != nil {
		t.Fatalf("OnModStart failed: %v", err)
	}
	filter := bson.M{
		"role_id": 10016,
		"version": 2,
	}
	update := bson.M{
		"$set": bson.M{
			"name":    "test",
			"version": 3,
		},
	}
	result, err := client.UpdateOne(context.Background(), "role_basic_state", filter, update, options.Update().SetUpsert(true))
	if err != nil {
		t.Fatalf("ReplaceOne failed: %#v ", err)
	} else {
		t.Logf("ReplaceOne success, %#v", result)
	}
}
