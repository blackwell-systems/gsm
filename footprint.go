package gsm

import "fmt"

// Footprint-conformance verification.
//
// Compositional certification relies on the footprints being accurate. A combinator
// rule's footprint is derived from its expression trees, reads included, so it is
// accurate by construction (normalization-confluence ast_event_local, ast_check_local,
// ast_repair_local). A closure's footprint is its declaration: an event's guard and effect
// read only its Writes() variables and the effect writes only those; an invariant repair
// modifies only its Watches() variables and reads only those; an invariant check reads
// only its footprint. If a closure secretly reads or writes another variable,
// disjointness no longer implies commutation and the certificate is unsound
// (ws_diverges, rc_diverges).
//
// verifyFootprints checks these properties for closures, so BuildCompositional with
// TrustClosureFootprints tests footprint conformance rather than assuming it. (Build's
// global check does not depend on footprints at all: it checks every pair exactly over the
// whole state space; Build's per-component path accepts combinator rules only.) For every state of the
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
// BuildCompositional therefore accepts closure rules only with the
// TrustClosureFootprints option, and reports the result as
// AssuranceOracleComponentsTested.

// syntactic reports whether the event's footprint is checked exactly from its
// combinator trees (effect, and guard if any), rather than tested by perturbation.
func (ev eventDef) syntactic() bool {
	return ev.effectAST != nil && (ev.guard == nil || ev.guardAST != nil)
}

// syntactic reports whether the invariant's footprint is derived from its combinator
// trees (conformant by construction), rather than tested by perturbation.
func (inv invariantDef) syntactic() bool {
	return inv.predAST != nil && inv.repairAST != nil
}

// maxPerturbationCalls caps the closure calls the footprint test of one closure rule
// may make. The test runs the closure once per component state for every single and
// pair change of the outside variables, so its cost is the component's state count
// times (1 + S + P), where S sums (domain - 1) over the outside variables and P sums
// the products of that over pairs of them. A wide outside variable (an Int over
// millions of values) makes that astronomically large; the per-component check returns
// an error up front instead of running for hours.
const maxPerturbationCalls = 1 << 28

// perturbationCalls estimates the closure calls perturbing outside a footprint
// costs per component state (1 + S + P, see maxPerturbationCalls), in float64 so a
// huge domain cannot overflow. widest names the widest outside variable.
func (r *Registry) perturbationCalls(footprint map[int]bool) (calls float64, widest int) {
	var sum, sumSq float64
	widest = -1
	for i, v := range r.vars {
		if footprint[i] {
			continue
		}
		d := float64(v.domain - 1)
		sum += d
		sumSq += d * d
		if widest < 0 || v.domain > r.vars[widest].domain {
			widest = i
		}
	}
	return 1 + sum + (sum*sum-sumSq)/2, widest
}

// checkPerturbationCost returns an error, before any closure runs, when testing a
// closure rule's footprint in component c (count states) would take more than
// maxPerturbationCalls closure calls. Combinator rules are checked syntactically and
// cost nothing here.
func (r *Registry) checkPerturbationCost(c *component, count int) error {
	check := func(kind, name string, footprint []int, perCall float64) error {
		calls, widest := r.perturbationCalls(indexSet(footprint))
		total := float64(count) * calls * perCall
		if total <= maxPerturbationCalls {
			return nil
		}
		return fmt.Errorf("gsm: testing the footprint of closure %s %q would take about %.3g closure calls "+
			"(more than %d): its component has %d states and the widest variable outside its footprint, %q, "+
			"has %d values. Write the rule with combinators (checked exactly, no perturbation), narrow the "+
			"domains, or use Build", kind, name, total, maxPerturbationCalls, count,
			r.vars[widest].name, r.vars[widest].domain)
	}
	for _, ei := range c.events {
		if ev := r.events[ei]; !ev.syntactic() {
			if err := check("event", ev.name, ev.writes, 1); err != nil {
				return err
			}
		}
	}
	for _, ii := range c.invariants {
		if inv := r.invariants[ii]; !inv.syntactic() {
			// The repair and the check are each tested.
			if err := check("invariant", inv.name, inv.footprint, 2); err != nil {
				return err
			}
		}
	}
	return nil
}

func indexSet(idx []int) map[int]bool {
	m := make(map[int]bool, len(idx))
	for _, i := range idx {
		m[i] = true
	}
	return m
}

// changedVars names the outside variable(s) a perturbation altered (b is -1 for one).
type changedVars struct{ a, b int }

func (r *Registry) describe(c changedVars) string {
	if c.b < 0 {
		return fmt.Sprintf("%q", r.vars[c.a].name)
	}
	return fmt.Sprintf("%q and %q", r.vars[c.a].name, r.vars[c.b].name)
}

// perturber.run calls fn with s changed in the variables outside the footprint: every
// other value of each single outside variable, and every combination of other
// values of each pair of outside variables. changed names the variable(s) altered.
// It stops and returns false as soon as fn does.
//
// All values, not one flip: outside variables sit at zero during component
// enumeration, so a single flip would only ever test value 1. Pairs, not only
// singles: a guard such as `paid && inStock` over two outside variables is masked
// by every single change from the zero background (each conjunct stays false).
type perturber struct {
	r       *Registry
	outside []int
}

func (r *Registry) newPerturber(footprint map[int]bool) perturber {
	p := perturber{r: r}
	for i := range r.vars {
		if !footprint[i] {
			p.outside = append(p.outside, i)
		}
	}
	return p
}

func (p perturber) run(s State, fn func(s2 State, changed changedVars) bool) bool {
	vars := p.r.vars
	for a, ia := range p.outside {
		va := vars[ia]
		ca := s.getRaw(va)
		for da := uint64(0); da < uint64(va.domain); da++ {
			if da == ca {
				continue
			}
			sa := s.setRaw(va, da)
			if !fn(sa, changedVars{ia, -1}) {
				return false
			}
			for _, ib := range p.outside[a+1:] {
				vb := vars[ib]
				cb := s.getRaw(vb)
				for db := uint64(0); db < uint64(vb.domain); db++ {
					if db == cb {
						continue
					}
					if !fn(sa.setRaw(vb, db), changedVars{ia, ib}) {
						return false
					}
				}
			}
		}
	}
	return true
}

// fieldMask returns the packed-state bits that hold the variables in set.
func (r *Registry) fieldMask(set map[int]bool) uint64 {
	var m uint64
	for i := range set {
		v := r.vars[i]
		m |= uint64((1<<v.bits)-1) << v.offset
	}
	return m
}

// firstOutsideWrite returns the index of the first variable NOT in writeSet whose
// value differs between s and t, or -1 if the change set is within writeSet.
// writeMask is fieldMask(writeSet); the common no-difference case is one XOR.
func (r *Registry) firstOutsideWrite(s, t State, writeSet map[int]bool, writeMask uint64) int {
	if (s.packed^t.packed)&^writeMask == 0 {
		return -1
	}
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

// writesMatch reports whether s and t agree on every variable in the write set
// whose bits are writeMask.
func writesMatch(s, t State, writeMask uint64) bool {
	return (s.packed^t.packed)&writeMask == 0
}

// verifyFootprints checks that every event effect and invariant repair/check that
// belongs to the component respects its footprint.
//
// Rules written in the combinator vocabulary need no test: their footprints are derived
// from the expression trees (an event's guard reads, effect reads and writes; an
// invariant's check reads and repair reads and writes), which is exact, and the
// components are built from those footprints, so every variable a combinator rule reads
// or writes lies in its component (checked here once more). Closure rules are opaque, so
// they are checked by perturbation against their declared footprint (see perturber),
// which is a test rather than a proof (see the soundness note above). Every event's
// effect and every repair also runs on every state of its component when the per-component
// check builds the component's tables, where each result is checked to be a state of the
// machine.
func (r *Registry) verifyFootprints(c *component) error {
	run := r.checked()
	in := indexSet(c.vars)
	for _, ei := range c.events {
		ev := r.events[ei]
		ws := indexSet(ev.writes)
		if ev.syntactic() {
			fp, _ := r.eventFootprint(ev)
			for _, vi := range fp {
				if !in[vi] {
					return fmt.Errorf("gsm: event %q reads or writes variable %q outside its footprint component",
						ev.name, r.vars[vi].name)
				}
			}
			continue
		}
		apply := func(s State) (State, error) { return run.applyEvent(ev, s) }
		if err := r.checkTransformFootprint(c, apply, ws, ws, "event", ev.name); err != nil {
			return err
		}
	}
	for _, ii := range c.invariants {
		inv := r.invariants[ii]
		if inv.syntactic() {
			fp, _ := r.invariantFootprint(inv)
			for _, vi := range fp {
				if !in[vi] {
					return fmt.Errorf("gsm: invariant %q reads or writes variable %q outside its footprint component",
						inv.name, r.vars[vi].name)
				}
			}
			continue
		}
		fp := indexSet(inv.footprint)
		// Repair may write only its footprint and depend only on its footprint.
		repair := func(s State) (State, error) { return run.repair(inv, s) }
		if err := r.checkTransformFootprint(c, repair, fp, fp, "invariant repair", inv.name); err != nil {
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
// component subspace, and for every change of one or two variables outside the footprint, it
// confirms fn writes nothing outside writeSet and its writeSet outputs do not
// depend on the outside variable. fn returns an error for a result that is not a
// state of the machine (checkedRules), which stops the check.
func (r *Registry) checkTransformFootprint(c *component, fn func(State) (State, error), writeSet, footprint map[int]bool, kind, name string) error {
	var outErr error
	wm := r.fieldMask(writeSet)
	pt := r.newPerturber(footprint)
	r.enumComponent(c, func(s State) {
		if outErr != nil {
			return
		}
		base, err := fn(s)
		if err != nil {
			outErr = err
			return
		}
		if v := r.firstOutsideWrite(s, base, writeSet, wm); v >= 0 {
			outErr = fmt.Errorf("gsm: %s %q writes variable %q outside its declared footprint",
				kind, name, r.vars[v].name)
			return
		}
		pt.run(s, func(s2 State, changed changedVars) bool {
			t2, err := fn(s2)
			if err != nil {
				outErr = err
				return false
			}
			if w := r.firstOutsideWrite(s2, t2, writeSet, wm); w >= 0 {
				outErr = fmt.Errorf("gsm: %s %q writes variable %q outside its declared footprint",
					kind, name, r.vars[w].name)
				return false
			}
			// s and s2 agree on the footprint, so a footprint-local fn must produce
			// the same declared-write outputs.
			if !writesMatch(base, t2, wm) {
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
	pt := r.newPerturber(footprint)
	r.enumComponent(c, func(s State) {
		if outErr != nil {
			return
		}
		base := check(s)
		pt.run(s, func(s2 State, changed changedVars) bool {
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
