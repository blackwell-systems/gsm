package gsm

import "fmt"

// Footprint-conformance verification.
//
// Compositional certification (and Build's disjointness path) rely on the
// declared footprints being accurate: an event effect writes only its Writes()
// variables and computes them from only its footprint; an invariant repair
// modifies only its Watches() variables and reads only those; an invariant check
// reads only its footprint. If a closure secretly reads or writes another
// variable, disjointness no longer implies commutation and the certificate is
// unsound.
//
// verifyFootprints checks these properties directly, so BuildCompositional
// VERIFIES footprint conformance rather than assuming it. It flips each variable
// outside a closure's footprint and confirms the closure's declared outputs do
// not change and no undeclared variable is written. Cost is polynomial
// (component subspace x variables), never global enumeration.
//
// Soundness note: single-variable flips detect any dependence on, or write to, an
// individual outside variable. A closure engineered to depend only on a joint
// change of two or more outside variables at once (with each single flip masked)
// is not detected; such closures do not arise from ordinary footprint mistakes.

func indexSet(idx []int) map[int]bool {
	m := make(map[int]bool, len(idx))
	for _, i := range idx {
		m[i] = true
	}
	return m
}

// flipVar returns s with variable v set to a value different from its current one
// (domains have size >= 2, so this always changes it).
func (r *Registry) flipVar(s State, v Var) State {
	cur := s.getRaw(v)
	next := (cur + 1) % uint64(v.domain)
	return s.setRaw(v, next)
}

// firstOutsideWrite returns the index of the first variable NOT in writeSet whose
// value differs between s and t, or -1 if the change set is within writeSet.
func (r *Registry) firstOutsideWrite(s, t State, writeSet map[int]bool) int {
	for i, v := range r.vars {
		if writeSet[i] {
			continue
		}
		if s.getRaw(v) != t.getRaw(v) {
			return i
		}
	}
	return -1
}

// writesMatch reports whether s and t agree on every variable in writeSet.
func (r *Registry) writesMatch(s, t State, writeSet map[int]bool) bool {
	for i, v := range r.vars {
		if writeSet[i] && s.getRaw(v) != t.getRaw(v) {
			return false
		}
	}
	return true
}

// verifyFootprints checks that every event effect and invariant repair/check that
// belongs to the component respects its declared footprint.
func (r *Registry) verifyFootprints(c *component) error {
	for _, ei := range c.events {
		ev := r.events[ei]
		ws := indexSet(ev.writes)
		apply := func(s State) State {
			if ev.guard != nil && !ev.guard(s) {
				return s
			}
			return ev.effect(s)
		}
		if err := r.checkTransformFootprint(c, apply, ws, ws, "event", ev.name); err != nil {
			return err
		}
	}
	for _, ii := range c.invariants {
		inv := r.invariants[ii]
		fp := indexSet(inv.footprint)
		// Repair may write only its footprint and depend only on its footprint.
		if err := r.checkTransformFootprint(c, inv.repair, fp, fp, "invariant repair", inv.name); err != nil {
			return err
		}
		// Check must read only its footprint.
		if err := r.checkPredicateFootprint(c, inv.check, fp, inv.name); err != nil {
			return err
		}
	}
	return nil
}

// checkTransformFootprint verifies a state transform fn whose declared writes are
// writeSet and declared footprint is footprint (writeSet is a subset). Over the
// component subspace, and for each variable flipped outside the footprint, it
// confirms fn writes nothing outside writeSet and its writeSet outputs do not
// depend on the outside variable.
func (r *Registry) checkTransformFootprint(c *component, fn func(State) State, writeSet, footprint map[int]bool, kind, name string) error {
	var outErr error
	r.enumComponent(c, func(s State) {
		if outErr != nil {
			return
		}
		base := fn(s)
		if v := r.firstOutsideWrite(s, base, writeSet); v >= 0 {
			outErr = fmt.Errorf("gsm: %s %q writes variable %q outside its declared footprint",
				kind, name, r.vars[v].name)
			return
		}
		for ui, v := range r.vars {
			if footprint[ui] {
				continue
			}
			s2 := r.flipVar(s, v)
			t2 := fn(s2)
			if w := r.firstOutsideWrite(s2, t2, writeSet); w >= 0 {
				outErr = fmt.Errorf("gsm: %s %q writes variable %q outside its declared footprint",
					kind, name, r.vars[w].name)
				return
			}
			// s and s2 agree on the footprint, so a footprint-local fn must produce
			// the same declared-write outputs.
			if !r.writesMatch(base, t2, writeSet) {
				outErr = fmt.Errorf("gsm: %s %q reads variable %q outside its declared footprint",
					kind, name, r.vars[ui].name)
				return
			}
		}
	})
	return outErr
}

// checkPredicateFootprint verifies an invariant's check depends only on its footprint.
func (r *Registry) checkPredicateFootprint(c *component, check CheckFunc, footprint map[int]bool, name string) error {
	var outErr error
	r.enumComponent(c, func(s State) {
		if outErr != nil {
			return
		}
		base := check(s)
		for ui, v := range r.vars {
			if footprint[ui] {
				continue
			}
			if check(r.flipVar(s, v)) != base {
				outErr = fmt.Errorf("gsm: invariant %q check reads variable %q outside its declared footprint",
					name, r.vars[ui].name)
				return
			}
		}
	})
	return outErr
}
