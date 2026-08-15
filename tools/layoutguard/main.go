package main

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/singlechecker"
)

var allowedTopLevelDirs = map[string]bool{
	"cmd":      true,
	"config":   true,
	"internal": true,
	"pkg":      true,
	"runtime":  true,
	"tools":    true,
}

var analyzer = &analysis.Analyzer{
	Name: "layoutguard",
	Doc:  "enforces Go file placement: no random Go packages at the project root",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Pos()).Filename
		checkFile(pass, file, filename)
	}

	return nil, nil
}

func checkFile(pass *analysis.Pass, file *ast.File, filename string) {
	clean := filepath.ToSlash(filepath.Clean(filename))
	if strings.HasSuffix(clean, "_test.go") {
		return
	}

	rel := toProjectRelative(clean)
	if rel == "" || strings.HasPrefix(rel, "../") {
		return
	}

	parts := strings.Split(rel, "/")
	if len(parts) == 1 {
		checkRootGoFile(pass, file, rel)
		return
	}

	if !allowedTopLevelDirs[parts[0]] {
		pass.Reportf(file.Package, "go file is outside allowed project layout: %s", rel)
	}
}

func checkRootGoFile(pass *analysis.Pass, file *ast.File, rel string) {
	if rel == "main.go" && file.Name.Name == "main" {
		return
	}

	pass.Reportf(file.Package, "go file must not be placed at project root: %s", rel)
}

func toProjectRelative(clean string) string {
	if !filepath.IsAbs(clean) {
		return strings.TrimPrefix(clean, "./")
	}

	workingDirectory, err := filepath.Abs(".")
	if err != nil {
		return ""
	}

	prefix := filepath.ToSlash(filepath.Clean(workingDirectory)) + "/"
	if !strings.HasPrefix(clean, prefix) {
		return ""
	}

	return strings.TrimPrefix(clean, prefix)
}

func main() {
	singlechecker.Main(analyzer)
}
