package gsm_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestMachineAST_WriteAndVerify is the RULES-level differential test against the
// external machine-checked AST oracle (normalization-confluence coq/extraction,
// the `astchecker` binary). Where TestConvergenceTables_WriteAndVerify cross-checks
// gsm's emitted step TABLES, this cross-checks gsm's convergence verdict against a
// verifier that recomputes convergence straight from the combinator RULES, so it
// does not trust gsm to have enumerated or normalized anything correctly.
//
// gsm builds a combinator machine and confirms WFC+CC in Go; it then serializes the
// rules and, if GSM_AST_CHECKER points at the built astchecker binary, asserts the
// verified oracle independently agrees the rules converge. Without the env var the
// test still exercises serialization (so it is a friendly default).
func TestMachineAST_WriteAndVerify(t *testing.T) {
	r := gsm.NewRegistry("commuting-combinators")
	a := r.Int("a", 0, 5)
	b := r.Int("b", 0, 5)
	flag := r.Bool("flag")

	r.DeclInvariant("a_cap", gsm.Le(gsm.V(a), gsm.Lit(3)), gsm.Do(gsm.Set(a, gsm.Lit(3))))
	r.DeclEvent("inc_a", gsm.Do(gsm.Set(a, gsm.Add(gsm.V(a), gsm.Lit(1)))))
	r.DeclEvent("inc_b", gsm.Do(gsm.Set(b, gsm.Add(gsm.V(b), gsm.Lit(1)))))
	r.DeclEvent("raise_flag", gsm.Do(gsm.Set(flag, gsm.Lit(1))))

	_, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	if !rep.WFC || !rep.CC {
		t.Fatalf("gsm expected WFC+CC before cross-check, got %s", rep)
	}

	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		t.Fatalf("WriteMachineAST: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("machine AST not written")
	}
	path := filepath.Join(t.TempDir(), "commuting.machine")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write machine file: %v", err)
	}

	checker := os.Getenv("GSM_AST_CHECKER")
	if checker == "" {
		t.Skipf("set GSM_AST_CHECKER to the verified astchecker binary to run the rules-level differential cross-check\nserialized machine:\n%s", buf.String())
	}
	out, err := exec.Command(checker, path).CombinedOutput()
	t.Logf("verified AST oracle: %s", out)
	if err != nil {
		t.Fatalf("verified AST oracle rejected gsm's rules (gsm said WFC+CC): %v", err)
	}
}

// TestMachineAST_WidenedFragment exercises the parts of the oracle beyond the
// original fragment: a variable with a nonzero minimum (min=2), an invariant using
// a disjunction (Or), and a guarded event (OnlyIf). gsm builds it convergent and the
// verified oracle re-certifies straight from the rules.
func TestMachineAST_WidenedFragment(t *testing.T) {
	r := gsm.NewRegistry("widened")
	a := r.Int("a", 2, 6) // min=2, domain 2..6
	b := r.Int("b", 0, 4)
	flag := r.Bool("flag")

	r.Rule("cap_a").Require(gsm.AtMost(a, 5)).RepairWith(gsm.SetTo(a, 5)).Add()
	// Disjunction that always holds (a >= 2 by construction); exercises Or export/eval.
	r.Rule("flag_or_a").Require(gsm.Or(gsm.Is(flag, 1), gsm.AtLeast(a, 2))).RepairWith(gsm.Raise(flag)).Add()

	// Guarded increment on a; independent increments on b and flag (disjoint writes commute).
	r.On("warm").OnlyIf(gsm.Below(a, 6)).Does(gsm.Inc(a)).Add()
	r.On("inc_b").Does(gsm.Inc(b)).Add()
	r.On("raise").Does(gsm.Raise(flag)).Add()

	_, rep, err := r.Build()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, rep)
	}
	if !rep.WFC || !rep.CC {
		t.Fatalf("gsm expected WFC+CC, got %s", rep)
	}

	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		t.Fatalf("WriteMachineAST: %v", err)
	}
	path := filepath.Join(t.TempDir(), "widened.machine")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write machine file: %v", err)
	}

	checker := os.Getenv("GSM_AST_CHECKER")
	if checker == "" {
		t.Skipf("set GSM_AST_CHECKER to run the widened-fragment cross-check\nserialized machine:\n%s", buf.String())
	}
	out, err := exec.Command(checker, path).CombinedOutput()
	t.Logf("verified AST oracle: %s", out)
	if err != nil {
		t.Fatalf("verified AST oracle rejected gsm's rules (gsm said WFC+CC): %v", err)
	}
}

// TestMachineAST_RejectsNonConvergent confirms the differential test has teeth: a
// machine whose events do NOT commute (two events write the same variable with
// order-dependent results, and no compensation restores order-independence) must be
// rejected by the verified oracle when it is available.
func TestMachineAST_RejectsNonConvergent(t *testing.T) {
	r := gsm.NewRegistry("non-commuting-combinators")
	x := r.Int("x", 0, 5)
	// Two events that both write x with different constants: order decides the result,
	// and there is no invariant to compensate, so the machine is not convergent.
	r.DeclEvent("set2", gsm.Do(gsm.Set(x, gsm.Lit(2))))
	r.DeclEvent("set4", gsm.Do(gsm.Set(x, gsm.Lit(4))))

	var buf bytes.Buffer
	if err := r.WriteMachineAST(&buf); err != nil {
		t.Fatalf("WriteMachineAST: %v", err)
	}
	path := filepath.Join(t.TempDir(), "nonconv.machine")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write machine file: %v", err)
	}

	checker := os.Getenv("GSM_AST_CHECKER")
	if checker == "" {
		t.Skip("set GSM_AST_CHECKER to the verified astchecker binary to run the rejection cross-check")
	}
	out, err := exec.Command(checker, path).CombinedOutput()
	t.Logf("verified AST oracle: %s", out)
	if err == nil {
		t.Fatalf("verified AST oracle ACCEPTED a non-convergent machine (expected rejection):\n%s", out)
	}
}
