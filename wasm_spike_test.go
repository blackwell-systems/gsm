//go:build wasmspike

package gsm

// SPIKE (stage 2 of the proof-gate plan): run the extracted checkers in-process
// as wasm on wazero (internal/wasmoracle) over every case the differential
// cross-check collects, and compare with the native checkers byte for byte.
// GSM_WASM_SPIKE=1 enables it. With GSM_CONVERGENCE_CHECKER / GSM_AST_CHECKER /
// GSM_FAST_CHECKER set, every wasm result must equal the native one (exit code,
// stdout and stderr); without them (other OSes in CI) the wasm results are only
// recorded. GSM_WASM_CASES=<file> writes one line per run (for comparing hosts),
// GSM_WASM_REPORT=<file> the summary.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/blackwell-systems/gsm/internal/wasmoracle"
)

func init() { wasmSpikeHook = runWasmSpike }

type spikeRun struct {
	name, which string
	exit        int
	out, errOut []byte
	wasm        time.Duration
	native      time.Duration
	nativeExit  int
	nativeOK    bool
}

func shortHash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

func runNative(bin, dir, base string, files ...[]byte) (int, []byte, []byte, time.Duration, error) {
	var args []string
	for i, f := range files {
		p := filepath.Join(dir, fmt.Sprintf("%s.%d", base, i))
		if err := os.WriteFile(p, f, 0o644); err != nil {
			return 0, nil, nil, 0, err
		}
		args = append(args, p)
	}
	var so, se bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = &so, &se
	t := time.Now()
	err := cmd.Run()
	d := time.Since(t)
	if err == nil {
		return 0, so.Bytes(), se.Bytes(), d, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), so.Bytes(), se.Bytes(), d, nil
	}
	return 0, nil, nil, d, err
}

func pct(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := int(p * float64(len(s)-1))
	return s[i]
}

func pctF(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(p*float64(len(s)-1))]
}

func runWasmSpike(cases []*diffCase) int {
	if os.Getenv("GSM_WASM_SPIKE") != "1" {
		return 0
	}
	ctx := context.Background()
	t0 := time.Now()
	if err := wasmoracle.Warm(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "wasm spike:", err)
		return 1
	}
	compile := time.Since(t0)
	nat := map[string]string{
		"checker":      os.Getenv("GSM_CONVERGENCE_CHECKER"),
		"astchecker":   os.Getenv("GSM_AST_CHECKER"),
		"checker_fast": os.Getenv("GSM_FAST_CHECKER"),
	}
	dir, err := os.MkdirTemp("", "gsm-wasm-spike-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "wasm spike:", err)
		return 1
	}
	defer removeDir(dir)

	sort.SliceStable(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	var runs []spikeRun
	var mismatches []string
	var buildTimes, tableWasm []time.Duration
	var ratios []float64
	for i, c := range cases {
		type job struct {
			which string
			files [][]byte
		}
		var jobs []job
		if c.tables != nil {
			jobs = append(jobs, job{"checker", [][]byte{c.tables}}, job{"checker_fast", [][]byte{c.tables}})
		}
		if c.ast != nil && c.boxStates > 0 && c.boxStates <= diffMaxBoxStates {
			f := [][]byte{c.ast}
			if c.pairs != nil {
				f = append(f, c.pairs)
			}
			jobs = append(jobs, job{"astchecker", f})
		}
		var wasmTable time.Duration
		for _, j := range jobs {
			t := time.Now()
			res, err := wasmoracle.Run(ctx, j.which, j.files...)
			d := time.Since(t)
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("%s | %s | wasm run error: %v", c.name, j.which, err))
				continue
			}
			r := spikeRun{name: c.name, which: j.which, exit: res.Exit, out: res.Stdout, errOut: res.Stderr, wasm: d}
			if j.which == "checker" {
				wasmTable = d
			}
			if bin := nat[j.which]; bin != "" {
				x, so, se, nd, nerr := runNative(bin, dir, fmt.Sprintf("%d-%s", i, j.which), j.files...)
				if nerr != nil {
					mismatches = append(mismatches, fmt.Sprintf("%s | %s | native run error: %v", c.name, j.which, nerr))
				} else {
					r.nativeOK, r.nativeExit, r.native = true, x, nd
					if x != res.Exit || !bytes.Equal(so, res.Stdout) || !bytes.Equal(se, res.Stderr) {
						mismatches = append(mismatches, fmt.Sprintf("%s | %s | native exit %d %q %q, wasm exit %d %q %q",
							c.name, j.which, x, firstLine(string(so)), firstLine(string(se)), res.Exit, firstLine(string(res.Stdout)), firstLine(string(res.Stderr))))
					}
				}
			}
			runs = append(runs, r)
		}
		// The gate's overhead is measured on the table oracle, against Build on
		// the same registry (best of 3, so warm-up noise does not inflate it).
		if wasmTable > 0 && c.goOK && c.reg != nil {
			best := time.Duration(1 << 62)
			for k := 0; k < 3; k++ {
				t := time.Now()
				_, _, _ = c.reg.build(true)
				if d := time.Since(t); d < best {
					best = d
				}
			}
			buildTimes = append(buildTimes, best)
			tableWasm = append(tableWasm, wasmTable)
			ratios = append(ratios, float64(wasmTable)/float64(best))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "wasm spike: %s/%s, %d CPUs; checker %s; astchecker %s; checker_fast %s\n",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(),
		wasmoracle.Digest("checker")[:16], wasmoracle.Digest("astchecker")[:16], wasmoracle.Digest("checker_fast")[:16])
	fmt.Fprintf(&b, "  module compile (once per process, all three): %v\n", compile.Round(time.Millisecond))
	byWhich := map[string][]time.Duration{}
	natWhich := map[string][]time.Duration{}
	compared := 0
	for _, r := range runs {
		byWhich[r.which] = append(byWhich[r.which], r.wasm)
		if r.nativeOK {
			compared++
			natWhich[r.which] = append(natWhich[r.which], r.native)
		}
	}
	fmt.Fprintf(&b, "  %d wasm runs over %d cases; %d compared with native byte for byte; %d mismatches\n",
		len(runs), len(cases), compared, len(mismatches))
	for _, w := range []string{"checker", "checker_fast", "astchecker"} {
		ds := byWhich[w]
		fmt.Fprintf(&b, "  %-12s wasm  n=%4d p50=%v p90=%v max=%v\n", w, len(ds),
			pct(ds, .5).Round(time.Microsecond), pct(ds, .9).Round(time.Microsecond), pct(ds, 1).Round(time.Microsecond))
		if nd := natWhich[w]; len(nd) > 0 {
			fmt.Fprintf(&b, "  %-12s exec  n=%4d p50=%v p90=%v max=%v (native binary, process spawn included)\n", w, len(nd),
				pct(nd, .5).Round(time.Microsecond), pct(nd, .9).Round(time.Microsecond), pct(nd, 1).Round(time.Microsecond))
		}
	}
	fmt.Fprintf(&b, "  Build (Go)   n=%4d p50=%v p90=%v max=%v\n", len(buildTimes),
		pct(buildTimes, .5).Round(time.Microsecond), pct(buildTimes, .9).Round(time.Microsecond), pct(buildTimes, 1).Round(time.Microsecond))
	fmt.Fprintf(&b, "  gate overhead (wasm checker time / Build time, same machine): p50=%.1fx p90=%.1fx max=%.1fx\n",
		pctF(ratios, .5), pctF(ratios, .9), pctF(ratios, 1))
	if len(mismatches) > 0 {
		b.WriteString("mismatches:\n  " + strings.Join(mismatches, "\n  ") + "\n")
	}
	fmt.Print(b.String())
	code := 0
	if p := os.Getenv("GSM_WASM_REPORT"); p != "" {
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "wasm spike:", err)
			code = 1
		}
	}
	if p := os.Getenv("GSM_WASM_CASES"); p != "" {
		var all strings.Builder
		for _, r := range runs {
			fmt.Fprintf(&all, "%s\t%s\t%d\t%s\t%s\n", r.name, r.which, r.exit, shortHash(r.out), shortHash(r.errOut))
		}
		if err := os.WriteFile(p, []byte(all.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "wasm spike:", err)
			code = 1
		}
	}
	if len(mismatches) > 0 {
		fmt.Fprintln(os.Stderr, "wasm spike: FAIL: wasm and native disagree, see above")
		return 1
	}
	return code
}
