package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// A program that records all its machines and then fails on its second run (a
// panic after Build) fails the gate, with -scan set as in bide: a run failure is
// a problem the scan's result must not replace.
func TestRunFailureFailsTheGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /usr/bin/true as a checker")
	}
	marker := filepath.Join(t.TempDir(), "ran-once")
	root := fixture(t, map[string]string{
		"prog/main.go": `package main

import (
	"os"

	"github.com/blackwell-systems/gsm"
)

func main() {
	r := gsm.NewRegistry("m")
	r.Bool("b")
	if _, _, err := r.Build(); err != nil {
		panic(err)
	}
	if _, err := os.Stat(` + strconv.Quote(marker) + `); err == nil {
		panic("second run")
	}
	if err := os.WriteFile(` + strconv.Quote(marker) + `, nil, 0o644); err != nil {
		panic(err)
	}
}
`,
	})
	catalog := filepath.Join(t.TempDir(), "machines.txt")
	if err := os.WriteFile(catalog, []byte("prog m certified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	err := run(filepath.Join(tmp, "r1"), catalog, "/usr/bin/true", "/usr/bin/true", root, "",
		filepath.Join(tmp, "r2"), root, gsmDir(t))
	if err == nil {
		t.Fatal("the program panicked on its second run, but the gate passed")
	}
	if _, serr := os.Stat(marker); serr != nil {
		t.Fatalf("the program did not run: %v", serr)
	}
}
