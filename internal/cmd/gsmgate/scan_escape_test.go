package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm/internal/gate"
)

// writeTree writes files under dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func gsmDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func scanRoot(t *testing.T, root, catalog string) string {
	t.Helper()
	cat, err := gate.ParseCatalog(strings.NewReader(catalog))
	if err != nil {
		t.Fatal(err)
	}
	got, err := scan(root, cat)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(got, "\n")
}

// wrapper is a module that makes gsm machines for its callers.
func wrapper(gsm string) map[string]string {
	return map[string]string{
		"go.mod":  "module example.com/wrap\n\ngo 1.22\n\nrequire github.com/blackwell-systems/gsm v0.0.0\n\nreplace github.com/blackwell-systems/gsm => " + gsm + "\n",
		"wrap.go": "package wrap\n" + imp + "func Make() { gsm.NewRegistry(\"w\").Build() }\n",
	}
}

// A main package that makes machines only through a module the scan does not
// type-check (in a hidden directory, or outside the repository) is still a
// program the catalog must list: it depends on gsm.
func TestScanWrappedGSM(t *testing.T) {
	gsm := gsmDir(t)
	for _, where := range []string{"hidden", "outside"} {
		t.Run(where, func(t *testing.T) {
			root := t.TempDir()
			wrapDir := filepath.Join(root, ".wrap")
			if where == "outside" {
				wrapDir = t.TempDir()
			}
			writeTree(t, wrapDir, wrapper(gsm))
			writeTree(t, root, map[string]string{
				"go.mod":            "module example.com/fx\n\ngo 1.22\n\nrequire (\n\texample.com/wrap v0.0.0\n\tgithub.com/blackwell-systems/gsm v0.0.0\n)\n\nreplace example.com/wrap => " + wrapDir + "\n\nreplace github.com/blackwell-systems/gsm => " + gsm + "\n",
				"cmd/ships/main.go": "package main\nimport \"example.com/wrap\"\nfunc main() { wrap.Make() }\n",
			})
			got := scanRoot(t, root, "")
			if !strings.Contains(got, "cmd/ships is a main package that depends on gsm") {
				t.Fatalf("want cmd/ships flagged, got:\n%s", got)
			}
		})
	}
}

func TestScanSymlinkedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	root := fixture(t, map[string]string{"main.go": "package main\nfunc main() {}\n"})
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"main.go": "package main\n" + imp + "func main() { gsm.NewRegistry(\"s\") }\n"})
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if got := scanRoot(t, root, ""); !strings.Contains(got, "linked is a symlinked directory") {
		t.Fatalf("want the symlink flagged, got:\n%s", got)
	}
}

// A file a build constraint leaves out of a program (or of a package it
// imports) can change it on another platform.
func TestScanIgnoredFiles(t *testing.T) {
	root := fixture(t, map[string]string{
		"prog/main.go":          "package main\nimport \"example.com/fx/lib\"\n" + imp + "func main() { gsm.NewRegistry(lib.Name()) }\n",
		"prog/extra_windows.go": "package main\nfunc init() {}\n",
		"lib/lib.go":            "package lib\nfunc Name() string { return \"p\" }\n",
		"lib/lib_plan9.go":      "package lib\nfunc init() {}\n",
	})
	got := scanRoot(t, root, "prog p certified\n")
	for _, want := range []string{
		"prog has files a build constraint excludes (prog/extra_windows.go)",
		"lib has files a build constraint excludes (lib/lib_plan9.go)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// An input read by a package the program imports (in the repository) can
// choose its machines as well as one the program reads itself.
func TestScanTransitiveInputs(t *testing.T) {
	root := fixture(t, map[string]string{
		"prog/main.go":     "package main\nimport \"example.com/fx/config\"\n" + imp + "func main() { gsm.NewRegistry(config.Name()) }\n",
		"config/config.go": "package config\nimport \"os\"\nfunc Name() string { return os.Getenv(\"N\") }\n",
		"plat/main.go":     "package main\nimport \"runtime\"\n" + imp + "func main() { gsm.NewRegistry(runtime.GOOS) }\n",
		"file/main.go":     "package main\nimport \"os\"\n" + imp + "func main() { b, _ := os.ReadFile(\"n\"); gsm.NewRegistry(string(b)) }\n",
		"clean/main.go":    "package main\n" + imp + "func main() { gsm.NewRegistry(\"c\") }\n",
	})
	got := scanRoot(t, root, "prog p certified\nplat q certified\nfile f certified\nclean c certified\n")
	for _, want := range []string{
		"program prog depends on example.com/fx/config, which reads os.Getenv",
		"program plat reads runtime.GOOS",
		"program file reads os.ReadFile",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "clean") {
		t.Errorf("clean should pass:\n%s", got)
	}
}

// A dependency that imports gsm only in files another platform or a build tag
// selects still makes the main package that imports it depend on gsm: the main
// rule must hold on every platform, not only the one the scan runs on.
func TestScanPlatformOnlyGSM(t *testing.T) {
	gsm := gsmDir(t)
	cases := map[string]map[string]string{
		// a plan9-only file imports gsm itself
		"direct": {
			"dep.go":       "package osdep\nfunc F() {}\n",
			"dep_plan9.go": "package osdep\n" + imp + "func init() { gsm.NewRegistry(\"p\") }\n",
		},
		// a file under a custom build tag imports gsm itself
		"tag": {
			"dep.go":         "package osdep\nfunc F() {}\n",
			"dep_special.go": "//go:build special\n\npackage osdep\n" + imp + "func init() { gsm.NewRegistry(\"s\") }\n",
		},
		// a windows-only file imports a package that imports gsm
		"transitive": {
			"dep.go":         "package osdep\nfunc F() {}\n",
			"dep_windows.go": "package osdep\nimport \"example.com/osdep/inner\"\nfunc init() { inner.G() }\n",
			"inner/inner.go": "package inner\n" + imp + "func G() { gsm.NewRegistry(\"i\") }\n",
		},
	}
	for name, dep := range cases {
		t.Run(name, func(t *testing.T) {
			depDir := t.TempDir()
			dep["go.mod"] = "module example.com/osdep\n\ngo 1.22\n\nrequire github.com/blackwell-systems/gsm v0.0.0\n\nreplace github.com/blackwell-systems/gsm => " + gsm + "\n"
			writeTree(t, depDir, dep)
			root := t.TempDir()
			writeTree(t, root, map[string]string{
				"go.mod":            "module example.com/fx\n\ngo 1.22\n\nrequire (\n\texample.com/osdep v0.0.0\n\tgithub.com/blackwell-systems/gsm v0.0.0\n)\n\nreplace example.com/osdep => " + depDir + "\n\nreplace github.com/blackwell-systems/gsm => " + gsm + "\n",
				"cmd/ships/main.go": "package main\nimport \"example.com/osdep\"\nfunc main() { osdep.F() }\n",
			})
			if got := scanRoot(t, root, ""); !strings.Contains(got, "cmd/ships is a main package that depends on gsm") {
				t.Fatalf("want cmd/ships flagged, got:\n%s", got)
			}
		})
	}
}
