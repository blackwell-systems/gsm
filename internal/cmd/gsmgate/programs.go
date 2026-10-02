package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/blackwell-systems/gsm/internal/gate"
)

// canonicalPath is the only PATH a program runs with.
const canonicalPath = "/usr/bin:/bin"

// runCanonical runs a built program once, canonically, so that it records its
// machines into gateDir: no arguments; an environment of only PATH
// (canonicalPath), HOME and TMPDIR (each a fresh empty directory) and the gate
// directory; standard input empty (the null device); and a fresh empty working
// directory. Every run of the gate is then the same run, and the gate certifies
// the machines a program makes when run that way.
func runCanonical(ctx context.Context, bin, gateDir string) ([]byte, error) {
	gateDir, err := filepath.Abs(gateDir)
	if err != nil {
		return nil, err
	}
	base, err := os.MkdirTemp("", "gsmgate-run-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(base) //nolint:errcheck // a temp directory
	dirs := map[string]string{}
	for _, d := range []string{"wd", "home", "tmp"} {
		dirs[d] = filepath.Join(base, d)
		if err = os.Mkdir(dirs[d], 0o700); err != nil {
			return nil, err
		}
	}
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = dirs["wd"]
	cmd.Env = []string{"PATH=" + canonicalPath, "HOME=" + dirs["home"], "TMPDIR=" + dirs["tmp"], gate.EnvDir + "=" + gateDir}
	cmd.Stdin = nil // the null device
	return cmd.CombinedOutput()
}

// runPrograms builds each catalog program under root with -tags gsmgate
// against the gsm checkout gsmDir (through a copy of its module's go.mod with a
// replace, so the repository is not changed) and runs it canonically twice,
// recording into run1/<program> and run2/<program>. It returns the programs that
// failed to build or run, each with its output.
func runPrograms(root, gsmDir string, cat *gate.Catalog, run1, run2 string) ([]string, error) {
	var failed []string
	bins, err := os.MkdirTemp("", "gsmgate-bin-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(bins) //nolint:errcheck // a temp directory
	for i, prog := range cat.Programs() {
		dir := filepath.Join(root, filepath.FromSlash(prog))
		bin := filepath.Join(bins, fmt.Sprintf("p%d", i))
		if out, berr := buildProgram(dir, gsmDir, bin); berr != nil {
			failed = append(failed, fmt.Sprintf("%s does not build: %v\n%s", prog, berr, out))
			continue
		}
		for _, rd := range []string{run1, run2} {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			start := time.Now()
			out, rerr := runCanonical(ctx, bin, filepath.Join(rd, filepath.FromSlash(prog)))
			cancel()
			fmt.Printf("%s: %s\n", prog, time.Since(start).Round(time.Millisecond))
			if rerr != nil {
				failed = append(failed, fmt.Sprintf("%s failed: %v\n%s", prog, rerr, out))
				break
			}
		}
	}
	return failed, nil
}

// buildProgram builds the main package in dir with -tags gsmgate, its module's
// gsm replaced by gsmDir in a copy of the module's go.mod.
func buildProgram(dir, gsmDir, bin string) ([]byte, error) {
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	gomod, err := goOutput(dir, env, "env", "GOMOD")
	if err != nil {
		return nil, err
	}
	gomod = strings.TrimSpace(gomod)
	if gomod == "" || gomod == os.DevNull {
		return nil, fmt.Errorf("%s is in no module", dir)
	}
	tmp, err := os.MkdirTemp("", "gsmgate-mod-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // a temp directory
	modfile := filepath.Join(tmp, "go.mod")
	for _, f := range []string{"go.mod", "go.sum"} {
		b, rerr := os.ReadFile(filepath.Join(filepath.Dir(gomod), f))
		if rerr != nil && f == "go.mod" {
			return nil, rerr
		}
		if rerr == nil {
			if err = os.WriteFile(filepath.Join(tmp, f), b, 0o644); err != nil {
				return nil, err
			}
		}
	}
	gsmAbs, err := filepath.Abs(gsmDir)
	if err != nil {
		return nil, err
	}
	if _, err = goOutput(dir, env, "mod", "edit", "-modfile="+modfile, "-replace", gsmPath+"="+gsmAbs); err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "build", "-modfile="+modfile, "-tags", "gsmgate", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = env
	return cmd.CombinedOutput()
}

func goOutput(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}
