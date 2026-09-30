package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScan 覆盖门禁的判据与已知误报源。
// 判据:包级变量「生产只读、测试可写」= 违规;两侧都写或都没写 = 不违规。
func TestScan(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		// 生产文件
		"a.go": `package a

import "context"

var seamFn = func(ctx context.Context) error { return nil }

var prodOnly string

var bothWritten string

var deadVar = func() {}

var ctx = context.Background()

func use() { _ = seamFn; _ = prodOnly; _ = bothWritten; _ = deadVar; _ = ctx }
`,
		// 生产侧也写 prodOnly 与 bothWritten(组装根注入),故两者都不算违规
		"b.go": `package a

func setup() {
	prodOnly = "from-assembly-root"
	bothWritten = "from-test-and-prod"
}
`,
		// 测试文件
		"a_test.go": `package a

import (
	"context"
	"testing"
)

func TestSeam(t *testing.T) {
	seamFn = func(ctx context.Context) error { return nil } // 违规:只有测试写
	bothWritten = "test"                                    // 不违规:生产也写
}

// 同名局部变量的赋值不得被当成写包级变量。
func TestLocalShadow(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, "k", "v")
	_ = ctx
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	got, err := scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	found := map[string]bool{}
	for _, v := range got {
		found[filepath.Base(v.File)+" "+v.Name] = true
	}

	if !found["a.go seamFn"] {
		t.Errorf("seamFn 应违规(只有测试写),实际结果 %v", got)
	}
	for _, name := range []string{"prodOnly", "bothWritten", "deadVar", "ctx"} {
		if found["a.go "+name] {
			t.Errorf("%s 不应违规(生产也写 / 无写入方 / 局部同名)", name)
		}
	}
	if len(found) != 1 {
		t.Errorf("违规数 = %d, want 1", len(found))
	}
}

// TestReadBaseline 基线文件:空行与 # 注释忽略,键为「文件 变量名」(不含行号)。
func TestReadBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.txt")
	body := "# 注释\n\nsrc/x.go seam\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	known := readBaseline(path)
	if !known["src/x.go seam"] {
		t.Errorf("基线未解析出条目: %v", known)
	}
	if len(known) != 1 {
		t.Errorf("条目数 = %d, want 1", len(known))
	}
	if missing := readBaseline(filepath.Join(t.TempDir(), "absent.txt")); len(missing) != 0 {
		t.Errorf("基线缺失时应为空, got %v", missing)
	}
}
