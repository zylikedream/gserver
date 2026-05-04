package logic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

var repoRoot string

func init() {
	// 从当前目录向上查找 go.mod 来确定仓库根目录
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			repoRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			repoRoot = "."
			break
		}
		dir = parent
	}
}

// loadTestTable 从 gameconfig/json/ 加载指定配表，支持追加额外行。
// 这样配表结构变更（如新增必填字段）后，测试自动跟随，无需手写。
func loadTestTable(t *testing.T, name string, extras ...map[string]any) []map[string]any {
	t.Helper()
	path := filepath.Join(repoRoot, "gameconfig/json/"+name+".json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load table %s: %v", name, err)
	}
	var data []map[string]any
	if err := json.Unmarshal(bytes, &data); err != nil {
		t.Fatalf("unmarshal table %s: %v", name, err)
	}
	data = append(data, extras...)
	return data
}
