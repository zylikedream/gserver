// checktestseam 禁止「为测试留的写入口」:生产代码里不得存在只有测试会写的标识符。
//
// 规则(见 docs/architecture/invariants.md「测试替身规则」):
// 一个包级变量,若生产文件从未写它、而测试文件写过它,即违规——
// 它存在的理由只能是让测试替换行为,这正是测试侵入业务。
// 依赖的正确进入方式是显式参数、构造注入的字段(deps)或真实边界替身。
//
// 只做这一条可判定的规则,不做风格判断。两处已知边界:
//   - 跨包的导出 setter(他包写本包变量)不检测。这类写入口本身要由审查挡。
//   - 同名局部变量的赋值靠"该函数是否声明过这个名字"排除,不做完整作用域分析。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// skipDirs 不参与检查:生成代码、子模块、工具产物。
var skipDirs = map[string]bool{
	".git":       true,
	".tools":     true,
	"bin":        true,
	"dist":       true,
	"pb":         true, // protoc 产出
	"client":     true, // 独立 module,另有 check-client-boundary
	"gameconfig": true, // 子模块
}

type violation struct {
	File string // 变量声明处
	Line int
	Name string
}

// String 报错用:带行号，便于定位。
func (v violation) String() string { return at(v.File, v.Line) + " " + v.Name }

// key 基线比对用:不含行号，避免无关编辑让基线失效。
func (v violation) key() string { return v.File + " " + v.Name }

func at(path string, line int) string { return path + ":" + strconv.Itoa(line) }

func main() {
	root := flag.String("root", ".", "仓库根")
	baselinePath := flag.String("baseline", "", "基线文件:每行一条待清理的违规(逐条清空后删除该文件)")
	flag.Parse()

	violations, err := scan(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	known := readBaseline(*baselinePath)
	var remaining, tolerated []violation
	for _, v := range violations {
		if known[v.key()] {
			tolerated = append(tolerated, v)
			continue
		}
		remaining = append(remaining, v)
	}

	for _, v := range remaining {
		fmt.Fprintf(os.Stderr, "%s: 包级变量只有测试写入,请改为显式参数/构造注入/真实边界替身\n", v)
	}
	if len(tolerated) > 0 {
		fmt.Fprintf(os.Stderr, "基线内待清理 %d 条(%s)\n", len(tolerated), *baselinePath)
	}
	if len(remaining) > 0 {
		os.Exit(1)
	}
	fmt.Printf("test seam OK: 无新增的测试写入口(%d 条已在基线内)\n", len(tolerated))
}

// scan 返回 package 级变量中「生产只读、测试可写」的那些。
func scan(root string) ([]violation, error) {
	pkgs, err := collect(root)
	if err != nil {
		return nil, err
	}
	var out []violation
	for _, p := range pkgs {
		for name, decl := range p.vars {
			if len(p.testWrites[name]) == 0 || len(p.prodWrites[name]) > 0 {
				continue
			}
			out = append(out, violation{File: decl.file, Line: decl.line, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

type declPos struct {
	file string
	line int
}

// pkgVars 一个目录(一个包)的包级变量与写入情况。
type pkgVars struct {
	vars       map[string]declPos
	prodWrites map[string][]string
	testWrites map[string][]string
}

func collect(root string) (map[string]*pkgVars, error) {
	pkgs := map[string]*pkgVars{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		dir := filepath.Dir(path)
		p := pkgs[dir]
		if p == nil {
			p = &pkgVars{
				vars:       map[string]declPos{},
				prodWrites: map[string][]string{},
				testWrites: map[string][]string{},
			}
			pkgs[dir] = p
		}
		isTest := strings.HasSuffix(path, "_test.go")
		for _, d := range f.Decls {
			writeFromDecl(fset, path, d, p, isTest)
		}
		return nil
	})
	return pkgs, err
}

func writeFromDecl(fset *token.FileSet, path string, d ast.Decl, p *pkgVars, isTest bool) {
	pos := func(n ast.Node) string { return at(path, fset.Position(n.Pos()).Line) }

	switch decl := d.(type) {
	case *ast.GenDecl:
		if decl.Tok == token.VAR {
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					line := fset.Position(vs.Pos()).Line
					if !isTest {
						p.vars[name.Name] = declPos{file: path, line: line}
					}
				}
			}
		}
	case *ast.FuncDecl:
		if decl.Body == nil {
			return
		}
		locals := localNames(decl)
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if s.Tok != token.ASSIGN && s.Tok != token.ADD_ASSIGN &&
					s.Tok != token.SUB_ASSIGN && s.Tok != token.MUL_ASSIGN &&
					s.Tok != token.QUO_ASSIGN && s.Tok != token.REM_ASSIGN {
					return true
				}
				for _, lhs := range s.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || locals[id.Name] {
						continue
					}
					record(p, id.Name, pos(s), isTest)
				}
			case *ast.IncDecStmt:
				id, ok := s.X.(*ast.Ident)
				if !ok || locals[id.Name] {
					return true
				}
				record(p, id.Name, pos(s), isTest)
			}
			return true
		})
	}
}

func record(p *pkgVars, name, pos string, isTest bool) {
	if isTest {
		p.testWrites[name] = append(p.testWrites[name], pos)
		return
	}
	p.prodWrites[name] = append(p.prodWrites[name], pos)
}

// localNames 收集函数内声明的名字(参数、返回值、:= 定义、var 声明),
// 用于排除"给局部变量赋值"被误判成写包级变量。
func localNames(fn *ast.FuncDecl) map[string]bool {
	locals := map[string]bool{}
	add := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, f := range fields.List {
			for _, n := range f.Names {
				locals[n.Name] = true
			}
		}
	}
	add(fn.Recv)
	add(fn.Type.Params)
	add(fn.Type.Results)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					locals[id.Name] = true
				}
			}
		case *ast.GenDecl:
			if s.Tok != token.VAR && s.Tok != token.CONST {
				return true
			}
			for _, spec := range s.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range vs.Names {
					locals[id.Name] = true
				}
			}
		case *ast.TypeSwitchStmt:
			if s.Assign != nil {
				if id, ok := s.Assign.(*ast.AssignStmt); ok {
					for _, lhs := range id.Lhs {
						if ident, ok := lhs.(*ast.Ident); ok {
							locals[ident.Name] = true
						}
					}
				}
			}
		case *ast.RangeStmt:
			if id, ok := s.Key.(*ast.Ident); ok {
				locals[id.Name] = true
			}
			if id, ok := s.Value.(*ast.Ident); ok {
				locals[id.Name] = true
			}
		}
		return true
	})
	return locals
}

func readBaseline(path string) map[string]bool {
	known := map[string]bool{}
	if path == "" {
		return known
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return known
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		known[line] = true
	}
	return known
}
