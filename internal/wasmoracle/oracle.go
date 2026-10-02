//go:build wasmspike

// Package wasmoracle is a SPIKE (stage 2 of the proof-gate plan): it runs the
// checkers extracted from the Coq proof in-process, as wasm32-wasip1 modules
// (OCaml bytecode plus the OCaml 4.14.2 runtime built by wasi-sdk) on wazero.
// No cgo, no host JS. Built only with -tags wasmspike; the .wasm files are
// produced by spike/wasm/build.sh and are not committed.
package wasmoracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"testing/fstest"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

var (
	//go:embed checker.wasm
	checkerWasm []byte
	//go:embed astchecker.wasm
	astcheckerWasm []byte
	//go:embed checker_fast.wasm
	checkerFastWasm []byte
)

// Modules by name.
var modules = map[string][]byte{
	"checker":      checkerWasm,
	"astchecker":   astcheckerWasm,
	"checker_fast": checkerFastWasm,
}

// Digest returns the SHA-256 of an embedded module.
func Digest(name string) string {
	s := sha256.Sum256(modules[name])
	return hex.EncodeToString(s[:])
}

// Result is one checker run: the WASI exit code and the two output streams.
type Result struct {
	Exit           int
	Stdout, Stderr []byte
	// MemBytes is the size of the module's linear memory when it exited.
	MemBytes uint64
}

type engine struct {
	rt       wazero.Runtime
	compiled map[string]wazero.CompiledModule
}

var (
	once   sync.Once
	eng    *engine
	engErr error
)

func load(ctx context.Context) (*engine, error) {
	once.Do(func() {
		// 65536 pages is the whole 4 GiB wasm32 address space: the bound is the
		// module's own, not a host policy.
		cfg := wazero.NewRuntimeConfig().WithMemoryLimitPages(65536).WithCloseOnContextDone(true)
		rt := wazero.NewRuntimeWithConfig(context.Background(), cfg)
		if _, err := wasi_snapshot_preview1.Instantiate(context.Background(), rt); err != nil {
			engErr = err
			return
		}
		e := &engine{rt: rt, compiled: map[string]wazero.CompiledModule{}}
		for name, bin := range modules {
			cm, err := rt.CompileModule(context.Background(), bin)
			if err != nil {
				engErr = fmt.Errorf("wasmoracle: compiling %s: %w", name, err)
				return
			}
			e.compiled[name] = cm
		}
		eng = e
	})
	return eng, engErr
}

// Warm compiles every module (once per process) and returns.
func Warm(ctx context.Context) error {
	_, err := load(ctx)
	return err
}

// Run runs checker name with the given input files, which are mounted read-only
// at /in and passed as arguments in order, exactly as the native binary takes
// file paths. A fresh module instance (fresh memory) serves every call.
func Run(ctx context.Context, name string, files ...[]byte) (Result, error) {
	e, err := load(ctx)
	if err != nil {
		return Result{}, err
	}
	cm, ok := e.compiled[name]
	if !ok {
		return Result{}, fmt.Errorf("wasmoracle: no module %q", name)
	}
	mfs := fstest.MapFS{}
	args := []string{name}
	for i, f := range files {
		p := fmt.Sprintf("f%d", i)
		mfs[p] = &fstest.MapFile{Data: f, Mode: fs.FileMode(0o444)}
		args = append(args, "/in/"+p)
	}
	var stdout, stderr bytes.Buffer
	mc := wazero.NewModuleConfig().WithName("").WithArgs(args...).
		WithStdout(&stdout).WithStderr(&stderr).
		WithFSConfig(wazero.NewFSConfig().WithFSMount(mfs, "/in"))
	var mem uint64
	mc = mc.WithStartFunctions()
	mod, err := e.rt.InstantiateModule(ctx, cm, mc)
	if err == nil {
		_, err = mod.ExportedFunction("_start").Call(ctx)
		if m := mod.Memory(); m != nil {
			mem = uint64(m.Size())
		}
		_ = mod.Close(ctx)
	}
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), MemBytes: mem}
	if err == nil {
		return res, nil
	}
	var ee *sys.ExitError
	if errors.As(err, &ee) {
		res.Exit = int(ee.ExitCode())
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return res, nil
	}
	return res, err
}
