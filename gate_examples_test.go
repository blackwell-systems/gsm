//go:build gsmgate

package gsm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm/internal/gate"
)

// TestExampleMachinesGate is the example-machine gate: it runs every program in
// the catalog (each Example function in a child run of this test binary, each
// README run block as its own program), all built with -tags gsmgate, so each
// records every machine it makes; then it checks those machines with both
// extracted checkers against the catalog (internal/gate). The oracle CI job runs
// it with GSM_REQUIRE_ORACLES=1:
//
//	go test -tags gsmgate -run TestExampleMachinesGate -count=1 .
//
// GSM_GATE_SUMMARY=<file> appends the report (GITHUB_STEP_SUMMARY).
func TestExampleMachinesGate(t *testing.T) {
	tbin := oracleBinary(t, envTableOracle)
	abin := oracleBinary(t, envASTOracle)
	cat, err := gate.ParseCatalogFile(gateCatalog)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	blocks := runBlocks(t)
	dumps := t.TempDir()
	// The @none programs run too: the gate fails if one makes a machine.
	progs := cat.Programs()
	for prog := range cat.None {
		progs = append(progs, prog)
	}
	for _, prog := range progs {
		dir := filepath.Join(dumps, filepath.FromSlash(prog))
		var cmd *exec.Cmd
		if b, ok := blocks[prog]; ok {
			cmd = gateSnippet(t, repo, b)
		} else {
			cmd = exec.Command(os.Args[0], "-test.run", "^"+prog+"$", "-test.count=1")
		}
		cmd.Env = gateEnv(dir)
		if out, cerr := cmd.CombinedOutput(); cerr != nil {
			t.Errorf("%s: %v\n%s", prog, cerr, out)
		}
	}
	rep, err := gate.Run(dumps, cat, gate.Checkers{Table: tbin, Rules: abin})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + rep.String())
	if p := os.Getenv("GSM_GATE_SUMMARY"); p != "" {
		f, ferr := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			t.Fatal(ferr)
		}
		_, werr := f.WriteString("### Example-machine gate\n\n" + rep.String())
		cerr := f.Close()
		if werr != nil || cerr != nil {
			t.Fatal(werr, cerr)
		}
	}
	if !rep.OK() {
		t.Error("the example-machine gate failed: see the report above")
	}
}

// gateSnippet returns the command that runs a README run block as its own
// program against this checkout, built with the gsmgate tag.
func gateSnippet(t *testing.T, repo string, b docBlock) *exec.Cmd {
	t.Helper()
	src, err := wrap(b)
	if err != nil {
		t.Fatalf("%s:%d: %v", b.file, b.line, err)
	}
	dir := t.TempDir()
	mod := "module docsnippet\n\ngo 1.22\n\nrequire github.com/blackwell-systems/gsm v0.0.0\n\nreplace github.com/blackwell-systems/gsm => " + repo + "\n"
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "-tags", "gsmgate", ".")
	cmd.Dir = dir
	return cmd
}

// gateEnv is the child environment: the gate directory set, and the oracle
// variables unset, so a child test binary does not run the differential itself.
func gateEnv(dir string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case envTableOracle, envASTOracle, envRequireOracles, "GSM_ORACLE_REPORT", "GSM_ORACLE_CASES", gate.EnvDir:
			continue
		}
		env = append(env, kv)
	}
	return append(env, gate.EnvDir+"="+dir, "GOFLAGS=-mod=mod", "GOWORK=off")
}
