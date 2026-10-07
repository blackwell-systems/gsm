package gsm_test

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// The differential tests for CheckMigration under declared independence and at-least-once
// delivery: on random small registries, migrations, event maps and declarations, its
// classification must agree with a brute-force enumeration of the delivery sequences the
// class allows, computed from the rule tables (tmodel, migration_differential_test.go), and
// every witness must replay in that model as two related runs.

// xl is a letter of the combined alphabet: an event of A (submitted before the switch) or
// an event of B.
type xl struct {
	old bool
	e   int
}

// declModel is a change with declared pairs: ia and ib are A's and B's declared pairs
// (both orders), allA and allB mean a side declares none (every pair reordered), and cross
// the extra declarations of an A-event in flight with a B-event.
type declModel struct {
	*bfInstance
	allA, allB bool
	ia, ib     map[[2]int]bool
	cross      map[[2]int]bool
}

// indep is the declared relation on the combined alphabet (live_declared_exact's IX).
func (d *declModel) indep(x, y xl) bool {
	switch {
	case x.old && y.old:
		return d.allA || d.ia[[2]int{x.e, y.e}]
	case !x.old && !y.old:
		return d.allB || d.ib[[2]int{x.e, y.e}]
	case !x.old:
		x, y = y, x
	}
	return d.allB || d.ib[[2]int{d.tau[x.e], y.e}] || d.cross[[2]int{x.e, y.e}]
}

// traceKey identifies a sequence's class under the declared relation: two sequences are
// equivalent iff they have the same projection on every pair of dependent letters (the
// projection lemma of trace theory; the projection on {x, x} is x's count).
func (d *declModel) traceKey(w []xl) string {
	letters := map[xl]bool{}
	for _, x := range w {
		letters[x] = true
	}
	var ls []xl
	for x := range letters {
		ls = append(ls, x)
	}
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].old != ls[j].old {
			return ls[i].old
		}
		return ls[i].e < ls[j].e
	})
	var b strings.Builder
	for i, x := range ls {
		for _, y := range ls[i:] {
			if x != y && d.indep(x, y) {
				continue
			}
			fmt.Fprintf(&b, "%v%d/%v%d:", x.old, x.e, y.old, y.e)
			for _, z := range w {
				if z == x || z == y {
					fmt.Fprintf(&b, "%v%d,", z.old, z.e)
				}
			}
			b.WriteByte(';')
		}
	}
	return b.String()
}

// perms returns every distinct ordering of the letters.
func perms(letters []xl) [][]xl {
	var out [][]xl
	used := make([]bool, len(letters))
	cur := make([]xl, 0, len(letters))
	var rec func()
	rec = func() {
		if len(cur) == len(letters) {
			out = append(out, append([]xl(nil), cur...))
			return
		}
		seen := map[xl]bool{}
		for i, x := range letters {
			if used[i] || seen[x] {
				continue
			}
			seen[x] = true
			used[i] = true
			cur = append(cur, x)
			rec()
			cur = cur[:len(cur)-1]
			used[i] = false
		}
	}
	rec()
	return out
}

// declDiverges reports whether two related runs of some multiset of at most ka A-events and
// kb B-events diverge: live, a run is a sequence split anywhere before its first B-event;
// at a barrier, every A-event precedes every B-event and the switch falls between them.
func (d *declModel) declDiverges(barrier bool, s0, ka, kb int) bool {
	multisets := func(ne, k int, old bool) [][]xl {
		out := [][]xl{nil}
		for e := 0; e < ne; e++ {
			var next [][]xl
			for _, m := range out {
				for c := 0; len(m)+c <= k; c++ {
					mm := append([]xl(nil), m...)
					for i := 0; i < c; i++ {
						mm = append(mm, xl{old, e})
					}
					next = append(next, mm)
				}
			}
			out = next
		}
		return out
	}
	runOut := func(w []xl, k int) int {
		s := s0
		for _, x := range w[:k] {
			s = d.A.step(x.e, s)
		}
		y := d.image(s)
		for _, x := range w[k:] {
			e := x.e
			if x.old {
				e = d.tau[e]
			}
			y = d.B.step(e, y)
		}
		return y
	}
	for _, ma := range multisets(len(d.A.eff), ka, true) {
		for _, mb := range multisets(len(d.B.eff), kb, false) {
			classes := map[string]int{}
			record := func(w []xl, out int) bool {
				k := d.traceKey(w)
				if prev, ok := classes[k]; ok && prev != out {
					return true
				}
				classes[k] = out
				return false
			}
			if barrier {
				for _, pa := range perms(ma) {
					for _, pb := range perms(mb) {
						w := append(append([]xl(nil), pa...), pb...)
						if record(w, runOut(w, len(pa))) {
							return true
						}
					}
				}
				continue
			}
			for _, w := range perms(append(append([]xl(nil), ma...), mb...)) {
				for k := 0; k <= len(w); k++ {
					if record(w, runOut(w, k)) {
						return true
					}
					if k < len(w) && !w[k].old {
						break
					}
				}
			}
		}
	}
	return false
}

// letters converts a witness run to the combined alphabet, checking that each in-flight
// event is applied as its translation, and returns the split point (the switch).
func (b *bfInstance) letters(r gsm.MigrationRun) ([]xl, int, error) {
	idx := func(name string, prefix byte, ne int) (int, error) {
		var e int
		if _, err := fmt.Sscanf(name, string(prefix)+"%d", &e); err != nil || e < 0 || e >= ne {
			return 0, fmt.Errorf("unknown event %q", name)
		}
		return e, nil
	}
	var w []xl
	for _, name := range r.Before {
		e, err := idx(name, 'a', len(b.A.eff))
		if err != nil {
			return nil, 0, err
		}
		w = append(w, xl{true, e})
	}
	split := len(w)
	if len(r.InFlightAt) != len(r.InFlight) {
		return nil, 0, fmt.Errorf("%d in-flight events at %d positions", len(r.InFlight), len(r.InFlightAt))
	}
	fly := map[int]int{}
	for k, i := range r.InFlightAt {
		e, err := idx(r.InFlight[k], 'a', len(b.A.eff))
		if err != nil {
			return nil, 0, err
		}
		if i < 0 || i >= len(r.After) || r.After[i] != fmt.Sprintf("b%d", b.tau[e]) {
			return nil, 0, fmt.Errorf("in-flight %s is not applied as its translation at %d", r.InFlight[k], i)
		}
		fly[i] = e
	}
	for i, name := range r.After {
		if e, ok := fly[i]; ok {
			w = append(w, xl{true, e})
			continue
		}
		e, err := idx(name, 'b', len(b.B.eff))
		if err != nil {
			return nil, 0, err
		}
		w = append(w, xl{false, e})
	}
	return w, split, nil
}

// replayX runs a letter sequence split at k from s0.
func (b *bfInstance) replayX(s0 int, w []xl, k int) int {
	s := s0
	for _, x := range w[:k] {
		s = b.A.step(x.e, s)
	}
	y := b.image(s)
	for _, x := range w[k:] {
		e := x.e
		if x.old {
			e = b.tau[e]
		}
		y = b.B.step(e, y)
	}
	return y
}

// support is the set of letters of w.
func support(w []xl) map[xl]bool {
	out := map[xl]bool{}
	for _, x := range w {
		out[x] = true
	}
	return out
}

func sameSupport(u, v []xl) bool {
	su, sv := support(u), support(v)
	if len(su) != len(sv) {
		return false
	}
	for x := range su {
		if !sv[x] {
			return false
		}
	}
	return true
}

// checkClassWitness replays w in the model as two runs related by the class: under
// declared pairs, trace equivalent (rel is the declared model); under at-least-once
// delivery (rel nil), with the same letters. A barrier witness switches after every
// A-event, except, at least once, a redelivery of one applied before the switch.
func (b *bfInstance) checkClassWitness(t *testing.T, where string, w *gsm.MigrationWitness, x, y gsm.Var, rel *declModel,
	barrier bool) (n1, n2 []xl) {
	t.Helper()
	if w == nil {
		t.Fatalf("%s: no witness", where)
	}
	s0 := w.Start.GetInt(x)
	w1, k1, err1 := b.letters(w.Run1)
	w2, k2, err2 := b.letters(w.Run2)
	if err1 != nil || err2 != nil {
		t.Fatalf("%s: witness does not convert: %v %v", where, err1, err2)
	}
	r1, r2 := b.replayX(s0, w1, k1), b.replayX(s0, w2, k2)
	if r1 != w.Result1.GetInt(y) || r2 != w.Result2.GetInt(y) || r1 == r2 {
		t.Fatalf("%s: witness replays to %d and %d, reported %s and %s", where, r1, r2, w.Result1, w.Result2)
	}
	if rel != nil {
		if rel.traceKey(w1) != rel.traceKey(w2) {
			t.Fatalf("%s: the witness runs are not related by declared swaps: %+v %+v", where, w.Run1, w.Run2)
		}
	} else if !sameSupport(w1, w2) {
		t.Fatalf("%s: the witness runs deliver different events: %+v %+v", where, w.Run1, w.Run2)
	}
	if barrier {
		for _, r := range []gsm.MigrationRun{w.Run1, w.Run2} {
			before := map[string]bool{}
			for _, e := range r.Before {
				before[e] = true
			}
			for _, e := range r.InFlight {
				if rel != nil || !before[e] {
					t.Fatalf("%s: a barrier witness has an event in flight: %+v %+v", where, w.Run1, w.Run2)
				}
			}
		}
	}
	return w1, w2
}

// aloMaxLen bounds the deliveries of a run of n messages in aloDiverges.
func aloMaxLen(n int) int { return min(n+2, 5) }

// aloDiverges reports whether two runs delivering one set of messages diverge: at most mA
// messages of A and mB of B (each an event, an event possibly submitted twice), each
// delivered at least once and at most aloMaxLen deliveries in all. Live, the switch falls
// anywhere before the first B-message; at a barrier, after every A-message was delivered,
// with redeliveries of A-messages allowed after it.
func (b *bfInstance) aloDiverges(barrier bool, s0, mA, mB int) bool {
	names := func(ne, k int) [][]int {
		out := [][]int{nil}
		var rec func(from int, cur []int)
		rec = func(from int, cur []int) {
			if len(cur) == k {
				return
			}
			for e := from; e < ne; e++ {
				next := append(append([]int(nil), cur...), e)
				out = append(out, next)
				rec(e, next)
			}
		}
		rec(0, nil)
		return out
	}
	type key struct {
		inB      bool
		s        int
		used, ln int
	}
	for _, na := range names(len(b.A.eff), mA) {
		for _, nb := range names(len(b.B.eff), mB) {
			var msgs []xl
			for _, e := range na {
				msgs = append(msgs, xl{true, e})
			}
			for _, e := range nb {
				msgs = append(msgs, xl{false, e})
			}
			all := 1<<uint(len(msgs)) - 1
			allA := 1<<uint(len(na)) - 1
			maxLen := aloMaxLen(len(msgs))
			memo := map[key]uint64{}
			var finals func(k key) uint64
			finals = func(k key) uint64 {
				if v, ok := memo[k]; ok {
					return v
				}
				var out uint64
				if k.inB && k.used == all {
					out |= 1 << uint(k.s)
				}
				if !k.inB && (!barrier || k.used&allA == allA) {
					out |= finals(key{inB: true, s: b.image(k.s), used: k.used, ln: k.ln})
				}
				if k.ln < maxLen {
					for i, m := range msgs {
						switch {
						case m.old && !k.inB:
							out |= finals(key{false, b.A.step(m.e, k.s), k.used | 1<<uint(i), k.ln + 1})
						case m.old:
							out |= finals(key{true, b.B.step(b.tau[m.e], k.s), k.used | 1<<uint(i), k.ln + 1})
						case k.inB:
							out |= finals(key{true, b.B.step(m.e, k.s), k.used | 1<<uint(i), k.ln + 1})
						}
					}
				}
				memo[k] = out
				return out
			}
			if v := finals(key{s: s0}); v&(v-1) != 0 {
				return true
			}
		}
	}
	return false
}

// randDeclared returns a random declaration for r's n events: none (all), or a random set
// of pairs, recorded in both orders.
func randDeclared(rng *rand.Rand, r *gsm.Registry, prefix string, n int) (all bool, pairs map[[2]int]bool) {
	pairs = map[[2]int]bool{}
	if rng.Intn(3) == 0 {
		return true, pairs
	}
	r.OnlyDeclaredPairs()
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if rng.Intn(2) == 0 {
				r.Independent(fmt.Sprintf("%s%d", prefix, i), fmt.Sprintf("%s%d", prefix, j))
				pairs[[2]int{i, j}], pairs[[2]int{j, i}] = true, true
			}
		}
	}
	return false, pairs
}

// randChange returns a random change from A to B with its registries and migration.
func randChange(rng *rand.Rand) (*bfInstance, *gsm.Registry, *gsm.Registry, gsm.Var, gsm.Var, gsm.Migration, map[string]string) {
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
	return inst, ra, rb, x, y, mig, events
}

func TestCheckMigration_DeclaredDifferentialRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(25))
	trials := 800
	if testing.Short() {
		trials = 300
	}
	const ka, kb = 3, 2
	seen := map[gsm.MigrationOutcome]int{}
	closureCertified, crossFailures := 0, 0
	for trial := 0; trial < trials; trial++ {
		inst, ra, rb, x, y, mig, events := randChange(rng)
		d := &declModel{bfInstance: inst, cross: map[[2]int]bool{}}
		d.allA, d.ia = randDeclared(rng, ra, "a", len(inst.A.eff))
		d.allB, d.ib = randDeclared(rng, rb, "b", len(inst.B.eff))
		var opts []gsm.MigrationOption
		for a := range inst.A.eff {
			for b := range inst.B.eff {
				if rng.Intn(5) == 0 {
					d.cross[[2]int{a, b}] = true
					opts = append(opts, gsm.MigrationInFlightIndependent(fmt.Sprintf("a%d", a), fmt.Sprintf("b%d", b)))
				}
			}
		}
		where := fmt.Sprintf("trial %d", trial)
		rep, err := gsm.CheckMigration(ra, rb, mig, events, opts...)
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		seen[rep.Outcome]++
		if rep.Declared != (!d.allA || !d.allB) || rep.Outcome == gsm.MigrationUnknown {
			t.Fatalf("%s: declared %v, outcome %v", where, rep.Declared, rep.Outcome)
		}
		liveDiv := d.declDiverges(false, 0, ka, kb)
		barDiv := d.declDiverges(true, 0, ka, kb)
		ctx := func() string {
			return fmt.Sprintf("%s (A %+v, B %+v, mig %v, tau %v, allA %v %v, allB %v %v, cross %v)\n%s", where, inst.A,
				inst.B, inst.mig, inst.tau, d.allA, d.ia, d.allB, d.ib, d.cross, rep)
		}
		switch rep.Outcome {
		case gsm.MigrationSafeOnline:
			if liveDiv {
				t.Fatalf("SAFE ONLINE, but two related live runs diverge: %s", ctx())
			}
		case gsm.MigrationSafeBehindBarrier:
			if barDiv {
				t.Fatalf("SAFE BEHIND A BARRIER, but two related barrier runs diverge: %s", ctx())
			}
			if c, ok := rep.Condition(gsm.MigrationAmodM); ok && c.Holds {
				closureCertified++
			}
		case gsm.MigrationUnsafe:
			w1, _ := inst.checkClassWitness(t, where, rep.BarrierWitness, x, y, d, true)
			if na, nb := countOld(w1); na <= ka && nb <= kb && !barDiv {
				t.Fatalf("the barrier witness fits the brute force's bound, which finds no divergence: %s", ctx())
			}
		}
		if rep.Outcome != gsm.MigrationSafeOnline {
			w1, _ := inst.checkClassWitness(t, where, rep.LiveWitness, x, y, d, false)
			if na, nb := countOld(w1); na <= ka && nb <= kb && !liveDiv {
				t.Fatalf("the live witness fits the brute force's bound, which finds no divergence: %s", ctx())
			}
			if rep.Failed == gsm.MigrationPermBStart && rep.Outcome == gsm.MigrationSafeBehindBarrier &&
				len(rep.LiveWitness.Run1.InFlight) > 0 {
				crossFailures++
			}
		}
		if barDiv && rep.Outcome != gsm.MigrationUnsafe {
			t.Fatalf("two related barrier runs diverge, outcome %v: %s", rep.Outcome, ctx())
		}
	}
	t.Logf("outcomes over %d trials: %v; %d barrier outcomes certified by the declared closure search; %d not online "+
		"by a declared pair with an event in flight", trials, seen, closureCertified, crossFailures)
	for _, o := range []gsm.MigrationOutcome{gsm.MigrationSafeOnline, gsm.MigrationSafeBehindBarrier, gsm.MigrationUnsafe} {
		if seen[o] == 0 {
			t.Errorf("no trial produced %v", o)
		}
	}
	if closureCertified == 0 || crossFailures == 0 {
		t.Errorf("closure certificates %d, in-flight declared-pair failures %d: want both", closureCertified, crossFailures)
	}
}

// countOld counts the A-letters and B-letters of w.
func countOld(w []xl) (na, nb int) {
	for _, x := range w {
		if x.old {
			na++
		} else {
			nb++
		}
	}
	return na, nb
}

func TestCheckMigration_AtLeastOnceDifferentialRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(26))
	trials := 1500
	if testing.Short() {
		trials = 400
	}
	const mA, mB = 2, 2
	seen := map[gsm.MigrationOutcome]int{}
	failed := map[string]int{}
	for trial := 0; trial < trials; trial++ {
		inst, ra, rb, x, y, mig, events := randChange(rng)
		where := fmt.Sprintf("trial %d", trial)
		rep, err := gsm.CheckMigration(ra, rb, mig, events, gsm.MigrationDeliveryClass(gsm.MigrationAtLeastOnce))
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		seen[rep.Outcome]++
		failed[rep.Failed]++
		if rep.Delivery != gsm.MigrationAtLeastOnce || rep.Outcome == gsm.MigrationUnknown {
			t.Fatalf("%s: delivery %v, outcome %v", where, rep.Delivery, rep.Outcome)
		}
		liveDiv := inst.aloDiverges(false, 0, mA, mB)
		barDiv := inst.aloDiverges(true, 0, mA, mB)
		ctx := func() string {
			return fmt.Sprintf("%s (A %+v, B %+v, mig %v, tau %v)\n%s", where, inst.A, inst.B, inst.mig, inst.tau, rep)
		}
		fits := func(w1, w2 []xl) bool {
			na, nb := countOld(keys(support(w1)))
			n := na + nb
			return na <= mA && nb <= mB && len(w1) <= aloMaxLen(n) && len(w2) <= aloMaxLen(n)
		}
		switch rep.Outcome {
		case gsm.MigrationSafeOnline:
			if liveDiv {
				t.Fatalf("SAFE ONLINE, but two live runs with the same messages diverge: %s", ctx())
			}
		case gsm.MigrationSafeBehindBarrier:
			if barDiv {
				t.Fatalf("SAFE BEHIND A BARRIER, but two barrier runs with the same messages diverge: %s", ctx())
			}
		case gsm.MigrationUnsafe:
			w1, w2 := inst.checkClassWitness(t, where, rep.BarrierWitness, x, y, nil, true)
			if fits(w1, w2) && !barDiv {
				t.Fatalf("the barrier witness fits the brute force's bound, which finds no divergence: %s", ctx())
			}
		}
		if rep.Outcome != gsm.MigrationSafeOnline {
			w1, w2 := inst.checkClassWitness(t, where, rep.LiveWitness, x, y, nil, false)
			if fits(w1, w2) && !liveDiv {
				t.Fatalf("the live witness fits the brute force's bound, which finds no divergence: %s", ctx())
			}
		}
		if barDiv && rep.Outcome != gsm.MigrationUnsafe {
			t.Fatalf("two barrier runs with the same messages diverge, outcome %v: %s", rep.Outcome, ctx())
		}
	}
	t.Logf("outcomes over %d trials: %v; deciding conditions %v", trials, seen, failed)
	for _, o := range []gsm.MigrationOutcome{gsm.MigrationSafeOnline, gsm.MigrationSafeBehindBarrier, gsm.MigrationUnsafe} {
		if seen[o] == 0 {
			t.Errorf("no trial produced %v", o)
		}
	}
	for _, k := range []string{gsm.MigrationIdemEvery, gsm.MigrationAbsorbS, gsm.MigrationAmodA} {
		if failed[k] == 0 {
			t.Errorf("no trial was unsafe by %s", k)
		}
	}
}

func keys(m map[xl]bool) []xl {
	var out []xl
	for x := range m {
		out = append(out, x)
	}
	return out
}
