package gsm

import (
	"fmt"
	"math/rand"
	"testing"
)

// Property test for Build's CC verdict against brute force.
//
// It generates random small machines whose event guards and effects read
// arbitrary variables (including variables other events write), builds them with
// Build and BuildCompositional, and compares the verdicts against a reference
// that shares none of gsm's verification code: it applies events through the raw
// closures, normalizes by iterating the first violated repair, and enumerates
// every ordering of event sequences from every start state in the guarantee
// domain (each valid state, plus the zero state Machine.NewState returns).
//
// The properties:
//   - soundness: a machine Build or BuildCompositional certifies has no divergent
//     orderings in the reference, for sequences of length 2 and 3;
//   - completeness of Build: when WFC holds and every pair is checked, Build
//     certifies exactly when the reference finds no divergent pair (length 2),
//     since that is CC1 over the domain.

// randMachine is a generated machine plus the parameters needed to describe it.
type randMachine struct {
	r    *Registry
	desc string
}

// key returns the mixed-radix index of s restricted to the variables vs.
func key(s State, vs []Var) int {
	k := 0
	for _, v := range vs {
		k = k*v.domain + int(s.getRaw(v))
	}
	return k
}

func space(vs []Var) int {
	n := 1
	for _, v := range vs {
		n *= v.domain
	}
	return n
}

func randSubset(rng *rand.Rand, vars []Var, min int) []Var {
	for {
		var out []Var
		for _, v := range vars {
			if rng.Intn(2) == 0 {
				out = append(out, v)
			}
		}
		if len(out) >= min {
			return out
		}
	}
}

func names(vs []Var) string {
	s := "{"
	for i, v := range vs {
		if i > 0 {
			s += ","
		}
		s += v.name
	}
	return s + "}"
}

// genMachine builds a random machine: 2 to 4 variables of domain 2 or 3, 0 to 2
// invariants with table-driven checks over their footprint and a constant repair
// target inside it, and 2 to 4 events, each writing a random nonempty set from
// values tabulated over a random read set. With probability readOwn an event reads
// only its write set (the footprint-conformant case); otherwise its read set is
// arbitrary and usually includes variables other events write.
func genMachine(rng *rand.Rand, id int) randMachine {
	r := NewRegistry(fmt.Sprintf("rand_%d", id))
	nv := 2 + rng.Intn(3)
	vars := make([]Var, nv)
	for i := range vars {
		if rng.Intn(2) == 0 {
			vars[i] = r.Bool(fmt.Sprintf("v%d", i))
		} else {
			vars[i] = r.Int(fmt.Sprintf("v%d", i), 0, 2)
		}
	}
	desc := fmt.Sprintf("vars=%d", nv)

	ni := rng.Intn(3)
	for k := 0; k < ni; k++ {
		fp := randSubset(rng, vars, 1)
		n := space(fp)
		tbl := make([]bool, n)
		for i := range tbl {
			tbl[i] = rng.Intn(3) != 0
		}
		// The repair writes a constant assignment of the footprint that satisfies
		// the check, so a single invariant always terminates.
		target := rng.Intn(n)
		tbl[target] = true
		vals := make([]uint64, len(fp))
		t := target
		for i := len(fp) - 1; i >= 0; i-- {
			vals[i] = uint64(t % fp[i].domain)
			t /= fp[i].domain
		}
		fpc := append([]Var(nil), fp...)
		r.Invariant(fmt.Sprintf("inv%d", k)).Watches(fpc...).
			Holds(func(s State) bool { return tbl[key(s, fpc)] }).
			Repair(func(s State) State {
				for i, v := range fpc {
					s = s.setRaw(v, vals[i])
				}
				return s
			}).Add()
		desc += fmt.Sprintf(" inv%d%s", k, names(fp))
	}

	ne := 2 + rng.Intn(3)
	for k := 0; k < ne; k++ {
		ws := randSubset(rng, vars, 1)
		var rs []Var
		if rng.Intn(4) == 0 {
			rs = ws
		} else {
			rs = randSubset(rng, vars, 0)
		}
		n := space(rs)
		guard := make([]bool, n)
		for i := range guard {
			guard[i] = rng.Intn(3) != 0
		}
		eff := make([][]uint64, len(ws))
		for wi, w := range ws {
			eff[wi] = make([]uint64, n)
			for i := range eff[wi] {
				eff[wi][i] = uint64(rng.Intn(w.domain))
			}
		}
		wsc, rsc := append([]Var(nil), ws...), append([]Var(nil), rs...)
		eb := r.Event(fmt.Sprintf("e%d", k)).Writes(wsc...)
		if rng.Intn(3) != 0 {
			eb = eb.Guard(func(s State) bool { return guard[key(s, rsc)] })
		}
		eb.Apply(func(s State) State {
			kk := key(s, rsc)
			for wi, w := range wsc {
				s = s.setRaw(w, eff[wi][kk])
			}
			return s
		}).Add()
		desc += fmt.Sprintf(" e%d:w%s r%s", k, names(ws), names(rs))
	}
	return randMachine{r: r, desc: desc}
}

// refModel is the brute-force reference semantics, built from the raw closures.
type refModel struct {
	r      *Registry
	states []State // every encodable state
}

func newRef(r *Registry) *refModel {
	m := &refModel{r: r}
	var rec func(i int, s State)
	rec = func(i int, s State) {
		if i == len(r.vars) {
			m.states = append(m.states, s)
			return
		}
		for d := 0; d < r.vars[i].domain; d++ {
			rec(i+1, s.setRaw(r.vars[i], uint64(d)))
		}
	}
	rec(0, State{packed: 0, vars: r.vars})
	return m
}

func (m *refModel) holds(s State) bool {
	for _, inv := range m.r.invariants {
		if !inv.check(s) {
			return false
		}
	}
	return true
}

// normalize iterates the first violated repair; ok is false if it does not
// terminate within the state count (a WFC failure).
func (m *refModel) normalize(s State) (State, bool) {
	for steps := 0; steps <= len(m.states); steps++ {
		fired := false
		for _, inv := range m.r.invariants {
			if !inv.check(s) {
				s = inv.repair(s)
				fired = true
				break
			}
		}
		if !fired {
			return s, true
		}
	}
	return s, false
}

func (m *refModel) step(e int, s State) (State, bool) {
	ev := m.r.events[e]
	if ev.guard == nil || ev.guard(s) {
		s = ev.effect(s)
	}
	return m.normalize(s)
}

func (m *refModel) run(seq []int, s State) (State, bool) {
	for _, e := range seq {
		var ok bool
		if s, ok = m.step(e, s); !ok {
			return s, false
		}
	}
	return s, true
}

// domain is the guarantee domain: valid states plus the zero state.
func (m *refModel) domain() []State {
	var out []State
	for _, s := range m.states {
		if s.packed == 0 || m.holds(s) {
			out = append(out, s)
		}
	}
	return out
}

// wfc reports whether normalization terminates from every state.
func (m *refModel) wfc() bool {
	for _, s := range m.states {
		if _, ok := m.normalize(s); !ok {
			return false
		}
	}
	return true
}

// permutations calls fn on every ordering of seq.
func permutations(seq []int, fn func([]int)) {
	p := append([]int(nil), seq...)
	var rec func(k int)
	rec = func(k int) {
		if k == len(p) {
			fn(p)
			return
		}
		for i := k; i < len(p); i++ {
			p[k], p[i] = p[i], p[k]
			rec(k + 1)
			p[k], p[i] = p[i], p[k]
		}
	}
	rec(0)
}

// divergence searches every multiset of `length` events (with repetition) from
// every domain state for two orderings that end in different states. It returns a
// description of the first one found, or "".
func (m *refModel) divergence(starts []State, length int) string {
	ne := len(m.r.events)
	seq := make([]int, length)
	var found string
	var rec func(i, from int)
	rec = func(i, from int) {
		if found != "" {
			return
		}
		if i == length {
			for _, s0 := range starts {
				var first State
				have := false
				permutations(seq, func(p []int) {
					if found != "" {
						return
					}
					end, _ := m.run(p, s0)
					if !have {
						first, have = end, true
						return
					}
					if end.packed != first.packed {
						found = fmt.Sprintf("from %s, orderings of %v reach %s and %s", s0, seq, first, end)
					}
				})
			}
			return
		}
		for e := from; e < ne; e++ {
			seq[i] = e
			rec(i+1, e)
		}
	}
	rec(0, 0)
	return found
}

func TestBuildMatchesBruteForce(t *testing.T) {
	machines := 3000
	if testing.Short() {
		machines = 500
	}
	rng := rand.New(rand.NewSource(20261001))
	var certified, rejected, compCertified, readOthers, certifiedReadOthers int
	for id := 0; id < machines; id++ {
		rm := genMachine(rng, id)
		ref := newRef(rm.r)
		if !ref.wfc() {
			continue
		}
		starts := ref.domain()
		div2 := ref.divergence(starts, 2)

		_, rep, err := rm.r.Build()
		if err == nil {
			certified++
			if div2 != "" {
				t.Fatalf("UNSOUND: Build certified %s\n%s\nbut %s", rm.desc, rep, div2)
			}
			if d3 := ref.divergence(starts, 3); d3 != "" {
				t.Fatalf("UNSOUND: Build certified %s\nbut %s", rm.desc, d3)
			}
		} else {
			rejected++
			if div2 == "" {
				t.Fatalf("INCOMPLETE: Build rejected %s (%v)\n%s\nbut no ordering of two events diverges", rm.desc, err, rep)
			}
		}

		if _, _, cerr := rm.r.BuildCompositional(); cerr == nil {
			compCertified++
			if div2 != "" {
				t.Fatalf("UNSOUND: BuildCompositional certified %s\nbut %s", rm.desc, div2)
			}
		}
		if readsOutsideWrites(rm.r) {
			readOthers++
			if err == nil {
				certifiedReadOthers++
			}
		}
	}
	t.Logf("%d machines (WFC holds): Build certified %d, rejected %d; BuildCompositional certified %d; "+
		"%d had an event reading outside its write set (%d of them certified by Build after the exact check)",
		certified+rejected, certified, rejected, compCertified, readOthers, certifiedReadOthers)
	if certified == 0 || rejected == 0 {
		t.Fatalf("generator is degenerate: certified=%d rejected=%d", certified, rejected)
	}
}

// readsOutsideWrites reports whether some event's guard or effect depends on a
// variable outside its write set: changing that one variable, to any value, from
// some state changes the event's result on its write set.
func readsOutsideWrites(r *Registry) bool {
	ref := newRef(r)
	for _, ev := range r.events {
		ws := map[int]bool{}
		for _, w := range ev.writes {
			ws[w] = true
		}
		apply := func(s State) State {
			if ev.guard != nil && !ev.guard(s) {
				return s
			}
			return ev.effect(s)
		}
		for _, s := range ref.states {
			base := apply(s)
			for vi, v := range r.vars {
				if ws[vi] {
					continue
				}
				for d := 0; d < v.domain; d++ {
					t := apply(s.setRaw(v, uint64(d)))
					for _, w := range ev.writes {
						if t.getRaw(r.vars[w]) != base.getRaw(r.vars[w]) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// FuzzBuildSoundness runs the same soundness property on a fuzzer-chosen seed.
// `go test` runs the seed corpus; `go test -fuzz FuzzBuildSoundness` explores more.
func FuzzBuildSoundness(f *testing.F) {
	for _, seed := range []int64{1, 2, 3, 42, 20261001} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		rm := genMachine(rand.New(rand.NewSource(seed)), 0)
		ref := newRef(rm.r)
		if !ref.wfc() {
			return
		}
		starts := ref.domain()
		if _, rep, err := rm.r.Build(); err == nil {
			if d := ref.divergence(starts, 2); d != "" {
				t.Fatalf("UNSOUND: Build certified %s\n%s\nbut %s", rm.desc, rep, d)
			}
		}
		if _, _, err := rm.r.BuildCompositional(); err == nil {
			if d := ref.divergence(starts, 2); d != "" {
				t.Fatalf("UNSOUND: BuildCompositional certified %s\nbut %s", rm.desc, d)
			}
		}
	})
}
