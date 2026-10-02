package gsm_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// The extracted checkers must decide exactly the property Build checks:
//   - repair terminates from every state (WFC over the whole state space);
//   - only the pairs declared independent are checked (every pair when none is);
//   - commutation is checked on the valid states plus the zero state, valid or
//     not, under gsm's step (which normalizes even when a guard is false).
// Each machine below is one where the checkers on normalization-confluence main
// (stage 0) and Build disagreed.

func writeTables(t *testing.T, m *gsm.Machine) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.tables")
	if err := m.WriteConvergenceTables(p); err != nil {
		t.Fatalf("WriteConvergenceTables: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read tables: %v", err)
	}
	return b
}

// runASTOracle runs the rules oracle on a machine and, when pairs is non-nil, a
// pairs file.
func runASTOracle(t *testing.T, machine, pairs []byte) (int, string) {
	t.Helper()
	bin := oracleBinary(t, envASTOracle)
	dir := t.TempDir()
	mp := filepath.Join(dir, "m.machine")
	if err := os.WriteFile(mp, machine, 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{mp}
	if pairs != nil {
		pp := filepath.Join(dir, "m.pairs")
		if err := os.WriteFile(pp, pairs, 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, pp)
	}
	return runOracleArgs(t, bin, args...)
}

// declaredPairsMachine: a := 0 and a := 1 do not commute, but only (seta0, setb)
// and (seta1, setb) are declared independent, so Build accepts.
func declaredPairsMachine() *gsm.Registry {
	r := gsm.NewRegistry("declared-pairs")
	a := r.Int("a", 0, 1)
	b := r.Int("b", 0, 1)
	r.DeclEvent("seta0", gsm.Do(gsm.Set(a, gsm.Lit(0))))
	r.DeclEvent("seta1", gsm.Do(gsm.Set(a, gsm.Lit(1))))
	r.DeclEvent("setb", gsm.Do(gsm.Set(b, gsm.Lit(1))))
	r.Independent("seta0", "setb").Independent("seta1", "setb")
	return r
}

// TestConvergenceTables_FormatV2: the tables carry the normal-form table and the
// declared pairs, so the table oracle can check Build's property.
func TestConvergenceTables_FormatV2(t *testing.T) {
	m, _, err := declaredPairsMachine().Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := string(writeTables(t, m))
	want := "gsm-tables 2\n4 3\nnf 0 1 2 3\npairs 2 0 2 1 2\n0 0 2 2\n1 1 3 3\n2 3 2 3\n"
	if got != want {
		t.Fatalf("tables:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteDeclaredPairs_Default: with no Independent declarations every pair is
// checked.
func TestWriteDeclaredPairs_Default(t *testing.T) {
	var b bytes.Buffer
	if err := invalidStateMachine().WriteDeclaredPairs(&b); err != nil {
		t.Fatal(err)
	}
	if b.String() != "pairs all\n" {
		t.Fatalf("WriteDeclaredPairs = %q, want %q", b.String(), "pairs all\n")
	}
}

// TestConvergenceTables_DeclaredPairs: Build checks only the declared pairs, and
// so must the table oracle.
func TestConvergenceTables_DeclaredPairs(t *testing.T) {
	m, rep, err := declaredPairsMachine().Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	code, out := runOracle(t, oracleBinary(t, envTableOracle), "declared.tables", writeTables(t, m))
	if code != 0 {
		t.Fatalf("table oracle must accept (exit 0), got %d:\n%s", code, out)
	}
}

// invalidStateMachine: x in 0..2, x = 2 is invalid (repaired to 1). The two
// events act only at x = 2, where they do not commute; on the valid states and
// the zero state they are no-ops, so Build accepts.
func invalidStateMachine() *gsm.Registry {
	r := gsm.NewRegistry("invalid-state-only")
	x := r.Int("x", 0, 2)
	r.DeclInvariant("x_le_1", gsm.Le(gsm.V(x), gsm.Lit(1)), gsm.Do(gsm.Set(x, gsm.Lit(1))))
	r.DeclEventGuarded("to0", gsm.Eq(gsm.V(x), gsm.Lit(2)), gsm.Do(gsm.Set(x, gsm.Lit(0))))
	r.DeclEventGuarded("to1", gsm.Eq(gsm.V(x), gsm.Lit(2)), gsm.Do(gsm.Set(x, gsm.Lit(1))))
	return r
}

// TestConvergenceTables_InvalidStatesNotChecked: Build checks valid states and the
// zero state; the tables include invariant-invalid encodings, which the oracle
// must not hold to commutation.
func TestConvergenceTables_InvalidStatesNotChecked(t *testing.T) {
	r := invalidStateMachine()
	m, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	code, out := runOracle(t, oracleBinary(t, envTableOracle), "invalid-state.tables", writeTables(t, m))
	if code != 0 {
		t.Fatalf("table oracle must accept (exit 0), got %d:\n%s", code, out)
	}
	code, out = runASTOracle(t, exportAST(t, r), nil)
	if code != 0 {
		t.Fatalf("rules oracle must accept (exit 0), got %d:\n%s", code, out)
	}
}

// TestMachineAST_RepairTerminatesEverywhere: x = 2 repairs to itself. No event
// reaches x = 2 from a valid state, but Build requires repair to terminate from
// every state (WFC over the whole state space), and so must the rules oracle.
func TestMachineAST_RepairTerminatesEverywhere(t *testing.T) {
	r := gsm.NewRegistry("wfc-unreachable")
	x := r.Int("x", 0, 2)
	r.DeclInvariant("x_le_1", gsm.Le(gsm.V(x), gsm.Lit(1)), gsm.Do(gsm.Set(x, gsm.Lit(2))))
	r.DeclEvent("reset", gsm.Do(gsm.Set(x, gsm.Lit(0))))
	_, rep, err := r.Build()
	if err == nil || rep == nil || rep.WFC {
		t.Fatalf("gsm must reject with WFC, got err=%v report=%v", err, rep)
	}
	code, out := runASTOracle(t, exportAST(t, r), nil)
	if code != 1 || !strings.Contains(out, "WFC") {
		t.Fatalf("rules oracle must reject on WFC (exit 1), got %d:\n%s", code, out)
	}
}

// TestMachineAST_DeclaredPairs: the rules oracle checks the declared pairs when
// given a pairs file, and every pair without one.
func TestMachineAST_DeclaredPairs(t *testing.T) {
	r := declaredPairsMachine()
	if _, rep, err := r.Build(); err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	machine := exportAST(t, r)
	var pairs bytes.Buffer
	if err := r.WriteDeclaredPairs(&pairs); err != nil {
		t.Fatal(err)
	}
	if got, want := pairs.String(), "pairs 2 0 2 1 2\n"; got != want {
		t.Fatalf("WriteDeclaredPairs = %q, want %q", got, want)
	}
	if code, out := runASTOracle(t, machine, pairs.Bytes()); code != 0 {
		t.Fatalf("rules oracle must accept the declared pairs (exit 0), got %d:\n%s", code, out)
	}
	if code, out := runASTOracle(t, machine, nil); code != 1 {
		t.Fatalf("without a pairs file every pair is checked, so the oracle must reject (exit 1), got %d:\n%s", code, out)
	}
}

// zeroInvalidMachine: a + b >= 1, repaired by a := 1, so the zero state is
// invalid. seta and markb commute on every valid state but not at zero, which
// Build checks because Machine.NewState starts there.
func zeroInvalidMachine(guardedFirst bool) *gsm.Registry {
	r := gsm.NewRegistry("zero-invalid")
	a := r.Int("a", 0, 1)
	b := r.Int("b", 0, 1)
	r.DeclInvariant("a_or_b", gsm.Le(gsm.Lit(1), gsm.Add(gsm.V(a), gsm.V(b))), gsm.Do(gsm.Set(a, gsm.Lit(1))))
	if guardedFirst {
		// At zero this guard is false. gsm's step still normalizes (to a=1),
		// and that is what makes the pair fail there.
		r.DeclEventGuarded("markb_if_a", gsm.Eq(gsm.V(a), gsm.Lit(1)), gsm.Do(gsm.Set(b, gsm.Lit(1))))
	} else {
		r.DeclEvent("seta", gsm.Do(gsm.Set(a, gsm.Lit(1))))
	}
	r.DeclEventGuarded("markb_if_not_a", gsm.Eq(gsm.V(a), gsm.Lit(0)), gsm.Do(gsm.Set(b, gsm.Lit(1))))
	return r
}

func TestMachineAST_InvalidZeroState(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		r := zeroInvalidMachine(guarded)
		_, rep, err := r.Build()
		if err == nil || rep == nil || rep.CCFailure == nil {
			t.Fatalf("guarded=%v: gsm must reject with CC, got err=%v report=%v", guarded, err, rep)
		}
		if got := rep.CCFailure.State.String(); !strings.Contains(got, "a=0") || !strings.Contains(got, "b=0") {
			t.Fatalf("guarded=%v: the CC counterexample must be the zero state, got %s", guarded, got)
		}
		code, out := runASTOracle(t, exportAST(t, r), nil)
		if code != 1 {
			t.Fatalf("guarded=%v: rules oracle must reject (exit 1), got %d:\n%s", guarded, code, out)
		}
	}
}
