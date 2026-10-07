package gsm_test

import (
	"fmt"
	"math/bits"
	"math/rand"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// The differential test for CheckMigration: on random small registries, migrations and event
// maps, its classification must agree with a brute-force enumeration of the switch's runs,
// computed by a model written here from the rule tables (not from gsm's code), and every
// witness must replay in that model.

// tmodel is a registry given by tables, over one Int x in 0..n-1.
type tmodel struct {
	n     int
	valid []bool
	rep   []int   // the repair of an invalid state
	eff   [][]int // eff[e][s]: the effect of event e at s
}

func (m tmodel) nf(s int) int {
	for !m.valid[s] {
		s = m.rep[s]
	}
	return s
}

// step is Machine.Apply: normalize an input other than the zero state, apply, repair.
func (m tmodel) step(e, s int) int {
	if s != 0 {
		s = m.nf(s)
	}
	return m.nf(m.eff[e][s])
}

func randModel(rng *rand.Rand, n, ne int) tmodel {
	m := tmodel{n: n, valid: make([]bool, n), rep: make([]int, n), eff: make([][]int, ne)}
	anyValid := false
	for s := range m.valid {
		m.valid[s] = rng.Intn(10) < 6
		anyValid = anyValid || m.valid[s]
	}
	if !anyValid {
		m.valid[rng.Intn(n)] = true
	}
	for s := 0; s < n; s++ {
		if m.valid[s] {
			continue
		}
		// Repair to a valid state or a lower one: terminating.
		var cands []int
		for t := 0; t < n; t++ {
			if m.valid[t] || t < s {
				cands = append(cands, t)
			}
		}
		m.rep[s] = cands[rng.Intn(len(cands))]
	}
	for e := range m.eff {
		m.eff[e] = make([]int, n)
		for s := range m.eff[e] {
			m.eff[e][s] = rng.Intn(n)
		}
	}
	return m
}

func (m tmodel) registry(name, prefix string) (*gsm.Registry, gsm.Var) {
	r := gsm.NewRegistry(name)
	x := r.Int("x", 0, m.n-1)
	r.Invariant("valid").Watches(x).
		Holds(func(s gsm.State) bool { return m.valid[s.GetInt(x)] }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(x, m.rep[s.GetInt(x)]) }).Add()
	for e := range m.eff {
		e := e
		r.Event(fmt.Sprintf("%s%d", prefix, e)).Writes(x).
			Apply(func(s gsm.State) gsm.State { return s.SetInt(x, m.eff[e][s.GetInt(x)]) }).Add()
	}
	return r, x
}

// bfInstance is a change from A to B with migration table mig and event translation tau.
type bfInstance struct {
	A, B tmodel
	mig  []int
	tau  []int
	memo map[bfKey]uint64
}

type bfKey struct {
	barrier, inB bool
	s            int
	ra, rb       [3]int
}

func (b *bfInstance) image(s int) int { return b.B.nf(b.mig[s]) }

// finals returns the set (a bitmask over B's states) of final states of every run from the
// configuration: under A with ra of A's events and rb of B's events still to apply, or under B.
func (b *bfInstance) finals(k bfKey) uint64 {
	if v, ok := b.memo[k]; ok {
		return v
	}
	var out uint64
	if k.inB {
		empty := true
		for e, c := range k.rb {
			if c == 0 {
				continue
			}
			empty = false
			n := k
			n.rb[e]--
			n.s = b.B.step(e, k.s)
			out |= b.finals(n)
		}
		if empty {
			out = 1 << uint(k.s)
		}
	} else {
		drained := true
		for e, c := range k.ra {
			if c == 0 {
				continue
			}
			drained = false
			n := k
			n.ra[e]--
			n.s = b.A.step(e, k.s)
			out |= b.finals(n)
		}
		if !k.barrier || drained {
			n := bfKey{barrier: k.barrier, inB: true, s: b.image(k.s), rb: k.rb}
			for e := range b.tau {
				n.rb[b.tau[e]] += k.ra[e]
			}
			out |= b.finals(n)
		}
	}
	b.memo[k] = out
	return out
}

// diverges reports whether some multisets of at most ka A-events and kb B-events have two
// runs from s0 with different final states.
func (b *bfInstance) diverges(barrier bool, s0, ka, kb int) bool {
	vecs := func(ne, k int) [][3]int {
		var out [][3]int
		var rec func(i, left int, cur [3]int)
		rec = func(i, left int, cur [3]int) {
			if i == ne {
				out = append(out, cur)
				return
			}
			for c := 0; c <= left; c++ {
				cur[i] = c
				rec(i+1, left-c, cur)
			}
		}
		rec(0, k, [3]int{})
		return out
	}
	for _, ra := range vecs(len(b.A.eff), ka) {
		for _, rb := range vecs(len(b.B.eff), kb) {
			if bits.OnesCount64(b.finals(bfKey{barrier: barrier, s: s0, ra: ra, rb: rb})) > 1 {
				return true
			}
		}
	}
	return false
}

// replay runs a witness run in the model and returns the final B state.
func (b *bfInstance) replay(s0 int, r gsm.MigrationRun) (int, error) {
	idx := func(name string, prefix byte, ne int) (int, error) {
		var e int
		if _, err := fmt.Sscanf(name, string(prefix)+"%d", &e); err != nil || e < 0 || e >= ne {
			return 0, fmt.Errorf("unknown event %q", name)
		}
		return e, nil
	}
	s := s0
	for _, name := range r.Before {
		e, err := idx(name, 'a', len(b.A.eff))
		if err != nil {
			return 0, err
		}
		s = b.A.step(e, s)
	}
	if len(r.After) < len(r.InFlight) {
		return 0, fmt.Errorf("fewer events after the switch than in flight")
	}
	for i, name := range r.InFlight {
		e, err := idx(name, 'a', len(b.A.eff))
		if err != nil {
			return 0, err
		}
		if want := fmt.Sprintf("b%d", b.tau[e]); r.After[i] != want {
			return 0, fmt.Errorf("in-flight %s is applied as %s, want %s", name, r.After[i], want)
		}
	}
	x := b.image(s)
	for _, name := range r.After {
		e, err := idx(name, 'b', len(b.B.eff))
		if err != nil {
			return 0, err
		}
		x = b.B.step(e, x)
	}
	return x, nil
}

func multiset(xs ...[]string) map[string]int {
	m := map[string]int{}
	for _, l := range xs {
		for _, x := range l {
			m[x]++
		}
	}
	return m
}

func sameCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// checkWitness replays w in the model: both runs from the start, with the same submitted
// events, ending at the reported, different states.
func (b *bfInstance) checkWitness(t *testing.T, where string, w *gsm.MigrationWitness, x gsm.Var, y gsm.Var, barrier bool) {
	t.Helper()
	if w == nil {
		t.Fatalf("%s: no witness", where)
	}
	s0 := w.Start.GetInt(x)
	r1, err1 := b.replay(s0, w.Run1)
	r2, err2 := b.replay(s0, w.Run2)
	if err1 != nil || err2 != nil {
		t.Fatalf("%s: witness does not replay: %v %v", where, err1, err2)
	}
	if r1 != w.Result1.GetInt(y) || r2 != w.Result2.GetInt(y) || r1 == r2 {
		t.Fatalf("%s: witness replays to %d and %d, reported %s and %s", where, r1, r2, w.Result1, w.Result2)
	}
	if !sameCounts(multiset(w.Run1.Before, w.Run1.InFlight), multiset(w.Run2.Before, w.Run2.InFlight)) ||
		!sameCounts(multiset(w.Run1.After[len(w.Run1.InFlight):]), multiset(w.Run2.After[len(w.Run2.InFlight):])) {
		t.Fatalf("%s: the witness runs have different events: %+v %+v", where, w.Run1, w.Run2)
	}
	if barrier && (len(w.Run1.InFlight) > 0 || len(w.Run2.InFlight) > 0 || !w.Barrier) {
		t.Fatalf("%s: a barrier witness has an event in flight: %+v %+v", where, w.Run1, w.Run2)
	}
}

func TestCheckMigration_DifferentialRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(20))
	trials := 500
	if testing.Short() {
		trials = 120
	}
	const ka, kb = 4, 3
	seen := map[gsm.MigrationOutcome]int{}
	anchored := 0
	for trial := 0; trial < trials; trial++ {
		nA, nB := 2+rng.Intn(3), 2+rng.Intn(3)
		eA, eB := 1+rng.Intn(3), 1+rng.Intn(3)
		inst := &bfInstance{A: randModel(rng, nA, eA), B: randModel(rng, nB, eB), memo: map[bfKey]uint64{}}
		inst.mig = make([]int, nA)
		for s := range inst.mig {
			inst.mig[s] = rng.Intn(nB)
		}
		inst.tau = make([]int, eA)
		events := map[string]string{}
		for e := range inst.tau {
			inst.tau[e] = rng.Intn(eB)
			events[fmt.Sprintf("a%d", e)] = fmt.Sprintf("b%d", inst.tau[e])
		}
		ra, x := inst.A.registry("A", "a")
		rb, y := inst.B.registry("B", "b")
		mig := func(old, blank gsm.State) gsm.State { return blank.SetInt(y, inst.mig[old.GetInt(x)]) }
		s0 := 0
		var opts []gsm.MigrationOption
		where := fmt.Sprintf("trial %d", trial)

		// Anchor the model to gsm's runtime: where a side builds, its Apply is the model's step.
		for _, side := range []struct {
			r *gsm.Registry
			v gsm.Var
			m tmodel
		}{{ra, x, inst.A}, {rb, y, inst.B}} {
			mach, _, err := side.r.Build()
			if err != nil {
				continue
			}
			anchored++
			for s := 0; s < side.m.n; s++ {
				st := mach.NewState().SetInt(side.v, s)
				for e := range side.m.eff {
					name := mach.Events()[e]
					if got := mach.Apply(st, name).GetInt(side.v); got != side.m.step(e, s) {
						t.Fatalf("%s: model step %s at %d is %d, Apply gives %d", where, name, s, side.m.step(e, s), got)
					}
				}
			}
		}
		if trial%3 == 0 {
			// A random start: a state of a registry with A's variable declaration, which
			// CheckMigration accepts as a state of A.
			twin := gsm.NewRegistry("twin")
			tx := twin.Int("x", 0, nA-1)
			tm, _, err := twin.Build()
			if err != nil {
				t.Fatal(err)
			}
			s0 = rng.Intn(nA)
			opts = append(opts, gsm.MigrationFrom(tm.NewState().SetInt(tx, s0)))
		}

		rep, err := gsm.CheckMigration(ra, rb, mig, events, opts...)
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		seen[rep.Outcome]++
		liveDiv := inst.diverges(false, s0, ka, kb)
		barDiv := inst.diverges(true, s0, ka, kb)
		ctx := func() string {
			return fmt.Sprintf("%s (A %+v, B %+v, mig %v, tau %v, start %d)\n%s", where, inst.A, inst.B, inst.mig, inst.tau, s0, rep)
		}
		switch rep.Outcome {
		case gsm.MigrationSafeOnline:
			if liveDiv {
				t.Fatalf("SAFE ONLINE, but two live runs diverge: %s", ctx())
			}
		case gsm.MigrationSafeBehindBarrier, gsm.MigrationUnknown:
			// Unknown claims nothing; that no barrier divergence exists then is what the AmodM
			// search's failure to find a witness predicts, and is checked here, not claimed.
			if barDiv {
				t.Fatalf("%v, but two barrier runs diverge: %s", rep.Outcome, ctx())
			}
		case gsm.MigrationUnsafe:
			inst.checkWitness(t, where, rep.BarrierWitness, x, y, true)
		}
		if rep.Outcome != gsm.MigrationSafeOnline {
			inst.checkWitness(t, where, rep.LiveWitness, x, y, false)
			w := rep.LiveWitness
			na := len(w.Run1.Before) + len(w.Run1.InFlight)
			nb := len(w.Run1.After) - len(w.Run1.InFlight)
			if na <= ka && nb <= kb && !liveDiv {
				t.Fatalf("the live witness fits the brute force's bound, which finds no divergence: %s", ctx())
			}
		}
		if barDiv && rep.Outcome != gsm.MigrationUnsafe {
			t.Fatalf("two barrier runs diverge, outcome %v: %s", rep.Outcome, ctx())
		}
	}
	t.Logf("outcomes over %d trials: %v; %d sides anchored to a built machine's Apply", trials, seen, anchored)
	for _, o := range []gsm.MigrationOutcome{gsm.MigrationSafeOnline, gsm.MigrationSafeBehindBarrier, gsm.MigrationUnsafe, gsm.MigrationUnknown} {
		if seen[o] == 0 {
			t.Errorf("no trial produced %v", o)
		}
	}
}
