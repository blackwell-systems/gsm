package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestSynthesize_FindsRepair: a registry whose invariants + events admit a convergent
// compensation. Synthesize discovers one (no Repair provided), and the resulting Machine
// actually converges.
func TestSynthesize_FindsRepair(t *testing.T) {
	// Approval with auto-approve: stage {pending,approved,held} × balance [0,2].
	r := gsm.NewRegistry("approval")
	stage := r.Enum("stage", "pending", "approved", "held")
	balance := r.Int("balance", 0, 2)
	// Invariant (validity only — NO Repair; that's what we synthesize).
	r.Invariant("funded_if_approved").Watches(stage, balance).
		Holds(func(s gsm.State) bool { return s.Get(stage) != "approved" || s.GetInt(balance) >= 2 }).
		Add()
	r.Event("approve").Writes(stage).
		Apply(func(s gsm.State) gsm.State { return s.Set(stage, "approved") }).Add()
	r.Event("credit").Writes(stage, balance).
		Apply(func(s gsm.State) gsm.State {
			b := s.GetInt(balance)
			if b < 2 {
				b++
			}
			if s.Get(stage) == "held" && b >= 2 {
				return s.Set(stage, "approved").SetInt(balance, b)
			}
			return s.SetInt(balance, b)
		}).Add()

	syn, err := r.Synthesize()
	if err != nil {
		t.Fatal(err)
	}
	if !syn.Convergent {
		t.Fatalf("expected a convergent compensation to exist:\n%s", syn)
	}
	if syn.Alternatives < 1 {
		t.Fatalf("Convergent but Alternatives=%d", syn.Alternatives)
	}

	// The synthesized machine must actually be usable and order-independent.
	m := syn.Machine()
	if m == nil {
		t.Fatal("Machine() returned nil for a convergent synthesis")
	}
	s0 := m.NewState()
	a := m.Apply(m.Apply(s0, "approve"), "credit")
	b := m.Apply(m.Apply(s0, "credit"), "approve")
	if a.ID() != b.ID() {
		t.Fatalf("synthesized machine not order-independent: %s vs %s", a, b)
	}
	if !m.IsValid(a) {
		t.Fatalf("synthesized machine reached an invalid state: %s", a)
	}

	// The repair map and report should be populated for a convergent synthesis.
	if len(syn.Repairs()) == 0 {
		t.Fatal("Repairs() empty for a convergent synthesis")
	}
	if !strings.Contains(syn.String(), "CONVERGENT") {
		t.Fatalf("report should say CONVERGENT:\n%s", syn)
	}
}

// TestSynthesize_ProvesImpossible: two events set the same variable to different constants.
// No compensation can reconcile last-writer-wins, and there are no invalid states to reroute,
// so Synthesize proves impossibility.
func TestSynthesize_ProvesImpossible(t *testing.T) {
	r := gsm.NewRegistry("conflict")
	x := r.Int("x", 0, 2)
	// No invariant → all states valid → nf is forced to identity.
	r.Event("set1").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 1) }).Add()
	r.Event("set2").Writes(x).Apply(func(s gsm.State) gsm.State { return s.SetInt(x, 2) }).Add()

	syn, err := r.Synthesize()
	if err != nil {
		t.Fatal(err)
	}
	if syn.Convergent {
		t.Fatalf("expected impossibility, but found a convergent compensation:\n%s", syn)
	}
	if !strings.Contains(syn.String(), "IMPOSSIBLE") {
		t.Fatalf("report should state impossibility:\n%s", syn)
	}
}

// TestBuild_MissingRepairErrors: an invariant without a Repair is fine for Synthesize but
// Build must refuse it with a clear message (not panic).
func TestBuild_MissingRepairErrors(t *testing.T) {
	r := gsm.NewRegistry("no-repair")
	flag := r.Bool("flag")
	r.Invariant("flag_off").Watches(flag).Holds(func(s gsm.State) bool { return !s.GetBool(flag) }).Add()
	r.Event("raise").Writes(flag).Apply(func(s gsm.State) gsm.State { return s.SetBool(flag, true) }).Add()

	if _, _, err := r.Build(); err == nil || !strings.Contains(err.Error(), "Synthesize") {
		t.Fatalf("expected a missing-Repair error pointing to Synthesize, got: %v", err)
	}
}
