// Command gsmgate checks the machines that programs built with -tags gsmgate
// recorded (one subdirectory of -dumps per program, written with GSM_GATE_DIR)
// against the two extracted checkers and a catalog of every machine and its
// expected verdict. With -scan it also type-checks the repository at that root
// and fails if it makes a gsm machine outside the catalog's programs (see scan).
// It exits 1 when the gate fails. See internal/gate and .github/oracle/README.md.
//
// It is its own module (it needs golang.org/x/tools; gsm itself has no
// dependencies). From this directory:
//
//	go run . -dumps DIR -catalog FILE \
//	    -table-checker checker -rules-checker astchecker \
//	    [-rerun DIR2] [-scan ROOT] [-summary FILE]
//
// -rerun names the records of a second run of the same programs; the gate fails
// if a program made different machines on the two runs.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/blackwell-systems/gsm/internal/gate"
)

func main() {
	dumps := flag.String("dumps", "", "directory of per-program machine records")
	catalog := flag.String("catalog", "", "catalog file")
	table := flag.String("table-checker", "", "the extracted table checker (checker)")
	rules := flag.String("rules-checker", "", "the extracted rules checker (astchecker)")
	scanRoot := flag.String("scan", "", "repository root to scan for gsm machines outside the catalog")
	summary := flag.String("summary", "", "append the Markdown report to this file (GITHUB_STEP_SUMMARY)")
	rerun := flag.String("rerun", "", "records of a second run of the same programs, which must make the same machines")
	flag.Parse()
	if *dumps == "" || *catalog == "" || *table == "" || *rules == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*dumps, *catalog, *table, *rules, *scanRoot, *summary, *rerun); err != nil {
		fmt.Fprintln(os.Stderr, "gsmgate:", err)
		os.Exit(1)
	}
}

func run(dumps, catalog, table, rules, scanRoot, summary, rerun string) error {
	cat, err := gate.ParseCatalogFile(catalog)
	if err != nil {
		return err
	}
	rep, err := gate.Run(dumps, cat, gate.Checkers{Table: table, Rules: rules})
	if err != nil {
		return err
	}
	var scanProblems []string
	if scanRoot != "" {
		if scanProblems, err = scan(scanRoot, cat); err != nil {
			return err
		}
	}
	if rerun != "" {
		diffs, derr := gate.CompareRuns(dumps, rerun)
		if derr != nil {
			return derr
		}
		scanProblems = append(scanProblems, diffs...)
	}
	var b strings.Builder
	b.WriteString("### gsm machine gate\n\n")
	b.WriteString(rep.String())
	for _, p := range scanProblems {
		fmt.Fprintf(&b, "\nFAIL (coverage): %s\n", p)
	}
	fmt.Print(b.String())
	if summary != "" {
		f, ferr := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			return ferr
		}
		_, werr := f.WriteString(b.String())
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
	}
	if !rep.OK() || len(scanProblems) > 0 {
		return fmt.Errorf("FAIL: a machine is rejected, disagrees with Build, or is not in the catalog (see above)")
	}
	return nil
}
