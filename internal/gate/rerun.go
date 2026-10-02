package gate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"
)

// CompareRuns compares the machines recorded by two runs of the same programs
// (dump directories a and b) and returns, per program, why they differ. A
// program whose machines depend on something one run does not fix (the clock,
// randomness, the host) makes different machines on two runs.
func CompareRuns(a, b string) ([]string, error) {
	ra, err := readRecords(a)
	if err != nil {
		return nil, err
	}
	rb, err := readRecords(b)
	if err != nil {
		return nil, err
	}
	progs := map[string]bool{}
	for p := range ra {
		progs[p] = true
	}
	for p := range rb {
		progs[p] = true
	}
	var out []string
	for p := range progs {
		fa, ferr := fingerprints(ra[p])
		if ferr != nil {
			return nil, ferr
		}
		fb, ferr := fingerprints(rb[p])
		if ferr != nil {
			return nil, ferr
		}
		if strings.Join(fa, "\n") != strings.Join(fb, "\n") {
			out = append(out, fmt.Sprintf("program %s made different machines on two runs (%d and %d records): its machines depend on something a run does not fix", p, len(fa), len(fb)))
		}
	}
	sort.Strings(out)
	return out, nil
}

// fingerprints returns one line per record: its kind, name, Build verdict and
// the hashes of its checker inputs, sorted.
func fingerprints(rs []Record) ([]string, error) {
	var out []string
	for _, r := range rs {
		line := fmt.Sprintf("%s|%s|%v|%v|%v", r.Kind, r.Name, r.BuildOK, r.WFCFail, r.CCFail)
		for _, ext := range []string{".rules", ".pairs", ".tables"} {
			h := "-"
			if p := r.input(ext); p != "" {
				b, err := os.ReadFile(p)
				if err != nil {
					return nil, err
				}
				h = fmt.Sprintf("%x", sha256.Sum256(b))
			}
			line += "|" + h
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}
