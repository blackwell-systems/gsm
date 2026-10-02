package gate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const gsmImport = "github.com/blackwell-systems/gsm"

// Scan walks a repository that uses gsm and returns why the catalog does not
// cover it: a directory whose non-test Go code creates a gsm registry or
// federation that is neither a catalog program nor exempt, a document that shows a
// gsm machine and is not listed with @doc, or a stale @doc or @exempt entry.
// Hidden directories, testdata and vendor are skipped. Test code is not scanned:
// test machines are not shipped.
func Scan(root string, cat *Catalog) ([]string, error) {
	programs := map[string]bool{}
	for _, p := range cat.Programs() {
		programs[p] = true
	}
	makers := map[string][]string{} // dir -> files that create machines
	docs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasSuffix(name, ".md"):
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if strings.Contains(string(b), "gsm.NewRegistry(") || strings.Contains(string(b), "gsm.NewFederation(") {
				docs[rel] = true
			}
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			makes, merr := makesMachine(path)
			if merr != nil {
				return merr
			}
			if makes {
				dir := filepath.ToSlash(filepath.Dir(rel))
				makers[dir] = append(makers[dir], rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var p []string
	for dir, files := range makers {
		if !programs[dir] && cat.Exempt[dir] == "" {
			p = append(p, fmt.Sprintf("%s creates gsm machines (%s) but is not a catalog program or @exempt", dir, strings.Join(files, ", ")))
		}
	}
	for dir := range cat.Exempt {
		if len(makers[dir]) == 0 {
			p = append(p, fmt.Sprintf("@exempt %s: no gsm machine is created there", dir))
		}
	}
	for doc := range docs {
		if _, ok := cat.Docs[doc]; !ok {
			p = append(p, fmt.Sprintf("%s shows a gsm machine but is not listed with @doc", doc))
		}
	}
	for doc := range cat.Docs {
		if !docs[doc] {
			p = append(p, fmt.Sprintf("@doc %s: the document shows no gsm machine", doc))
		}
	}
	sort.Strings(p)
	return p, nil
}

// makesMachine reports whether a Go file calls gsm.NewRegistry or
// gsm.NewFederation (under any import name, or dot-imported).
func makesMachine(path string) (bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	local := ""
	for _, im := range f.Imports {
		if p, uerr := strconv.Unquote(im.Path.Value); uerr == nil && p == gsmImport {
			local = "gsm"
			if im.Name != nil {
				local = im.Name.Name
			}
		}
	}
	if local == "" || local == "_" {
		return false, nil
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		var fn string
		switch x := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, isID := x.X.(*ast.Ident); isID && id.Name == local {
				fn = x.Sel.Name
			}
		case *ast.Ident:
			if local == "." {
				fn = x.Name
			}
		}
		found = fn == "NewRegistry" || fn == "NewFederation"
		return !found
	})
	return found, nil
}
