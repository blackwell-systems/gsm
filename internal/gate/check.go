package gate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Checkers names the two extracted checker binaries. Exit codes: 0 verified, 1
// not verified, 2 input rejected.
type Checkers struct {
	Table string // checker: format-2 step tables
	Rules string // astchecker: rules plus the declared pairs
}

// Outcome is the gate's result for one recorded machine.
type Outcome struct {
	Program string
	Record  Record
	Verdict Verdict // the catalog's verdict, "" when unlisted
	Other   Verdict // when Verdict is "": the verdict listed for the machine as another kind
	// Checker exit codes, -1 when the checker did not run.
	RulesExit, TablesExit int
	RulesOut, TablesOut   string
	Problems              []string
}

// Report is the gate's result for a dump directory.
type Report struct {
	Outcomes []Outcome
	Problems []string // catalog-level: unlisted programs, listed machines never made
	Runs     int      // checker invocations (identical inputs run once)
	Elapsed  time.Duration
}

// OK reports whether the gate passed.
func (r *Report) OK() bool {
	if len(r.Problems) > 0 {
		return false
	}
	for _, o := range r.Outcomes {
		if len(o.Problems) > 0 {
			return false
		}
	}
	return true
}

func exitText(code int) string {
	switch code {
	case -1:
		return "not run"
	case 0:
		return "verified"
	case 1:
		return "rejected"
	case 2:
		return "input refused"
	}
	return fmt.Sprintf("exit %d", code)
}

// String renders the report as Markdown.
func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "| program | machine | kind | Build | rules checker | table checker | expected | result |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|\n")
	for _, o := range r.Outcomes {
		build := "-"
		if o.Record.Kind == KindBuild {
			build = "accepts"
			if !o.Record.BuildOK {
				build = "rejects"
				switch {
				case o.Record.WFCFail:
					build += " (WFC)"
				case o.Record.CCFail:
					build += " (CC)"
				}
			}
		}
		res := "ok"
		if len(o.Problems) > 0 {
			res = "FAIL: " + strings.Join(o.Problems, "; ")
		}
		exp := string(o.Verdict)
		switch {
		case exp == "" && o.Other != "":
			exp = "(listed " + string(o.Other) + ")"
		case exp == "":
			exp = "(unlisted)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", o.Program, o.Record.Name, o.Record.Kind, build,
			exitText(o.RulesExit), exitText(o.TablesExit), exp, res)
	}
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "\nFAIL: %s\n", p)
	}
	verdict := "PASS"
	if !r.OK() {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "\n%s: %d machine records, %d checker runs, %s\n", verdict, len(r.Outcomes), r.Runs, r.Elapsed.Round(time.Millisecond))
	return b.String()
}

type runKey struct {
	bin  string
	hash [32]byte
}

type runResult struct {
	exit int
	out  string
}

// Run checks every machine recorded under dumps against the catalog.
func Run(dumps string, cat *Catalog, ck Checkers) (*Report, error) {
	start := time.Now()
	recs, err := readRecords(dumps)
	if err != nil {
		return nil, err
	}
	rep := &Report{}
	byProgram := map[string][]Entry{}
	for _, e := range cat.Entries {
		byProgram[e.Program] = append(byProgram[e.Program], e)
	}
	matched := map[Entry]bool{}
	cache := map[runKey]runResult{}
	run := func(bin string, args ...string) (int, string, error) {
		var all []byte
		for _, a := range args {
			b, rerr := os.ReadFile(a)
			if rerr != nil {
				return 0, "", rerr
			}
			all = append(append(all, fmt.Sprintf("%d:", len(b))...), b...)
		}
		k := runKey{bin: bin, hash: sha256.Sum256(all)}
		if r, ok := cache[k]; ok {
			return r.exit, r.out, nil
		}
		code, out, rerr := execChecker(bin, args...)
		if rerr != nil {
			return 0, "", rerr
		}
		rep.Runs++
		cache[k] = runResult{code, out}
		return code, out, nil
	}

	progs := make([]string, 0, len(recs))
	for p := range recs {
		progs = append(progs, p)
	}
	sort.Strings(progs)
	for _, prog := range progs {
		entries, listed := byProgram[prog]
		if !listed {
			rep.Problems = append(rep.Problems, fmt.Sprintf("program %s made gsm machines but is not in the catalog", prog))
		}
		rs := recs[prog]
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].stem < rs[j].stem })
		for _, rec := range rs {
			o := Outcome{Program: prog, Record: rec, RulesExit: -1, TablesExit: -1}
			if rules := rec.input(".rules"); rules != "" {
				args := []string{rules}
				if pairs := rec.input(".pairs"); pairs != "" {
					args = append(args, pairs)
				}
				if o.RulesExit, o.RulesOut, err = run(ck.Rules, args...); err != nil {
					return nil, err
				}
			}
			if tables := rec.input(".tables"); tables != "" {
				if o.TablesExit, o.TablesOut, err = run(ck.Table, tables); err != nil {
					return nil, err
				}
			}
			if listed {
				o.Verdict = match(entries, rec, matched)
				for _, e := range entries {
					if o.Verdict == "" && e.Machine == rec.Name {
						o.Other = e.Verdict
					}
				}
			}
			o.Problems = judge(o, listed)
			rep.Outcomes = append(rep.Outcomes, o)
		}
	}
	for _, e := range cat.Entries {
		if !matched[e] {
			rep.Problems = append(rep.Problems, fmt.Sprintf("catalog line %d: %s did not make %s (%s)", e.Line, e.Program, e.Machine, e.Verdict))
		}
	}
	rep.Elapsed = time.Since(start)
	return rep, nil
}

// match returns the verdict of the catalog entry for rec's machine and kind, or
// "" when there is none.
func match(entries []Entry, rec Record, matched map[Entry]bool) Verdict {
	for _, e := range entries {
		if e.Machine != rec.Name {
			continue
		}
		if (rec.Kind == KindSynthesized) != (e.Verdict == Synthesized) {
			continue
		}
		if rec.Kind == KindBuild && (e.Verdict == Rejected || e.Verdict == NotBuilt) == rec.BuildOK {
			continue
		}
		matched[e] = true
		return e.Verdict
	}
	return ""
}

// judge returns why o fails the gate, or nothing when it passes.
func judge(o Outcome, listed bool) []string {
	var p []string
	rec := o.Record
	for _, c := range []struct {
		name string
		exit int
	}{{"rules", o.RulesExit}, {"table", o.TablesExit}} {
		if c.exit == 2 {
			p = append(p, "the "+c.name+" checker refused the exported input")
		}
	}
	switch {
	case rec.Kind == KindCompositional:
		return append(p, "a compositional machine has no global tables, so the checkers cannot certify it")
	case !listed:
		return p // the program is reported as unlisted
	case o.Verdict == "":
		what := "an accepted"
		switch {
		case rec.Kind == KindSynthesized:
			what = "a synthesized"
		case !rec.BuildOK:
			what = "a rejected"
		}
		if o.Other != "" {
			return append(p, fmt.Sprintf("listed %s, but it is %s machine (Build: %s)", o.Other, what, firstLine(rec.BuildErr)))
		}
		return append(p, fmt.Sprintf("%s machine %q is not in the catalog (Build: %s)", what, rec.Name, firstLine(rec.BuildErr)))
	}
	switch o.Verdict {
	case Certified:
		switch o.RulesExit {
		case -1:
			p = append(p, "listed certified, but its rules were not exported: "+rec.RulesErr)
		case 0:
		case 2: // reported above
		default:
			p = append(p, "the rules checker does not verify it ("+exitText(o.RulesExit)+"): "+firstLine(o.RulesOut))
		}
		p = append(p, tablesVerified(o)...)
	case CertifiedTables:
		if o.RulesExit != -1 {
			p = append(p, "listed certified-tables, but its rules export: list it certified")
		}
		p = append(p, tablesVerified(o)...)
	case Synthesized:
		p = append(p, tablesVerified(o)...)
	case NotBuilt:
		if rec.WFCFail || rec.CCFail {
			p = append(p, "listed not-built, but Build reached its convergence check and rejects it: list it rejected")
		}
	case Rejected:
		if !rec.WFCFail && !rec.CCFail {
			p = append(p, "Build failed before the convergence check, so no checker can confirm the rejection (list it not-built): "+firstLine(rec.BuildErr))
			break
		}
		if o.RulesExit == -1 && o.TablesExit == -1 {
			p = append(p, "no checker could run on it")
		}
		switch o.TablesExit {
		case -1, 1, 2:
		case 0:
			p = append(p, "Build rejects it but the table checker verifies it")
		default:
			p = append(p, "the table checker failed ("+exitText(o.TablesExit)+"): "+firstLine(o.TablesOut))
		}
		switch {
		case o.RulesExit == -1, o.RulesExit == 2:
		case o.RulesExit == 0:
			p = append(p, "Build rejects it but the rules checker verifies it")
		case o.RulesExit != 1:
			p = append(p, "the rules checker failed ("+exitText(o.RulesExit)+"): "+firstLine(o.RulesOut))
		case strings.Contains(o.RulesOut, "outside the certified fragment"):
			p = append(p, "the rules checker refuses it as outside its fragment, so it does not confirm the rejection")
		case strings.Contains(o.RulesOut, "(WFC)") != rec.WFCFail:
			p = append(p, "the rules checker rejects it for a different reason than Build: "+firstLine(o.RulesOut))
		}
	}
	return p
}

func tablesVerified(o Outcome) []string {
	switch o.TablesExit {
	case -1:
		return []string{"its tables were not exported: " + o.Record.TablesErr}
	case 0, 2: // 2 is reported by judge
		return nil
	}
	return []string{"the table checker does not verify it (" + exitText(o.TablesExit) + "): " + firstLine(o.TablesOut)}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// execChecker runs a checker and returns its exit code and combined output.
func execChecker(bin string, args ...string) (int, string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		return 0, string(out), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out), nil
	}
	return -1, string(out), fmt.Errorf("run %s: %w", bin, err)
}
