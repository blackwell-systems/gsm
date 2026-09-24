package gsm_test

import (
	"math/rand"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// buildCommutingMachine builds a machine whose events are ALL pairwise independent, so a
// passing Build means every permutation of every event multiset must reach the same normal
// form. It deliberately exercises both CC-verification paths:
//   - inc_a / inc_a2 both write `a` (share the a_cap invariant footprint) → brute-force CC;
//     they commute because both are +1 with an order-independent clamp.
//   - inc_b, raise_flag touch disjoint state → CC by footprint disjointness.
func buildCommutingMachine(t *testing.T) (m *gsm.Machine, a, b, flag gsm.Var) {
	t.Helper()
	r := gsm.NewRegistry("commuting")

	a = r.Int("a", 0, 5)
	b = r.Int("b", 0, 5)
	flag = r.Bool("flag")

	// a is capped at 3 by compensation; incrementing past the cap clamps, order-independently.
	r.Invariant("a_cap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 3 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, 3) }).Add()

	inc := func(name string, v gsm.Var) {
		r.Event(name).Writes(v).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
	}
	inc("inc_a", a)
	inc("inc_a2", a)
	inc("inc_b", b)
	r.Event("raise_flag").Writes(flag).
		Apply(func(s gsm.State) gsm.State { return s.SetBool(flag, true) }).Add()

	// No Independent() calls → all pairs are checked for CC (allIndependent mode). A passing
	// Build therefore certifies full order-independence.
	machine, rep, err := r.Build()
	if err != nil {
		t.Fatalf("commuting machine failed to build: %v\n%s", err, rep)
	}
	if !rep.CC || !rep.WFC {
		t.Fatalf("expected WFC+CC, got %s", rep)
	}
	return machine, a, b, flag
}

// applySeq folds a sequence of events over the machine from its zero state.
func applySeq(m *gsm.Machine, seq []string) gsm.State {
	s := m.NewState()
	for _, e := range seq {
		s = m.Apply(s, e)
	}
	return s
}

// permutations returns every ordering of the input (Heap's algorithm). Callers keep the
// multiset small enough that n! is cheap.
func permutations(in []string) [][]string {
	var out [][]string
	a := append([]string(nil), in...)
	var gen func(k int)
	gen = func(k int) {
		if k == 1 {
			out = append(out, append([]string(nil), a...))
			return
		}
		for i := 0; i < k; i++ {
			gen(k - 1)
			if k%2 == 0 {
				a[i], a[k-1] = a[k-1], a[i]
			} else {
				a[0], a[k-1] = a[k-1], a[0]
			}
		}
	}
	gen(len(a))
	return out
}

// TestConfluence_AllPermutations is the core normalization-confluence guarantee at full
// strength: for an all-independent machine, EVERY permutation of an event multiset lands on
// exactly one normal form. This is Theorem 5.4 (Stream Convergence) exercised exhaustively.
func TestConfluence_AllPermutations(t *testing.T) {
	m, a, b, flag := buildCommutingMachine(t)

	// 6 events (720 permutations), with repeats across both CC paths.
	multiset := []string{"inc_a", "inc_a2", "inc_a", "inc_b", "raise_flag", "inc_b"}

	perms := permutations(multiset)
	if len(perms) != 720 {
		t.Fatalf("expected 720 permutations, got %d", len(perms))
	}

	want := applySeq(m, perms[0])
	for _, p := range perms {
		got := applySeq(m, p)
		if got.ID() != want.ID() {
			t.Fatalf("confluence violated: order %v → %s, but %v → %s", perms[0], want, p, got)
		}
	}

	// Value check: a incremented 3× then capped at 3, b incremented 2×, flag raised.
	if got := want.GetInt(a); got != 3 {
		t.Fatalf("a = %d, want 3 (capped)", got)
	}
	if got := want.GetInt(b); got != 2 {
		t.Fatalf("b = %d, want 2", got)
	}
	if !want.GetBool(flag) {
		t.Fatal("flag = false, want true")
	}
}

// TestConfluence_RandomizedMultisets shuffles many random multisets and confirms every shuffle
// of a given multiset converges to the same state as its canonical order — confluence under
// random interleaving, complementing the exhaustive small-case check.
func TestConfluence_RandomizedMultisets(t *testing.T) {
	m, _, _, _ := buildCommutingMachine(t)
	events := []string{"inc_a", "inc_a2", "inc_b", "raise_flag"}
	rng := rand.New(rand.NewSource(1)) // fixed seed → deterministic test

	for trial := 0; trial < 500; trial++ {
		// Build a random multiset of length 1..12.
		n := 1 + rng.Intn(12)
		base := make([]string, n)
		for i := range base {
			base[i] = events[rng.Intn(len(events))]
		}
		want := applySeq(m, base)

		// Three independent shuffles of the SAME multiset must all agree with `want`.
		for s := 0; s < 3; s++ {
			shuf := append([]string(nil), base...)
			rng.Shuffle(len(shuf), func(i, j int) { shuf[i], shuf[j] = shuf[j], shuf[i] })
			if got := applySeq(m, shuf); got.ID() != want.ID() {
				t.Fatalf("trial %d: shuffle diverged\n base: %v → %s\n shuf: %v → %s", trial, base, want, shuf, got)
			}
		}
	}
}

// TestConfluence_NormalFormAlwaysValid confirms every reachable state (after any event) is a
// valid normal form — compensation always restores validity (WFC).
func TestConfluence_NormalFormAlwaysValid(t *testing.T) {
	m, _, _, _ := buildCommutingMachine(t)
	events := []string{"inc_a", "inc_a2", "inc_b", "raise_flag"}
	rng := rand.New(rand.NewSource(2))
	s := m.NewState()
	for i := 0; i < 2000; i++ {
		s = m.Apply(s, events[rng.Intn(len(events))])
		if !m.IsValid(s) {
			t.Fatalf("step %d produced an invalid state: %s", i, s)
		}
	}
}
