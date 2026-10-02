package gate

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// Verdict is the result a catalog entry expects for one machine.
type Verdict string

const (
	// Certified: Build accepts the machine and both checkers verify it.
	Certified Verdict = "certified"
	// CertifiedTables: Build accepts the machine and the table checker verifies
	// it; its rules are closures, so they are not exported and the rules checker
	// does not run. A machine whose rules export is listed Certified instead.
	CertifiedTables Verdict = "certified-tables"
	// Rejected: Build rejects the machine for WFC or CC, and every checker that
	// can run on it rejects it for the same reason.
	Rejected Verdict = "rejected"
	// Synthesized: a machine with a synthesized repair (BuildOrSynthesize), which
	// the table checker verifies. Its repair is a table, not rules.
	Synthesized Verdict = "synthesized"
	// NotBuilt: Build rejects the registry as written and neither its rules nor
	// its tables can be exported (an invariant left without a repair for
	// BuildOrSynthesize to synthesize, say), so no checker can run on it. The
	// machine the program then uses is listed separately.
	NotBuilt Verdict = "not-built"
)

// Entry lists one machine of one program.
type Entry struct {
	Program string
	Machine string
	Verdict Verdict
	Line    int
}

// Catalog lists every machine the gate checks, plus, for Scan, the documents
// that show gsm machines and the directories exempt from it. Its text format, one
// item per line, with a word starting with # starting a comment:
//
//	<program> <machine> <verdict>
//	@doc <path> <note>
//	@exempt <dir> <reason>
//
// A program is the directory its records are written to under the gate's dump
// directory (a Go package directory, an Example function name, or a document's
// run block such as README.md#1). A machine is the registry's name.
type Catalog struct {
	Entries []Entry
	Docs    map[string]string // path -> note
	Exempt  map[string]string // dir -> reason
}

// ParseCatalogFile reads a catalog file.
func ParseCatalogFile(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := ParseCatalog(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// ParseCatalog reads a catalog.
func ParseCatalog(r io.Reader) (*Catalog, error) {
	c := &Catalog{Docs: map[string]string{}, Exempt: map[string]string{}}
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		f := strings.Fields(line)
		for i, w := range f {
			if strings.HasPrefix(w, "#") { // a comment runs to the end of the line
				f = f[:i]
				break
			}
		}
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "@doc", "@exempt":
			if len(f) < 3 {
				return nil, fmt.Errorf("line %d: %s needs a path and a note", n, f[0])
			}
			m := c.Docs
			if f[0] == "@exempt" {
				m = c.Exempt
			}
			if _, dup := m[f[1]]; dup {
				return nil, fmt.Errorf("line %d: %s %s listed twice", n, f[0], f[1])
			}
			m[f[1]] = strings.Join(f[2:], " ")
			continue
		}
		if len(f) != 3 {
			return nil, fmt.Errorf("line %d: want <program> <machine> <verdict>, got %q", n, strings.TrimSpace(line))
		}
		v := Verdict(f[2])
		switch v {
		case Certified, CertifiedTables, Rejected, Synthesized, NotBuilt:
		default:
			return nil, fmt.Errorf("line %d: unknown verdict %q", n, f[2])
		}
		key := f[0] + " " + f[1] + " " + f[2]
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("line %d: duplicates line %d", n, prev)
		}
		seen[key] = n
		c.Entries = append(c.Entries, Entry{Program: f[0], Machine: f[1], Verdict: v, Line: n})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return c, nil
}

// Programs returns the catalog's programs, in order of first appearance.
func (c *Catalog) Programs() []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range c.Entries {
		if !seen[e.Program] {
			seen[e.Program] = true
			out = append(out, e.Program)
		}
	}
	return out
}
