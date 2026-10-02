package gsm_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm/internal/gate"
)

// gateCatalog lists every machine the examples make, for the example-machine
// gate (gate_examples_test.go, .github/oracle/README.md).
const gateCatalog = ".github/oracle/machines.txt"

// gatePrograms returns the programs the gate has to cover: every Example function
// in this directory's test files, and every `gocheck: run` block of the checked
// docs as <file>#<k>.
func gatePrograms(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Example") {
				out = append(out, fn.Name.Name)
			}
		}
	}
	for p := range runBlocks(t) {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// runBlocks returns the `gocheck: run` blocks of the checked docs, keyed
// <file>#<k> for the k-th run block of the file.
func runBlocks(t *testing.T) map[string]docBlock {
	t.Helper()
	out := map[string]docBlock{}
	for _, file := range docFiles {
		k := 0
		for _, b := range extractBlocks(t, file) {
			if b.mode == "run" {
				k++
				out[fmt.Sprintf("%s#%d", file, k)] = b
			}
		}
	}
	return out
}

func TestGateCatalogListsEveryExample(t *testing.T) {
	cat, err := gate.ParseCatalogFile(gateCatalog)
	if err != nil {
		t.Fatal(err)
	}
	// A program that makes no machine is listed with @none.
	listed := map[string]bool{}
	for _, p := range cat.Programs() {
		listed[p] = true
	}
	for p := range cat.None {
		listed[p] = true
	}
	want := map[string]bool{}
	for _, p := range gatePrograms(t) {
		want[p] = true
		if !listed[p] {
			t.Errorf("%s is not in %s: list each machine it makes and its verdict (or list it @none if it makes none)", p, gateCatalog)
		}
	}
	for p := range listed {
		if !want[p] {
			t.Errorf("%s lists %s, which is no Example function or run block", gateCatalog, p)
		}
	}
	if !want["README.md#1"] {
		t.Errorf("the README flagship example (README.md#1) is not a run block")
	}
}
