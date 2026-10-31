package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const module = "github.com/colligo-go/colligo"

func load(t *testing.T) map[string]*packages.Package {
	t.Helper()
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps, Dir: "../.."}
	roots, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]*packages.Package{}
	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		if _, seen := all[p.PkgPath]; seen {
			return
		}
		all[p.PkgPath] = p
		for _, imp := range p.Imports {
			walk(imp)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return all
}

func reaches(all map[string]*packages.Package, from, targetPrefix string) []string {
	start, ok := all[from]
	if !ok {
		return nil
	}
	var hits []string
	seen := map[string]bool{}
	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		for path, imp := range p.Imports {
			if seen[path] {
				continue
			}
			seen[path] = true
			if strings.HasPrefix(path, targetPrefix) {
				hits = append(hits, path)
			}
			walk(imp)
		}
	}
	walk(start)
	return hits
}

func TestCoreDoesNotImportSources(t *testing.T) {
	all := load(t)
	for _, core := range []string{"model", "store", "downloader", "verifier", "facts", "format", "checksum"} {
		if hits := reaches(all, module+"/"+core, module+"/source"); len(hits) > 0 {
			t.Errorf("%s imports %v", core, hits)
		}
	}
}

func TestLightDoorDoesNotImportStore(t *testing.T) {
	all := load(t)
	for _, light := range []string{"cmd/colligo", "verifier", "facts", "format", "manifest", "diagnoses"} {
		if hits := reaches(all, module+"/"+light, module+"/store"); len(hits) > 0 {
			t.Errorf("%s imports %v", light, hits)
		}
	}
}

func TestTransportDoesNotImportStoreOrSources(t *testing.T) {
	all := load(t)
	for _, forbidden := range []string{module + "/store", module + "/source"} {
		if hits := reaches(all, module+"/transport", forbidden); len(hits) > 0 {
			t.Errorf("transport imports %v", hits)
		}
	}
}

func TestNoInitFunctions(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == ".git" || d.Name() == "docs") {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "init" {
				t.Errorf("%s declares init()", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
