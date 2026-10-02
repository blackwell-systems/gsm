package gsm

import "fmt"

// Footprint-conformance verification.
//
// Compositional certification relies on the declared footprints being accurate:
// an event's guard and effect read only its Writes() variables and the effect
// writes only those; an invariant repair
// modifies only its Watches() variables and reads only those; an invariant check
// reads only its footprint. If a closure secretly reads or writes another
// variable, disjointness no longer implies commutation and the certificate is
// unsound.
//
// verifyFootprints checks these properties directly, so BuildCompositional
// checks footprint conformance rather than assuming it. (Build does not depend on
// footprints at all: it checks every pair exactly over the whole state space.) For every state of the
// component subspace (outside variables at zero) it sets each variable outside a
// closure's footprint to every other value of its domain, one variable at a time,
// and confirms the closure's declared outputs do not change and no undeclared
// variable is written. Cost is polynomial (component subspace x sum of outside
// domain sizes), never global enumeration.
//
// Soundness note (closures): perturbing every single outside variable and every
// pair of outside variables, at every value, from the zero background detects any
// dependence on one or two outside variables. A closure that depends only on a
// joint change of three or more outside variables at once (every single and pair
// change masked, for example a guard `a && b && c` over three outside variables)
// is not detected. For closure rules, footprint conformance is therefore tested,
// not proved. Combinator rules have no such gap: they are checked syntactically.

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

// changedVars names the outside variable(s) a perturbation altered (b is -1 for one).
type changedVars struct{ a, b int }

func (r *Registry) describe(c changedVars) string {
	if c.b < 0 {
		return fmt.Sprintf("%q", r.vars[c.a].name)
	}
	return fmt.Sprintf("%q and %q", r.vars[c.a].name, r.vars[c.b].name)
}

// perturb calls fn with s changed in the variables outside the footprint: every
// other value of each single outside variable, and every combination of other
// values of each pair of outside variables. changed names the variable(s) altered.
// It stops and returns false as soon as fn does.
//
// All values, not one flip: outside variables sit at zero during component
// enumeration, so a single flip would only ever test value 1. Pairs, not only
// singles: a guard such as `paid && inStock` over two outside variables is masked
// by every single change from the zero background (each conjunct stays false).
func (r *Registry) perturb(s State, footprint map[int]bool, fn func(s2 State, changed changedVars) bool) bool {
	var outside []int
	for i := range r.vars {
		if !footprint[i] {
			outside = append(outside, i)
		}
	}
	others := func(v Var, cur uint64) []uint64 {
		out := make([]uint64, 0, v.domain-1)
		for d := 0; d < v.domain; d++ {
			if uint64(d) != cur {
				out = append(out, uint64(d))
			}
		}
		return out
	}
	for a, ia := range outside {
		va := r.vars[ia]
		for _, da := range others(va, s.getRaw(va)) {
			sa := s.setRaw(va, da)
			if !fn(sa, changedVars{ia, -1}) {
				return false
			}
			for _, ib := range outside[a+1:] {
				vb := r.vars[ib]
				for _, db := range others(vb, s.getRaw(vb)) {
					if !fn(sa.setRaw(vb, db), changedVars{ia, ib}) {
						return false
					}
				}
			}
		}
	}
	return true
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
//
// Rules written in the combinator vocabulary are checked syntactically, which is
// exact: an event's read set (its guard's variables and the variables its
// assignments read) must lie inside its write set, and an invariant's footprint is
// derived from every variable its predicate and repair mention, so it is conformant
// by construction. Closure rules are opaque, so they are checked by perturbation
// (see perturb), which is a test rather than a proof (see the soundness note above).
func (r *Registry) verifyFootprints(c *component) error {
	for _, ei := range c.events {
		ev := r.events[ei]
		ws := indexSet(ev.writes)
		if ev.effectAST != nil && (ev.guard == nil || ev.guardAST != nil) {
			reads := ev.effectAST.readVars()
			if ev.guardAST != nil {
				reads = append(reads, ev.guardAST.vars()...)
			}
			for _, vi := range reads {
				if !ws[vi] {
					return fmt.Errorf("gsm: event %q reads variable %q outside its declared footprint",
						ev.name, r.vars[vi].name)
				}
			}
			continue
		}
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
		if inv.predAST != nil && inv.repairAST != nil {
			continue // footprint derived from the AST: conformant by construction
		}
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
		r.perturb(s, footprint, func(s2 State, changed changedVars) bool {
			t2 := fn(s2)
			if w := r.firstOutsideWrite(s2, t2, writeSet); w >= 0 {
				outErr = fmt.Errorf("gsm: %s %q writes variable %q outside its declared footprint",
					kind, name, r.vars[w].name)
				return false
			}
			// s and s2 agree on the footprint, so a footprint-local fn must produce
			// the same declared-write outputs.
			if !r.writesMatch(base, t2, writeSet) {
				outErr = fmt.Errorf("gsm: %s %q reads variable %s outside its declared footprint",
					kind, name, r.describe(changed))
				return false
			}
			return true
		})
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
		r.perturb(s, footprint, func(s2 State, changed changedVars) bool {
			if check(s2) != base {
				outErr = fmt.Errorf("gsm: invariant %q check reads variable %s outside its declared footprint",
					name, r.describe(changed))
				return false
			}
			return true
		})
	})
	return outErr
}
