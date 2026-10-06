package gsm

// Differential test: every machine this test suite passes to Build, plus the
// random combinator machines in oracle_random_test.go, is cross-checked against
// both checkers extracted from the Coq proof (normalization-confluence
// coq/extraction). The cross-check runs after all tests, from TestMain, when
// GSM_CONVERGENCE_CHECKER and GSM_AST_CHECKER name the binaries; with
// GSM_REQUIRE_ORACLES=1 a missing binary fails the run.
//
// The checkers decide the property Build checks (normalization-confluence
// TableFast.check_fast, proven equal to TableCheck.check_tables, and
// AstChecker.checkBuild): repair terminates from
// every state, and every declared pair commutes on the valid states and the zero
// state. So the only expected disagreement is a rules-oracle refusal outside its
// certified fragment (arithmetic that could wrap, or a possibly negative write
// into a two-valued variable). Every other disagreement fails the run, in both
// directions: the table oracle also runs on the tables of every machine Build
// rejected for CC (rebuilt without the CC phase), and must reject them; and the
// rules oracle must reject for the same reason Build did (WFC or CC).

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
	// Size caps keep the differential run short; the checkers themselves handle
	// gsm's full 2^20 states.
	diffMaxTableStates = 2048
	diffMaxBoxStates   = 4096
)

type diffCase struct {
	name      string
	goOK      bool
	goErr     string
	wfcFail   bool
	ccFail    bool
	allPairs  bool // Build checked every pair (no Independent/OnlyDeclaredPairs)
	boxStates int
	ast       []byte // nil when not exportable
	astErr    string
	pairs     []byte // the declared pairs, in the rules oracle's pairs-file format
	// tables: the step tables Build returned, or for a CC failure the tables of
	// the same registry rebuilt without the CC phase (so the table oracle must
	// reject them). nil when Build failed otherwise or the tables are too large.
	tables      []byte
	tableStates int
	// tableClass is set when Build certified the machine but its tables could not be
	// exported for the table oracle (an entry outside the in-domain encodings). That
	// is a disagreement in itself, counted with the oracle results, not a skip.
	tableClass string
	// tableIOErr is set when the tables could not be written to or read from a temp
	// file. That is a failure of the test harness, not a disagreement, and fails the run.
	tableIOErr string
}

var (
	diffMu    sync.Mutex
	diffCases []*diffCase
	diffSeen  = map[[32]byte]bool{}
)

// init chains recordBuild after any observer already installed (the gsmgate
// build's gate.go), since the order of package init functions is not specified.
func init() {
	prev := buildObserver
	buildObserver = func(r *Registry, m *Machine, rep *Report, err error) {
		if prev != nil {
			prev(r, m, rep, err)
		}
		recordBuild(r, m, rep, err)
	}
}

func recordBuild(r *Registry, m *Machine, rep *Report, err error) {
	c := newDiffCase(r, m, rep, err)
	id := []byte(fmt.Sprintf("%v|%s|%v|%s|", c.goOK, c.goErr, c.allPairs, c.tableClass))
	id = append(id, c.ast...)
	id = append(id, 0)
	id = append(id, c.pairs...)
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

// newDiffCase records one Build result for the cross-check.
func newDiffCase(r *Registry, m *Machine, rep *Report, err error) *diffCase {
	c := &diffCase{name: r.name, goOK: err == nil, allPairs: r.allIndependent}
	if err != nil {
		c.goErr = err.Error()
	}
	if rep != nil {
		c.boxStates = rep.StateCount
		c.wfcFail = err != nil && !rep.WFC && strings.Contains(c.goErr, "WFC")
		c.ccFail = err != nil && rep.WFC && rep.CCFailure != nil
	}
	if r.abs != nil {
		// Built by abstraction: the rules oracle would enumerate the declared ranges, so
		// only the representative tables are compared (the table oracle must accept them
		// exactly when Build did, and reject them on a CC failure).
		c.astErr = "verified by abstraction"
		if c.goOK || c.ccFail {
			tb, e := r.representativeTables()
			switch {
			case e != nil:
				c.tableClass = "BUG: the representative tables cannot be computed (" + firstLine(e.Error()) + ")"
			case len(tb.NF) <= diffMaxTableStates:
				c.tables, c.tableStates = formatTables(tb), len(tb.NF)
			}
		}
		return c
	}
	var buf bytes.Buffer
	if e := r.WriteMachineAST(&buf); e == nil {
		c.ast = buf.Bytes()
	} else {
		c.astErr = e.Error()
	}
	var pb bytes.Buffer
	if e := r.WriteDeclaredPairs(&pb); e == nil {
		c.pairs = pb.Bytes()
	}
	var tm *Machine // the tables to compare
	switch {
	case m != nil && !m.lazy:
		tm = m
	case c.ccFail:
		if m2, _, e := r.build(false); e == nil {
			tm = m2
		}
	}
	if tm != nil {
		var refused, ioErr error
		c.tables, c.tableStates, refused, ioErr = diffTables(tm)
		switch {
		case refused != nil:
			c.tableClass = "BUG: the tables cannot be exported for the table oracle (" + firstLine(refused.Error()) + ")"
		case ioErr != nil:
			c.tableIOErr = ioErr.Error()
		}
	}
	return c
}

// diffTables renders the machine's tables exactly as WriteConvergenceTables does.
// Tables over the size cap return nil and no errors. A certified machine whose
// tables the exporter refuses (an entry outside the in-domain encodings) returns
// refused, so the case is reported as a disagreement instead of silently not
// compared; a temp-file failure returns ioErr, which fails the run.
func diffTables(m *Machine) (b []byte, n int, refused, ioErr error) {
	for s := range m.valid {
		if m.valid[s] {
			n++
		}
	}
	if n > diffMaxTableStates {
		return nil, n, nil, nil
	}
	if _, refused = m.convergenceTables(); refused != nil {
		return nil, n, refused, nil
	}
	dir, err := os.MkdirTemp("", "gsm-diff-")
	if err != nil {
		return nil, n, nil, err
	}
	defer removeDir(dir)
	p := filepath.Join(dir, "m.tables")
	if err = m.WriteConvergenceTables(p); err != nil {
		return nil, n, nil, err
	}
	if b, err = os.ReadFile(p); err != nil {
		return nil, n, nil, err
	}
	return b, n, nil, nil
}

func removeDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "differential: removing temp dir:", err)
	}
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

// runChecker writes content (and, when non-nil, a pairs file) and runs the checker.
func runChecker(bin string, content, pairs []byte, name string, dir string) (int, string, error) {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return -1, "", err
	}
	args := []string{p}
	if pairs != nil {
		pp := p + ".pairs"
		if err := os.WriteFile(pp, pairs, 0o644); err != nil {
			return -1, "", err
		}
		args = append(args, pp)
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		return 0, string(out), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out), nil
	}
	return -1, string(out), err
}

// Classes. "agree", "skip" and "fragment" are expected; anything starting with
// "BUG" fails the run.
func classifyAST(c *diffCase, exit int, out string) string {
	oracleWFC := strings.Contains(out, "(WFC)")
	switch {
	case !c.goOK && !c.wfcFail && !c.ccFail:
		return "skip: Build failed before WFC/CC (" + firstLine(c.goErr) + ")"
	case exit == 2:
		return "BUG: rules oracle rejected the exported input"
	case exit == 1 && strings.Contains(out, "outside the certified fragment"):
		return "fragment: rules oracle does not certify this machine's arithmetic"
	case c.goOK && exit == 0:
		return "agree"
	case c.wfcFail && exit == 1 && oracleWFC:
		return "agree"
	case c.ccFail && exit == 1 && !oracleWFC:
		return "agree"
	default:
		return fmt.Sprintf("BUG: Build ok=%v (%s), rules oracle exit %d", c.goOK, firstLine(c.goErr), exit)
	}
}

func classifyTable(c *diffCase, exit int) string {
	switch {
	case exit == 2:
		return "BUG: table oracle rejected the emitted tables"
	case c.goOK && exit == 0:
		return "agree"
	case c.ccFail && exit == 1:
		return "agree"
	case c.goOK:
		return "BUG: Build accepted tables the table oracle rejects"
	default:
		return "BUG: the table oracle accepts tables in which a pair Build checks does not commute"
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
	for _, c := range cases {
		if c.tableIOErr != "" {
			fmt.Fprintf(os.Stderr, "differential: %s: writing the tables for the table oracle: %s\n", c.name, c.tableIOErr)
			return 1
		}
	}

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
			r := diffResult{c: c, astExit: -1, tableExit: -1, tableClass: c.tableClass}
			if c.ast != nil && c.boxStates > 0 && c.boxStates <= diffMaxBoxStates {
				x, out, cerr := runChecker(abin, c.ast, c.pairs, fmt.Sprintf("%d.machine", i), dir)
				if cerr != nil {
					errMu.Lock()
					runErr = cerr
					errMu.Unlock()
				}
				r.astExit, r.astOut, r.astClass = x, out, classifyAST(c, x, out)
			}
			if c.tables != nil {
				x, out, cerr := runChecker(tbin, c.tables, nil, fmt.Sprintf("%d.tables", i), dir)
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
		}
		if r.tableClass != "" {
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
