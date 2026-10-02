// Package gate is the example-machine gate: every machine a listed program makes
// is checked by the two checkers extracted from the Coq proof
// (normalization-confluence coq/extraction), and the run fails when a checker
// rejects a machine, disagrees with Build, or a machine is not listed.
//
// A program built with -tags gsmgate and run with GSM_GATE_DIR set records each
// machine it makes (gsm's gate.go calls WriteRecord): every Build result, every
// synthesized machine and every compositional machine. Run reads those records,
// runs the checkers on them and compares the verdicts with a catalog that names
// every machine and its expected verdict. Scan checks that a repository builds
// no gsm machine outside the catalog's programs.
package gate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// EnvDir names the directory a gsmgate build records its machines in. Unset, the
// gsmgate build records nothing.
const EnvDir = "GSM_GATE_DIR"

// Record kinds.
const (
	KindBuild         = "build"         // a Registry.Build result, accepted or rejected
	KindSynthesized   = "synthesized"   // a Synthesis.Machine (BuildOrSynthesize's fallback)
	KindCompositional = "compositional" // a BuildCompositional machine: no global tables
)

// Record is one machine a program made. The checker inputs sit next to it:
// <stem>.rules (WriteMachineAST), <stem>.pairs (WriteDeclaredPairs) and
// <stem>.tables (format 2, as WriteConvergenceTables writes), each present only
// when it could be exported.
type Record struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	BuildOK  bool   `json:"build_ok"`            // KindBuild: Build returned no error
	BuildErr string `json:"build_err,omitempty"` // KindBuild: Build's error
	WFCFail  bool   `json:"wfc_fail,omitempty"`  // Build rejected it for WFC
	CCFail   bool   `json:"cc_fail,omitempty"`   // Build rejected it for CC; the tables are then the registry's without the CC phase
	// Why the rules or the tables were not exported (closures, a synthesized
	// repair, a compositional machine, or a Build that failed before CC).
	RulesErr  string `json:"rules_err,omitempty"`
	TablesErr string `json:"tables_err,omitempty"`

	stem string // path without extension, set when read
}

var seq atomic.Int64

// WriteRecord writes rec and the exported checker inputs (nil when not
// exported) into dir. The record is written last, so a record on disk always has
// its inputs.
func WriteRecord(dir string, rec Record, rules, pairs, tables []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stem := filepath.Join(dir, fmt.Sprintf("%d-%04d", os.Getpid(), seq.Add(1)))
	for _, f := range []struct {
		ext  string
		data []byte
	}{{".rules", rules}, {".pairs", pairs}, {".tables", tables}} {
		if f.data == nil {
			continue
		}
		if err := os.WriteFile(stem+f.ext, f.data, 0o644); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stem+".json", b, 0o644)
}

// input returns the path of one of the record's checker inputs, or "" when it
// was not exported.
func (r Record) input(ext string) string {
	p := r.stem + ext
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// readRecords returns the records under root, keyed by program: the directory a
// record sits in, relative to root, with forward slashes.
func readRecords(root string) (map[string][]Record, error) {
	out := map[string][]Record{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var rec Record
		if err = json.Unmarshal(b, &rec); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		rec.stem = path[:len(path)-len(".json")]
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		prog := filepath.ToSlash(rel)
		out[prog] = append(out[prog], rec)
		return nil
	})
	return out, err
}
