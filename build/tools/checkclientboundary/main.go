// checkclientboundary 强制 client/ 作为 gserver 的外部黑盒调用者。
//
// 规则：client 目录下任何 .go 文件（含 _test.go 与构建约束排除的文件）
// 都不得 import gserver 或其任意子包；唯一放行 gserver/client/...。
//
// 使用 go/parser 而非 go list .Imports，因为后者会漏掉 TestImports、
// XTestImports 以及当前 build tag 未参与构建的文件。
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const clientModulePrefix = "gserver/client"

func main() {
	root := flag.String("root", "client", "client 目录根")
	flag.Parse()

	var violations []string
	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// 跳过生成的 pb：内容由 protoc 产出，不承载业务 import。
			if d.Name() == "pb" && path != *root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "gserver" ||
				(strings.HasPrefix(importPath, "gserver/") &&
					importPath != clientModulePrefix &&
					!strings.HasPrefix(importPath, clientModulePrefix+"/")) {
				violations = append(violations, fmt.Sprintf("%s imports %s", path, importPath))
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "client 黑盒边界被破坏：\n%s\n", strings.Join(violations, "\n"))
		os.Exit(1)
	}
	fmt.Println("client boundary OK: no gserver implementation imports")
}
