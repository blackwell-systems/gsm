package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// A program runs canonically: no arguments, an environment of only PATH, a
// fresh empty HOME and TMPDIR and the gate directory, empty standard input, and
// a fresh empty working directory. So every run of the gate is the same run.
func TestRunCanonical(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX environment")
	}
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.22\n",
		"main.go": `package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	env := os.Environ()
	sort.Strings(env)
	in, _ := io.ReadAll(os.Stdin)
	wd, _ := os.Getwd()
	ents, _ := os.ReadDir(wd)
	home, _ := os.ReadDir(os.Getenv("HOME"))
	tmp, _ := os.ReadDir(os.Getenv("TMPDIR"))
	out := fmt.Sprintf("args=%d\nstdin=%q\nwd=%d\nhome=%d\ntmp=%d\n", len(os.Args)-1, in, len(ents), len(home), len(tmp))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		out += "env " + k + "\n"
	}
	dir := os.Getenv("GSM_GATE_DIR")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "probe.txt"), []byte(out), 0o644)
}
`,
	})
	bin := filepath.Join(t.TempDir(), "probe")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = src
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	t.Setenv("GSM_PROBE_LEAK", "1") // must not reach the program
	gateDir := filepath.Join(t.TempDir(), "rec")
	if out, err := runCanonical(context.Background(), bin, gateDir); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(gateDir, "probe.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	var env []string
	for _, l := range got {
		if strings.HasPrefix(l, "env ") {
			env = append(env, strings.TrimPrefix(l, "env "))
		}
	}
	sort.Strings(env)
	want := "GSM_GATE_DIR HOME PATH TMPDIR"
	if strings.Join(env, " ") != want {
		t.Errorf("environment %v, want only %s", env, want)
	}
	for _, l := range []string{"args=0", `stdin=""`, "wd=0", "home=0", "tmp=0"} {
		if !strings.Contains(string(b), l+"\n") {
			t.Errorf("want %s in:\n%s", l, b)
		}
	}
}
