package logic

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

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/util/gconv"
)

type RoleGM struct {
	RoleModule
	ctx context.Context
}

var _ IRoleModule = (*RoleGM)(nil)

// ========== 帮助提取 ==========

type CmdDoc struct {
	Name    string
	Brief   string
	Usage   string
	Example string
}

var (
	gmCmdDocs []CmdDoc
	gmCmdMap  map[string]reflect.Method
	gmInited  bool
)

func gmEnsureInited() {
	if gmInited {
		return
	}
	gmInited = true
	gmCmdDocs = gmExtractDocs()
	gmCmdMap = gmBuildCmdMap()
}

// gmExtractDocs 用 go/doc 解析 role_gm.go 源文件，提取带 "用法:" 注释的方法作为 GM 命令
func gmExtractDocs() []CmdDoc {
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
			if t.Name != "RoleGM" {
				continue
			}
			var docs []CmdDoc
			for _, m := range t.Methods {
				if !m.Decl.Name.IsExported() {
					continue
				}
				// 只提取带 "用法:" 注释的方法（区分 GM 命令和 proto handler）
				if !strings.Contains(m.Doc, "用法:") && !strings.Contains(m.Doc, "用法：") {
					continue
				}
				cmdName := camelToSnake(m.Name)
				d := gmParseCmdDoc(cmdName, m.Doc)
				docs = append(docs, d)
			}
			return docs
		}
	}
	return nil
}

func gmParseCmdDoc(name string, docStr string) CmdDoc {
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

func gmBuildCmdMap() map[string]reflect.Method {
	m := make(map[string]reflect.Method)
	t := reflect.TypeFor[*RoleGM]()
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		if !method.IsExported() {
			continue
		}
		cmdName := camelToSnake(method.Name)
		if _, ok := m[cmdName]; !ok {
			m[cmdName] = method
		}
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

// ========== 命令路由 ==========

func (r *RoleGM) execCommand(name string, args []string) error {
	gmEnsureInited()
	method, ok := gmCmdMap[name]
	if !ok {
		return fmt.Errorf("unknown command: %s", name)
	}
	in := make([]reflect.Value, 1+len(args))
	in[0] = reflect.ValueOf(r)
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

// ========== Proto Handler ==========

func (r *RoleGM) ReqGMCommand(ctx context.Context, req *pb.ReqGMCommand) (*pb.RspGMCommand, error) {
	parts := strings.Fields(req.Cmd)
	if len(parts) == 0 {
		return &pb.RspGMCommand{Result: "empty command"}, nil
	}
	r.ctx = ctx
	err := r.execCommand(parts[0], parts[1:])
	if err != nil {
		return &pb.RspGMCommand{Result: err.Error()}, nil
	}
	return &pb.RspGMCommand{Result: "ok"}, nil
}

func (r *RoleGM) ReqGMHelp(ctx context.Context, req *pb.ReqGMHelp) (*pb.RspGMHelp, error) {
	gmEnsureInited()
	cmds := make([]*pb.PCmdDoc, 0, len(gmCmdDocs))
	for _, d := range gmCmdDocs {
		cmds = append(cmds, &pb.PCmdDoc{
			Name:    d.Name,
			Brief:   d.Brief,
			Usage:   d.Usage,
			Example: d.Example,
		})
	}
	return &pb.RspGMHelp{Commands: cmds}, nil
}

// ========== GM 命令定义 ==========
// 每个命令方法的注释即为帮助文档，格式：
// 第一行：简要说明
// 用法: 命令名 [参数...]
// 示例: 命令名 参数值...
//
// 带 "用法:" 注释的方法会被 go/doc 识别为 GM 命令

// AddGoods 添加物品或货币
// 用法: add_goods [物品ID] [数量]
// 示例: add_goods 1001 10
func (r *RoleGM) AddGoods(itemID int, num int) error {
	return r.Role.Bag.SaveGoods(r.ctx, nil, []*gamecfg.GardenGoodStack{MakeGoodStack(itemID, int(num))}, "gm")
}

// RemoveGoods 移除物品或货币
// 用法: remove_goods [物品ID] [数量]
// 示例: remove_goods 1001 5
func (r *RoleGM) RemoveGoods(itemID int, num int) error {
	return r.Role.Bag.SaveGoods(r.ctx, []*gamecfg.GardenGoodStack{MakeGoodStack(itemID, int(num))}, nil, "gm")
}
