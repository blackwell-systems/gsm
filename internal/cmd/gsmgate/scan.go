package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/blackwell-systems/gsm/internal/gate"
)

const gsmPath = "github.com/blackwell-systems/gsm"

// makers are gsm's functions and methods that create or build a machine. Any
// reference to one (a call, a function or method value) in a package's non-test
// code makes the package a machine maker.
var makers = map[string]map[string]bool{
	"":           {"NewRegistry": true, "NewFederation": true},
	"Registry":   {"Build": true, "BuildOrSynthesize": true, "BuildCompositional": true, "Synthesize": true, "SynthesizeWith": true},
	"Federation": {"Build": true, "BuildCoordinated": true, "Certify": true},
	"Synthesis":  {"Machine": true},
}

// inputs are what lets a run of a program choose other machines than the gate's
// one run with no arguments: flags, the arguments and the environment.
var inputs = map[string]map[string]bool{
	"flag": nil, // every object of package flag
	"os":   {"Args": true, "Getenv": true, "LookupEnv": true, "Environ": true, "ExpandEnv": true},
}

// docExts are the files read as documentation.
var docExts = map[string]bool{".md": true, ".mdx": true, ".markdown": true, ".rst": true, ".adoc": true, ".txt": true, ".html": true, ".htm": true}

// scan type-checks every Go module under root that has a file importing gsm
// (each module on its own, test code excluded) and returns why the catalog does not cover the repository:
//   - a package refers to a gsm function or method that makes a machine and is
//     not a catalog program (a main package the gate runs);
//   - a non-test Go file imports gsm but is in no type-checked package (a build
//     tag or GOOS excludes it), so it could make machines unseen;
//   - a catalog program reads flags, its arguments or the environment, so a run
//     other than the gate's could make other machines;
//   - a document shows a gsm machine and is not listed with @doc, or an @doc
//     entry shows none.
//
// Hidden directories, testdata, vendor and node_modules are skipped.
func scan(root string, cat *gate.Catalog) ([]string, error) {
	programs := map[string]bool{}
	for _, p := range cat.Programs() {
		programs[p] = true
	}
	var modules []string
	importers := map[string]bool{} // non-test .go files importing gsm, by absolute path
	docs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		switch {
		case name == "go.mod":
			modules = append(modules, filepath.Dir(path))
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			imp, ierr := importsGSM(path)
			if ierr != nil {
				return ierr
			}
			if imp {
				importers[path] = true
			}
		case docExts[strings.ToLower(filepath.Ext(name))]:
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if docShowsMachine(string(b)) {
				docs[rel] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var p []string
	makerDirs := map[string][]string{} // dir -> positions of the references
	mainPkg := map[string]bool{}
	checked := map[string]bool{}
	// Only a package that imports gsm can refer to its functions, so only the
	// modules holding such a file are type-checked.
	need := map[string]bool{}
	for f := range importers {
		if m := moduleOf(f, modules); m != "" {
			need[m] = true
		}
	}
	for _, mod := range modules {
		if !need[mod] {
			continue
		}
		cfg := &packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
				packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
			Dir:   mod,
			Env:   append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod"),
			Tests: false,
		}
		pkgs, lerr := packages.Load(cfg, "./...")
		if lerr != nil {
			return nil, fmt.Errorf("load %s: %w", mod, lerr)
		}
		for _, pkg := range pkgs {
			for _, e := range pkg.Errors {
				p = append(p, fmt.Sprintf("%s does not type-check, so it cannot be scanned: %s", pkg.PkgPath, e))
			}
			for _, f := range pkg.GoFiles {
				checked[f] = true
			}
			if pkg.TypesInfo == nil || len(pkg.GoFiles) == 0 {
				continue
			}
			rel, rerr := filepath.Rel(root, filepath.Dir(pkg.GoFiles[0]))
			if rerr != nil {
				return nil, rerr
			}
			dir := filepath.ToSlash(rel)
			if pkg.Name == "main" {
				mainPkg[dir] = true
			}
			var reads []string
			for id, obj := range pkg.TypesInfo.Uses {
				switch {
				case isMaker(obj):
					makerDirs[dir] = append(makerDirs[dir], pkg.Fset.Position(id.Pos()).String())
				case programs[dir] && isInput(obj):
					reads = append(reads, obj.Pkg().Name()+"."+obj.Name())
				}
			}
			if len(reads) > 0 {
				p = append(p, fmt.Sprintf("program %s reads %s: the gate runs it once with no arguments, so a run that sets them could make other machines", dir, strings.Join(uniq(reads), ", ")))
			}
		}
	}
	for f := range importers {
		if !checked[f] {
			p = append(p, fmt.Sprintf("%s imports gsm but is in no type-checked package (a build constraint excludes it), so its machines would go unchecked", f))
		}
	}
	for dir, at := range makerDirs {
		switch {
		case !programs[dir]:
			p = append(p, fmt.Sprintf("%s makes gsm machines (%s) but is not a catalog program", dir, strings.Join(first(uniq(at), 3), ", ")))
		case !mainPkg[dir]:
			p = append(p, fmt.Sprintf("%s is a catalog program but not a main package, so the gate cannot run it", dir))
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

// moduleOf returns the innermost module directory holding file, or "".
func moduleOf(file string, modules []string) string {
	best := ""
	for _, m := range modules {
		if strings.HasPrefix(file, m+string(filepath.Separator)) && len(m) > len(best) {
			best = m
		}
	}
	return best
}

// isMaker reports whether obj is one of gsm's machine-making functions or methods.
func isMaker(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != gsmPath {
		return false
	}
	recv := ""
	if r := fn.Type().(*types.Signature).Recv(); r != nil {
		t := r.Type()
		if ptr, isPtr := t.(*types.Pointer); isPtr {
			t = ptr.Elem()
		}
		if named, isNamed := t.(*types.Named); isNamed {
			recv = named.Obj().Name()
		}
	}
	return makers[recv][fn.Name()]
}

func isInput(obj types.Object) bool {
	if obj.Pkg() == nil {
		return false
	}
	names, ok := inputs[obj.Pkg().Path()]
	return ok && (names == nil || names[obj.Name()])
}

// importsGSM reports whether a Go file imports gsm, whatever its build
// constraints.
func importsGSM(path string) (bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, im := range f.Imports {
		if p, uerr := strconv.Unquote(im.Path.Value); uerr == nil && p == gsmPath {
			return true, nil
		}
	}
	return false, nil
}

var (
	docImportRE = regexp.MustCompile(`(\.|[A-Za-z_][A-Za-z0-9_]*)?[ \t]*"github\.com/blackwell-systems/gsm"`)
	docDotRE    = regexp.MustCompile(`(^|[^.\w])New(Registry|Federation)\(`)
)

// docShowsMachine reports whether a document shows gsm making a machine: a call
// of NewRegistry or NewFederation qualified by gsm or by a name the document
// imports gsm as, or unqualified when it dot-imports gsm.
func docShowsMachine(text string) bool {
	quals := map[string]bool{"gsm": true}
	dot := false
	for _, m := range docImportRE.FindAllStringSubmatch(text, -1) {
		switch m[1] {
		case "", "import":
		case ".":
			dot = true
		default:
			quals[m[1]] = true
		}
	}
	for q := range quals {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(q) + `\s*\.\s*New(Registry|Federation)\(`).MatchString(text) {
			return true
		}
	}
	return dot && docDotRE.MatchString(text)
}

func uniq(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

func first(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("%d more", len(s)-n))
	}
	return s
}
