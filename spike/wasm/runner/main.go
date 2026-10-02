//go:build wasmspike

// Command runner (SPIKE) runs one embedded checker on wazero over input files
// and reports the verdict and wall time: runner <checker> <file>...
// RUNNER_TIMEOUT (a Go duration) bounds the run; on expiry the module is
// closed and the run fails (exit 98), as the gate would fail closed.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/blackwell-systems/gsm/internal/wasmoracle"
)

func main() {
	ctx := context.Background()
	if d, err := time.ParseDuration(os.Getenv("RUNNER_TIMEOUT")); err == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	t0 := time.Now()
	if err := wasmoracle.Warm(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	compile := time.Since(t0)
	var files [][]byte
	for _, p := range os.Args[2:] {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		files = append(files, b)
	}
	t1 := time.Now()
	res, err := wasmoracle.Run(ctx, os.Args[1], files...)
	run := time.Since(t1)
	os.Stdout.Write(res.Stdout)
	os.Stderr.Write(res.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		if ctx.Err() != nil {
			os.Exit(98)
		}
		os.Exit(99)
	}
	fmt.Fprintf(os.Stderr, "wasm %s: compile %v, run %v, exit %d, linear memory %d MiB\n", os.Args[1], compile.Round(time.Millisecond), run.Round(time.Millisecond), res.Exit, res.MemBytes>>20)
	os.Exit(res.Exit)
}
