// Package noleak implements the go/analysis check described in
// CONFIDENTIALITY.md §3.1: it closes the two side doors the type system
// alone misses. It bans calling the real-value accessor of secret.* outside
// internal/fsaccess and transport/, and bans a string field or parameter
// named like a path, URL or credential in core packages.
package noleak

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "noleak",
	Doc:      "bans raw string where the domain requires secret.*, and real-value access outside the authorized packages",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

var suspectName = regexp.MustCompile(`(?i)(path|url|uri|file|name|token|secret|key)`)

func allowedToReveal(pkgPath string) bool {
	return strings.HasSuffix(pkgPath, "/fsaccess") ||
		pkgPath == "fsaccess" ||
		strings.Contains(pkgPath, "/transport") ||
		strings.HasSuffix(pkgPath, "transport")
}

func exemptFromStringCheck(pkgPath string) bool {
	return strings.Contains(pkgPath, "/source/") ||
		strings.HasSuffix(pkgPath, "/transport") ||
		strings.HasSuffix(pkgPath, "transport") ||
		strings.HasSuffix(pkgPath, "/fsaccess") ||
		strings.Contains(pkgPath, "/cmd/") ||
		strings.HasSuffix(pkgPath, "/cmd") ||
		strings.HasSuffix(pkgPath, "/noleak")
}

func isTestFile(pass *analysis.Pass, pos ast.Node) bool {
	return strings.HasSuffix(pass.Fset.Position(pos.Pos()).Filename, "_test.go")
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	pkgPath := pass.Pkg.Path()

	if !allowedToReveal(pkgPath) {
		insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
			call := n.(*ast.CallExpr)
			if isTestFile(pass, call) {
				return
			}
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				if fn.Sel.Name == "Raw" || fn.Sel.Name == "Reveal" {
					pass.Reportf(call.Pos(), "call to %s.%s outside internal/fsaccess or transport/: a secret.* real value can't leave the authorized package", receiverName(fn.X), fn.Sel.Name)
				}
			case *ast.Ident:
				if fn.Name == "Reveal" {
					pass.Reportf(call.Pos(), "call to %s outside internal/fsaccess or transport/: a secret.* real value can't leave the authorized package", fn.Name)
				}
			}
		})
	}

	if !exemptFromStringCheck(pkgPath) {
		insp.Preorder([]ast.Node{(*ast.Field)(nil)}, func(n ast.Node) {
			field := n.(*ast.Field)
			if len(field.Names) == 0 || isTestFile(pass, field) {
				return
			}
			t := pass.TypesInfo.TypeOf(field.Type)
			if t == nil || !types.Identical(t, types.Typ[types.String]) {
				return
			}
			for _, name := range field.Names {
				if suspectName.MatchString(name.Name) {
					pass.Reportf(name.Pos(), "field/parameter %q is a raw string where the domain requires secret.Name, secret.Endpoint or secret.Secret", name.Name)
				}
			}
		})
	}

	return nil, nil
}

func receiverName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "value"
}
