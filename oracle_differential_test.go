package gsm

// Differential test: every machine this test suite passes to Build, plus the
// random combinator machines in oracle_random_test.go, is cross-checked against
// both checkers extracted from the Coq proof (normalization-confluence
// coq/extraction). The cross-check runs after all tests, from TestMain, when
// GSM_CONVERGENCE_CHECKER and GSM_AST_CHECKER name the binaries; with
// GSM_REQUIRE_ORACLES=1 a missing binary fails the run.
//
// Every disagreement is classified. The known spec differences between Build and
// the checkers (Build checks only declared pairs, and checks from the zero state
// even when it is invalid; the table checker also checks invariant-invalid
// encodings; the rules checker requires repair to terminate only where an event
// can reach) are reported, not failed. Anything else fails the run.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	// Size caps keep the extracted checkers (list-based, quadratic lookups) fast.
	diffMaxTableStates = 2048
	diffMaxBoxStates   = 4096
)

type diffCase struct {
	name        string
	goOK        bool
	goErr       string
	wfcFail     bool
	ccFail      bool
	ccAtZero    bool // the CC counterexample is the zero state
	zeroValid   bool // the zero state satisfies every invariant
	allPairs    bool // Build checked every pair (no Independent/OnlyDeclaredPairs)
	boxStates   int
	ast         []byte // nil when not exportable
	astErr      string
	tables      []byte // nil when Build failed or the tables are too large
	tableStates int
	// For a table-checker rejection: do all ordered pairs commute on Build's CC
	// domain (nf-fixed states plus the zero state)? If so, the rejection comes
	// from invariant-invalid encodings, which Build does not check.
	allPairsCommuteOnDomain bool
	// Evidence for classifying a rules-oracle acceptance of a machine Build
	// rejected (computed only for exportable machines within the size cap):
	// ccFailsOnValid: a checked pair fails to commute on some valid, nonzero state
	// (then the rejection is not only about the zero state).
	// wfcReachableOK: repair terminates from every state an event reaches from a
	// valid state (then the WFC failure is only at states no event reaches).
	ccFailsOnValid bool
	wfcReachableOK bool
}

var (
	diffMu    sync.Mutex
	diffCases []*diffCase
	diffSeen  = map[[32]byte]bool{}
)

func init() { buildObserver = recordBuild }

func recordBuild(r *Registry, m *Machine, rep *Report, err error) {
	c := &diffCase{name: r.name, goOK: err == nil, allPairs: r.allIndependent}
	if err != nil {
		c.goErr = err.Error()
	}
	if rep != nil {
		c.boxStates = rep.StateCount
		c.wfcFail = err != nil && !rep.WFC && strings.Contains(c.goErr, "WFC")
		c.ccFail = err != nil && rep.WFC && rep.CCFailure != nil
		if rep.CCFailure != nil {
			c.ccAtZero = rep.CCFailure.State.packed == 0
		}
	}
	if len(r.vars) > 0 && r.totalBits <= 20 {
		c.zeroValid = r.allInvariantsHold(State{packed: 0, vars: r.vars})
	}
	var buf bytes.Buffer
	if e := r.WriteMachineAST(&buf); e == nil {
		c.ast = buf.Bytes()
	} else {
		c.astErr = e.Error()
	}
	if m != nil && !m.lazy {
		c.tables, c.tableStates = diffTables(m)
		c.allPairsCommuteOnDomain = diffAllPairsCommute(m)
	}
	if c.ast != nil && c.boxStates > 0 && c.boxStates <= diffMaxBoxStates {
		if c.ccFail {
			c.ccFailsOnValid = diffCCFailsOnValid(r)
		}
		if c.wfcFail {
			c.wfcReachableOK = diffReachableRepairTerminates(r)
		}
	}
	id := []byte(fmt.Sprintf("%v|%s|%v|", c.goOK, c.goErr, c.allPairs))
	id = append(id, c.ast...)
	id = append(id, 0)
	id = append(id, c.tables...)
	key := sha256.Sum256(id)
	diffMu.Lock()
	defer diffMu.Unlock()
	if !diffSeen[key] {
		diffSeen[key] = true
		diffCases = append(diffCases, c)
	}
}

// diffTables renders the machine's tables exactly as WriteConvergenceTables does,
// or returns nil when they exceed the size cap.
func diffTables(m *Machine) ([]byte, int) {
	n := 0
	for s := range m.valid {
		if m.valid[s] {
			n++
		}
	}
	if n > diffMaxTableStates {
		return nil, n
	}
	dir, err := os.MkdirTemp("", "gsm-diff-")
	if err != nil {
		return nil, n
	}
	defer removeDir(dir)
	p := filepath.Join(dir, "m.tables")
	if err = m.WriteConvergenceTables(p); err != nil {
		return nil, n
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, n
	}
	return b, n
}

func removeDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "differential: removing temp dir:", err)
	}
}

func diffAllPairsCommute(m *Machine) bool {
	for s := range m.nf {
		if !m.valid[s] || (m.nf[s] != uint64(s) && s != 0) {
			continue
		}
		for i := range m.step {
			for j := range m.step {
				if m.step[j][m.step[i][s]] != m.step[i][m.step[j][s]] {
					return false
				}
			}
		}
	}
	return true
}

// diffCCFailsOnValid rebuilds the step tables without the CC check and reports
// whether some checked pair fails to commute on a valid state other than zero.
func diffCCFailsOnValid(r *Registry) bool {
	m, _, err := r.build(false)
	if err != nil {
		return true // be conservative: no evidence that only the zero state fails
	}
	for _, p := range r.ccPairs() {
		i, j := p[0], p[1]
		for s := 1; s < len(m.nf); s++ {
			if !m.valid[s] || m.nf[s] != uint64(s) {
				continue
			}
			if m.step[j][m.step[i][s]] != m.step[i][m.step[j][s]] {
				return true
			}
		}
	}
	return false
}

// diffReachableRepairTerminates reports whether repair terminates from every state
// an event reaches from a valid state: the only states the rules oracle normalizes.
func diffReachableRepairTerminates(r *Registry) bool {
	limit := 1
	for _, v := range r.vars {
		limit *= v.domain
	}
	for p := uint64(0); p < 1<<r.totalBits; p++ {
		if !r.isValidEncoding(p) {
			continue
		}
		s := State{packed: p, vars: r.vars}
		if !r.allInvariantsHold(s) {
			continue
		}
		for _, ev := range r.events {
			t := r.clampState(r.applyEvent(ev, s))
			seen := map[uint64]bool{t.packed: true}
			for steps := 0; !r.allInvariantsHold(t); steps++ {
				t = r.applyFirstRepair(t)
				if seen[t.packed] || steps > limit {
					return false
				}
				seen[t.packed] = true
			}
		}
	}
	return true
}

type diffResult struct {
	c          *diffCase
	astExit    int // -1 when not run
	astOut     string
	tableExit  int // -1 when not run
	tableOut   string
	astClass   string
	tableClass string
}

func runChecker(bin string, content []byte, name string, dir string) (int, string, error) {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return -1, "", err
	}
	out, err := exec.Command(bin, p).CombinedOutput()
	if err == nil {
		return 0, string(out), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out), nil
	}
	return -1, string(out), err
}

// Classes. "agree" and the spec:/fragment classes are expected; anything starting
// with "BUG" fails the run.
func classifyAST(c *diffCase, exit int, out string) string {
	goOther := !c.goOK && !c.wfcFail && !c.ccFail
	switch {
	case goOther:
		return "skip: Build failed before WFC/CC (" + firstLine(c.goErr) + ")"
	case exit == 2:
		return "BUG: rules oracle rejected the exported input"
	case exit == 1 && strings.Contains(out, "outside the certified fragment"):
		return "fragment: rules oracle does not certify this machine's arithmetic"
	case c.goOK && exit == 0, !c.goOK && exit == 1:
		return "agree"
	case c.goOK && exit == 1 && !c.allPairs && !c.allPairsCommuteOnDomain:
		return "spec: declared pairs (Build checks only declared pairs; the rules oracle checks all)"
	case c.ccFail && exit == 0 && c.ccAtZero && !c.zeroValid && !c.ccFailsOnValid:
		return "spec: zero state (Build also checks from the invalid zero state; the rules oracle checks valid states only)"
	case c.wfcFail && exit == 0 && c.wfcReachableOK:
		return "spec: WFC domain (Build requires repair to terminate on every encoding; the rules oracle only where an event reaches)"
	default:
		return fmt.Sprintf("BUG: Build ok=%v (%s), rules oracle exit %d", c.goOK, firstLine(c.goErr), exit)
	}
}

func classifyTable(c *diffCase, exit int) string {
	switch {
	case exit == 0:
		return "agree"
	case exit == 2:
		return "BUG: table oracle rejected the emitted tables"
	case !c.allPairsCommuteOnDomain && !c.allPairs:
		return "spec: declared pairs (Build checks only declared pairs; the table oracle checks all)"
	case c.allPairsCommuteOnDomain:
		return "spec: invalid states (the tables include invariant-invalid encodings; Build checks valid states and the zero state)"
	default:
		return "BUG: Build accepted tables in which a checked pair does not commute"
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestMain(m *testing.M) {
	code := m.Run()
	if dcode := runDifferential(); dcode != 0 && code == 0 {
		code = dcode
	}
	os.Exit(code)
}

func runDifferential() int {
	tbin, abin := os.Getenv("GSM_CONVERGENCE_CHECKER"), os.Getenv("GSM_AST_CHECKER")
	if tbin == "" || abin == "" {
		if os.Getenv("GSM_REQUIRE_ORACLES") == "1" {
			fmt.Fprintln(os.Stderr, "differential: GSM_REQUIRE_ORACLES=1 but GSM_CONVERGENCE_CHECKER or GSM_AST_CHECKER is unset")
			return 1
		}
		return 0
	}
	dir, err := os.MkdirTemp("", "gsm-diff-run-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "differential:", err)
		return 1
	}
	defer removeDir(dir)

	diffMu.Lock()
	cases := append([]*diffCase(nil), diffCases...)
	diffMu.Unlock()

	results := make([]diffResult, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	var runErr error
	var errMu sync.Mutex
	for i, c := range cases {
		wg.Add(1)
		go func(i int, c *diffCase) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := diffResult{c: c, astExit: -1, tableExit: -1}
			if c.ast != nil && c.boxStates > 0 && c.boxStates <= diffMaxBoxStates {
				x, out, cerr := runChecker(abin, c.ast, fmt.Sprintf("%d.machine", i), dir)
				if cerr != nil {
					errMu.Lock()
					runErr = cerr
					errMu.Unlock()
				}
				r.astExit, r.astOut, r.astClass = x, out, classifyAST(c, x, out)
			}
			if c.tables != nil {
				x, out, cerr := runChecker(tbin, c.tables, fmt.Sprintf("%d.tables", i), dir)
				if cerr != nil {
					errMu.Lock()
					runErr = cerr
					errMu.Unlock()
				}
				r.tableExit, r.tableOut, r.tableClass = x, out, classifyTable(c, x)
			}
			results[i] = r
		}(i, c)
	}
	wg.Wait()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "differential: running a checker:", runErr)
		return 1
	}
	return reportDifferential(results)
}

func reportDifferential(results []diffResult) int {
	sort.SliceStable(results, func(i, j int) bool { return results[i].c.name < results[j].c.name })
	counts := map[string]int{}
	byClass := map[string][]string{}
	var bugLines []string
	astRun, tabRun, notExportable, tooBig := 0, 0, 0, 0
	for _, r := range results {
		if r.astExit >= 0 {
			astRun++
			counts["rules: "+r.astClass]++
		} else if r.c.ast == nil {
			notExportable++
		} else {
			tooBig++
		}
		if r.tableExit >= 0 {
			tabRun++
			counts["tables: "+r.tableClass]++
		}
		for _, x := range []struct{ which, class, out string }{
			{"rules", r.astClass, r.astOut}, {"tables", r.tableClass, r.tableOut},
		} {
			if x.class == "" || x.class == "agree" || strings.HasPrefix(x.class, "skip") {
				continue
			}
			key := x.which + ": " + x.class
			byClass[key] = append(byClass[key], r.c.name)
			if strings.HasPrefix(x.class, "BUG") {
				bugLines = append(bugLines, fmt.Sprintf("- %s | %s | Build ok=%v %s | %s", r.c.name, x.which, r.c.goOK, firstLine(r.c.goErr), x.class),
					"    oracle: "+strings.ReplaceAll(strings.TrimSpace(x.out), "\n", " / "))
				if x.which == "rules" {
					bugLines = append(bugLines, "    machine: "+strings.ReplaceAll(strings.TrimSpace(string(r.c.ast)), "\n", " "))
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "differential: %d distinct Build results; rules oracle ran on %d (%d not exportable, %d over the size cap); table oracle ran on %d\n",
		len(results), astRun, notExportable, tooBig, tabRun)
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %5d  %s\n", counts[k], k)
	}
	ckeys := make([]string, 0, len(byClass))
	for k := range byClass {
		ckeys = append(ckeys, k)
	}
	sort.Strings(ckeys)
	for _, k := range ckeys {
		names := byClass[k]
		shown := names
		if len(shown) > 12 {
			shown = shown[:12]
		}
		fmt.Fprintf(&b, "%s (%d): %s", k, len(names), strings.Join(shown, ", "))
		if len(names) > len(shown) {
			fmt.Fprintf(&b, ", ... %d more", len(names)-len(shown))
		}
		b.WriteString("\n")
	}
	if len(bugLines) > 0 {
		b.WriteString("unexplained disagreements:\n")
		b.WriteString(strings.Join(bugLines, "\n"))
		b.WriteString("\n")
	}
	fmt.Print(b.String())
	code := 0
	if p := os.Getenv("GSM_ORACLE_REPORT"); p != "" {
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "differential: writing report:", err)
			code = 1
		}
	}
	if p := os.Getenv("GSM_ORACLE_CASES"); p != "" {
		var all strings.Builder
		for _, r := range results {
			fmt.Fprintf(&all, "%s\trules=%d %s\ttables=%d %s\n", r.c.name, r.astExit, r.astClass, r.tableExit, r.tableClass)
		}
		if err := os.WriteFile(p, []byte(all.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "differential: writing cases:", err)
			code = 1
		}
	}
	if len(bugLines) > 0 {
		fmt.Fprintln(os.Stderr, "differential: FAIL: unexplained disagreement(s), see above")
		return 1
	}
	return code
}
