package gm

import (
	"context"
	"fmt"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"unicode"

	"gserver/src/apps/role/internal/logic"
	"gserver/src/apps/role/internal/logic/bag"

	"github.com/gogf/gf/v2/util/gconv"
)

// CmdDoc 命令文档
type CmdDoc struct {
	Name    string
	Brief   string
	Usage   string
	Example string
}

// GM 命令处理器，持有 RoleMain 引用，在 actor 上下文内执行
type GM struct {
	role *logic.RoleMain
	ctx  context.Context
}

func NewGM(ctx context.Context, role *logic.RoleMain) *GM {
	return &GM{role: role, ctx: ctx}
}

// ========== 帮助提取 ==========

var (
	cmdDocs []CmdDoc
	cmdMap  map[string]reflect.Method
	inited  bool
)

func ensureInited() {
	if inited {
		return
	}
	inited = true
	cmdDocs = extractDocs()
	cmdMap = buildCmdMap()
}

// extractDocs 用 go/doc 解析 gm.go 源文件提取命令文档
func extractDocs() []CmdDoc {
	_, filePath, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filePath)
	filename := filepath.Base(filePath)

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return fi.Name() == filename
	}, parser.ParseComments)
	if err != nil {
		return nil
	}

	for _, pkg := range pkgs {
		p := doc.New(pkg, "./", 0)
		for _, t := range p.Types {
			if t.Name != "GM" {
				continue
			}
			var docs []CmdDoc
			for _, m := range t.Methods {
				if !m.Decl.Name.IsExported() {
					continue
				}
				cmdName := camelToSnake(m.Name)
				d := parseCmdDoc(cmdName, m.Doc)
				docs = append(docs, d)
			}
			return docs
		}
	}
	return nil
}

func parseCmdDoc(name string, docStr string) CmdDoc {
	d := CmdDoc{Name: name}
	lines := strings.Split(docStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if d.Brief == "" {
			d.Brief = line
		}
		if strings.HasPrefix(line, "用法:") || strings.HasPrefix(line, "用法：") {
			d.Usage = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "用法:"), "用法："))
		}
		if strings.HasPrefix(line, "示例:") || strings.HasPrefix(line, "示例：") {
			d.Example = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "示例:"), "示例："))
		}
	}
	return d
}

// buildCmdMap 构建命令名到 reflect.Method 的映射
func buildCmdMap() map[string]reflect.Method {
	m := make(map[string]reflect.Method)
	t := reflect.TypeFor[*GM]()
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		if !method.IsExported() {
			continue
		}
		cmdName := camelToSnake(method.Name)
		m[cmdName] = method
	}
	return m
}

func camelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

// GetCmdDocs 返回命令文档列表
func GetCmdDocs() []CmdDoc {
	ensureInited()
	return cmdDocs
}

// ========== 命令路由 ==========

// ExecCommand 执行 GM 命令
func (g *GM) ExecCommand(name string, args []string) error {
	ensureInited()
	method, ok := cmdMap[name]
	if !ok {
		return fmt.Errorf("unknown command: %s", name)
	}
	// 构建参数：receiver + args...
	in := make([]reflect.Value, 1+len(args))
	in[0] = reflect.ValueOf(g)
	for i, arg := range args {
		paramType := method.Type.In(i + 1)
		val := gconv.Convert(arg, paramType.Kind().String())
		in[i+1] = reflect.ValueOf(val).Convert(paramType)
	}
	results := method.Func.Call(in)
	if len(results) > 0 {
		if err, ok := results[0].Interface().(error); ok && err != nil {
			return err
		}
	}
	return nil
}

// ========== GM 命令定义 ==========
// 每个命令方法的注释即为帮助文档，格式：
// 第一行：简要说明
// 用法: 命令名 [参数...]
// 示例: 命令名 参数值...

// AddGoods 添加物品或货币
// 用法: add_goods [物品ID] [数量]
// 示例: add_goods 1001 10
func (g *GM) AddGoods(itemID int, num uint64) error {
	return g.role.Bag.AddItem(g.ctx, []bag.Item{
		{ID: itemID, Num: num},
	})
}

// RemoveGoods 移除物品或货币
// 用法: remove_goods [物品ID] [数量]
// 示例: remove_goods 1001 5
func (g *GM) RemoveGoods(itemID int, num uint64) error {
	return g.role.Bag.DecItem(g.ctx, []bag.Item{
		{ID: itemID, Num: num},
	})
}
