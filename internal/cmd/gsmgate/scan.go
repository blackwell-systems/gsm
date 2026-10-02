package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
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

// inputs are identifiers through which a program can read input: flags,
// arguments, the environment, standard input, files and the platform. Every
// object of a package mapped to nil counts. It is a list, not a proof (the
// README names what it misses), and an early warning: the gate's canonical runs
// (runCanonical) are what fix a program's input. The clock, randomness and the
// host name are not here: bide's core reads them for timestamps, keys and lease
// owners.
var inputs = map[string]map[string]bool{
	"flag":    nil,
	"os":      {"Args": true, "Getenv": true, "LookupEnv": true, "Environ": true, "ExpandEnv": true, "Stdin": true, "ReadFile": true, "Open": true, "OpenFile": true, "ReadDir": true, "DirFS": true, "Getwd": true},
	"syscall": {"Getenv": true, "Environ": true},
	"runtime": {"GOOS": true, "GOARCH": true},
}

// otherPlatforms are the platforms whose import graphs the scan adds to this
// one's, so that a main package that depends on gsm only on another platform is
// still a program the catalog must list.
var otherPlatforms = []string{"linux/amd64", "windows/amd64", "darwin/arm64", "freebsd/amd64", "openbsd/amd64", "netbsd/amd64", "solaris/amd64", "aix/ppc64", "plan9/amd64", "android/arm64", "ios/arm64", "js/wasm", "wasip1/wasm"}

// docExts are the files read as documentation.
var docExts = map[string]bool{".md": true, ".mdx": true, ".markdown": true, ".rst": true, ".adoc": true, ".txt": true, ".html": true, ".htm": true}

// pkgInfo is what the scan knows about one package of the repository.
type pkgInfo struct {
	path, dir, name string // import path, directory relative to root, package name
	imports         []string
	ignored         []string // .go files a build constraint excludes, relative to root
	module          string   // module directory, absolute
}

// scan returns why the catalog does not cover the repository at root. It loads
// every Go module under root (hidden, testdata, vendor and node_modules
// directories included; test code excluded) with its full import graph, and
// type-checks the modules whose packages the rules below need. It fails if:
//   - a main package depends on gsm, directly or through any module (one in a
//     hidden directory or outside the repository included), and is not a catalog
//     program;
//   - a package refers to a gsm function or method that makes a machine and is
//     not a catalog program, or a catalog program is not a main package;
//   - a catalog program, or a package of the repository it imports, reads an
//     input (see inputs), or has a .go file a build constraint excludes;
//   - a non-test Go file imports gsm but is in no loaded package;
//   - a directory is a symbolic link (the scan does not follow it);
//   - a document shows a gsm machine without an @doc line, or an @doc entry
//     shows none.
func scan(root string, cat *gate.Catalog) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	programs := map[string]bool{}
	for _, p := range cat.Programs() {
		programs[p] = true
	}
	var p []string
	var modules []string
	importers := map[string]bool{} // non-test .go files importing gsm, absolute
	goDirs := map[string]bool{}    // directories holding non-test .go files, absolute
	docs := map[string]bool{}
	rel := func(path string) string {
		r, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return path
		}
		return filepath.ToSlash(r)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		name := d.Name()
		if d.Type()&fs.ModeSymlink != 0 {
			if st, serr := os.Stat(path); serr == nil && st.IsDir() {
				p = append(p, fmt.Sprintf("%s is a symlinked directory, which the scan does not follow", rel(path)))
			}
			return nil
		}
		if d.IsDir() {
			if name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case name == "go.mod":
			modules = append(modules, filepath.Dir(path))
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			goDirs[filepath.Dir(path)] = true
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
				docs[rel(path)] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Each module's packages are named by directory, since "./..." leaves out
	// directories starting with "." or "_" and testdata.
	patterns := map[string][]string{}
	for dir := range goDirs {
		if m := moduleOf(filepath.Join(dir, "x.go"), modules); m != "" {
			r, rerr := filepath.Rel(m, dir)
			if rerr != nil {
				return nil, rerr
			}
			patterns[m] = append(patterns[m], "./"+filepath.ToSlash(r))
		}
	}

	// The import graph of every module, untyped (go list -deps): cheap.
	pkgs := map[string]*pkgInfo{} // repository packages by import path
	touches := map[string]bool{}  // import path -> depends on gsm (any package, any module)
	listed := map[string]bool{}   // files of loaded packages
	var graph []*packages.Package
	for _, mod := range modules {
		if len(patterns[mod]) == 0 {
			continue
		}
		ps, lerr := packages.Load(&packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps | packages.NeedModule,
			Dir:  mod, Env: append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod"),
		}, patterns[mod]...)
		if lerr != nil {
			return nil, fmt.Errorf("load %s: %w", rel(mod), lerr)
		}
		for _, pkg := range ps {
			for _, e := range pkg.Errors {
				// A directory whose files a build constraint all excludes has no
				// package here; a gsm import in one of them is reported below.
				if strings.Contains(e.Msg, "build constraints exclude all Go files") {
					continue
				}
				p = append(p, fmt.Sprintf("%s (module %s) does not load, so it cannot be scanned: %s", pkg.PkgPath, rel(mod), e))
			}
			graph = append(graph, pkg)
			for _, f := range pkg.GoFiles {
				listed[f] = true
			}
			if len(pkg.GoFiles) == 0 {
				continue
			}
			info := &pkgInfo{path: pkg.PkgPath, dir: rel(filepath.Dir(pkg.GoFiles[0])), name: pkg.Name, module: mod}
			for ip := range pkg.Imports {
				info.imports = append(info.imports, ip)
			}
			for _, f := range pkg.IgnoredFiles {
				if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
					info.ignored = append(info.ignored, rel(f))
				}
			}
			pkgs[pkg.PkgPath] = info
		}
	}
	// Which packages depend on gsm, on any platform: the union of the import
	// graphs of this platform and of otherPlatforms (for the modules with a main
	// package), plus any package one of whose excluded files (another platform's,
	// or a build tag's) imports gsm itself.
	edges := map[string]map[string]bool{}
	direct := map[string]bool{gsmPath: true}
	addGraph := func(ps []*packages.Package) error {
		var verr error
		packages.Visit(ps, nil, func(pkg *packages.Package) {
			if edges[pkg.PkgPath] == nil {
				edges[pkg.PkgPath] = map[string]bool{}
			}
			for ip := range pkg.Imports {
				edges[pkg.PkgPath][ip] = true
			}
			for _, f := range pkg.IgnoredFiles {
				if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
					continue
				}
				imp, ierr := importsGSM(f)
				if ierr != nil {
					verr = ierr
					return
				}
				if imp {
					direct[pkg.PkgPath] = true
				}
			}
		})
		return verr
	}
	if err = addGraph(graph); err != nil {
		return nil, err
	}
	mainMods := map[string]bool{}
	for _, info := range pkgs {
		if info.name == "main" {
			mainMods[info.module] = true
		}
	}
	for _, mod := range modules {
		if !mainMods[mod] {
			continue
		}
		for _, plat := range otherPlatforms {
			goos, goarch, _ := strings.Cut(plat, "/")
			// Best effort: a package that does not load on another platform keeps
			// its edges from this one.
			ps, lerr := packages.Load(&packages.Config{
				Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps,
				Dir:  mod,
				Env:  append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0"),
			}, patterns[mod]...)
			if lerr != nil {
				continue
			}
			if err = addGraph(ps); err != nil {
				return nil, err
			}
		}
	}
	var touch func(path string, seen map[string]bool) bool
	touch = func(path string, seen map[string]bool) bool {
		if v, ok := touches[path]; ok {
			return v
		}
		if seen[path] {
			return false
		}
		seen[path] = true
		v := direct[path]
		for dep := range edges[path] {
			if touch(dep, seen) {
				v = true
			}
		}
		touches[path] = v
		return v
	}
	for path := range edges {
		touch(path, map[string]bool{})
	}

	// Rule: every main package that depends on gsm is a catalog program.
	for _, info := range pkgs {
		if info.name == "main" && touches[info.path] && !programs[info.dir] {
			p = append(p, fmt.Sprintf("%s is a main package that depends on gsm but is not a catalog program", info.dir))
		}
	}

	// The repository packages each program reaches.
	reach := map[string][]string{} // program dir -> repository import paths, itself included
	byDir := map[string]*pkgInfo{}
	for _, info := range pkgs {
		byDir[info.dir] = info
	}
	for prog := range programs {
		start, ok := byDir[prog]
		if !ok {
			continue // a program that is not a loaded package: the gate itself fails on it
		}
		seen := map[string]bool{}
		var walk func(path string)
		walk = func(path string) {
			info, inRepo := pkgs[path]
			if !inRepo || seen[path] {
				return
			}
			seen[path] = true
			reach[prog] = append(reach[prog], path)
			for _, ip := range info.imports {
				walk(ip)
			}
		}
		walk(start.path)
		for _, path := range reach[prog] {
			if ign := pkgs[path].ignored; len(ign) > 0 {
				p = append(p, fmt.Sprintf("%s has files a build constraint excludes (%s), which can change program %s on another platform", pkgs[path].dir, strings.Join(ign, ", "), prog))
			}
		}
	}

	// Typed load of the packages that import gsm directly or that a program
	// reaches, each module on its own.
	typed := map[string]map[string]bool{} // module -> patterns
	addTyped := func(mod, dir string) {
		r, rerr := filepath.Rel(mod, dir)
		if rerr != nil {
			return
		}
		if typed[mod] == nil {
			typed[mod] = map[string]bool{}
		}
		typed[mod]["./"+filepath.ToSlash(r)] = true
	}
	for f := range importers {
		if m := moduleOf(f, modules); m != "" {
			addTyped(m, filepath.Dir(f))
		}
	}
	for _, paths := range reach {
		for _, path := range paths {
			addTyped(pkgs[path].module, filepath.Join(root, filepath.FromSlash(pkgs[path].dir)))
		}
	}
	reads := map[string][]string{} // import path -> inputs it reads
	makerDirs := map[string][]string{}
	for _, mod := range modules {
		if len(typed[mod]) == 0 {
			continue
		}
		var pats []string
		for pat := range typed[mod] {
			pats = append(pats, pat)
		}
		sort.Strings(pats)
		ps, lerr := packages.Load(&packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
				packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
			Dir: mod, Env: append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod"),
		}, pats...)
		if lerr != nil {
			return nil, fmt.Errorf("load %s: %w", rel(mod), lerr)
		}
		for _, pkg := range ps {
			for _, e := range pkg.Errors {
				p = append(p, fmt.Sprintf("%s does not type-check, so it cannot be scanned: %s", pkg.PkgPath, e))
			}
			if pkg.TypesInfo == nil || len(pkg.GoFiles) == 0 {
				continue
			}
			dir := rel(filepath.Dir(pkg.GoFiles[0]))
			for id, obj := range pkg.TypesInfo.Uses {
				switch {
				case isMaker(obj):
					makerDirs[dir] = append(makerDirs[dir], rel(pkg.Fset.Position(id.Pos()).Filename)+":"+strconv.Itoa(pkg.Fset.Position(id.Pos()).Line))
				case isInput(obj):
					reads[pkg.PkgPath] = append(reads[pkg.PkgPath], obj.Pkg().Name()+"."+obj.Name())
				}
			}
		}
	}
	for prog, paths := range reach {
		for _, path := range paths {
			r := reads[path]
			if len(r) == 0 {
				continue
			}
			what := "program " + prog + " reads "
			if pkgs[path].dir != prog {
				what = "program " + prog + " depends on " + path + ", which reads "
			}
			p = append(p, what+strings.Join(uniq(r), ", ")+": the gate runs it once with no arguments, so another run could make other machines")
		}
	}
	for f := range importers {
		if !listed[f] {
			p = append(p, fmt.Sprintf("%s imports gsm but is in no loaded package (a build constraint excludes it), so its machines would go unchecked", rel(f)))
		}
	}
	for dir, at := range makerDirs {
		switch {
		case !programs[dir]:
			p = append(p, fmt.Sprintf("%s makes gsm machines (%s) but is not a catalog program", dir, strings.Join(first(uniq(at), 3), ", ")))
		case byDir[dir] == nil || byDir[dir].name != "main":
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
	return uniq(p), nil
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
