package gsm

import (
	"fmt"
	"sort"
	"strings"
)

// maxSynthNodes bounds the backtracking search. If the search hits this budget before
// completing, Synthesis reports Exhaustive=false (undetermined) rather than a false verdict.
const maxSynthNodes = 20_000_000

// Synthesis is the result of Registry.Synthesize.
type Synthesis struct {
	Convergent bool // a convergent compensation was found
	Exhaustive bool // the search completed; !Convergent && Exhaustive ⇒ provably impossible
	Nodes      int  // backtracking nodes explored (transparency)

	r        *Registry
	nf       []uint64 // representative convergent normal-form table (nil if none)
	step     [][]uint64
	invalids []uint64 // packed invalid states (for Repairs())
	witness  string   // when impossible: why (a critical pair no repair can reconcile)
}

// SynthOption configures synthesis. See Prefer.
type SynthOption func(*synthConfig)

type synthConfig struct {
	cost func(from, to State) int // per-repair cost for candidate ordering (nil → minimal-change)
}

// Prefer biases synthesis toward the repairs you want. cost(from, to) scores repairing the
// invalid state `from` to the valid target `to` — lower is more preferred. Synthesis tries
// lower-cost targets first, so the representative compensation is the least-costly convergent
// one it finds (an ordering bias, not a guaranteed global optimum). It only chooses AMONG
// convergent repairs; it never makes a non-convergent repair convergent. Without Prefer,
// synthesis defaults to minimal-change (fewest variables altered).
func Prefer(cost func(from, to State) int) SynthOption {
	return func(c *synthConfig) { c.cost = cost }
}

// Synthesize searches for a compensation (a normal-form map on invalid states) that makes the
// registry converge — satisfying WFC and CC — from the invariants' validity predicates and
// the events alone, defaulting to a least-invasive (minimal-change) repair. See SynthesizeWith
// to steer the choice with a preference.
func (r *Registry) Synthesize() (*Synthesis, error) { return r.SynthesizeWith() }

// SynthesizeWith is Synthesize with options (see Prefer). Any Repair functions on the
// invariants are IGNORED: the point is to generate one. It returns a representative convergent
// compensation if one exists (a ready-to-use Machine and inspectable Repairs), proves
// impossibility when the search is exhaustive and finds nothing (with a Witness where
// available), or reports Exhaustive=false when it hit its search budget without a verdict.
//
// Convergent does not mean desirable: a repair merely makes orderings agree. The default
// ordering prefers minimal-change repairs; use Prefer to encode a domain policy. If the only
// convergent repairs are unacceptable, the events — not the compensation — need redesign.
//
// The search is backtracking with forward-checking (pure Go, no solver dependency); it prunes
// the assignment tree but is worst-case exponential (CC synthesis is NP-hard). A SAT/SMT
// encoding would push the ceiling further.
func (r *Registry) SynthesizeWith(opts ...SynthOption) (*Synthesis, error) {
	var cfg synthConfig
	for _, o := range opts {
		o(&cfg)
	}
	if r.totalBits > 20 {
		return nil, fmt.Errorf("gsm: state space too large (%d bits, max 20)", r.totalBits)
	}
	packedCount := 1 << r.totalBits
	mk := func(id int) State { return State{packed: uint64(id), vars: r.vars} }

	// Partition encodable states into valid and invalid.
	validEnc := make([]bool, packedCount)
	isValidState := make([]bool, packedCount)
	var valids, invalids []int
	for s := 0; s < packedCount; s++ {
		if !r.isValidEncoding(uint64(s)) {
			continue
		}
		validEnc[s] = true
		if r.allInvariantsHold(mk(s)) {
			isValidState[s] = true
			valids = append(valids, s)
		} else {
			invalids = append(invalids, s)
		}
	}
	if len(valids) == 0 {
		return nil, fmt.Errorf("gsm: no valid states — invariants are unsatisfiable")
	}

	// Precompute the raw post-event state for every (event, encodable state) — independent of
	// the candidate compensation. clampState keeps results within valid encodings.
	rawStep := make([][]uint64, len(r.events))
	for ei, ev := range r.events {
		rawStep[ei] = make([]uint64, packedCount)
		for s := 0; s < packedCount; s++ {
			if validEnc[s] {
				rawStep[ei][s] = r.clampState(r.applyEvent(ev, mk(s))).packed
			}
		}
	}
	pairs := r.ccPairs()

	// Candidate ordering: try lower-cost repair targets first, so backtracking's first solution
	// is the least-costly convergent one it finds. Default cost is minimal-change (fewest
	// variables altered); Prefer overrides it with a domain policy.
	cost := cfg.cost
	if cost == nil {
		cost = func(from, to State) int {
			d := 0
			for _, v := range r.vars {
				if from.getRaw(v) != to.getRaw(v) {
					d++
				}
			}
			return d
		}
	}
	targets := make([][]int, len(invalids))
	for k, inv := range invalids {
		invState := mk(inv)
		ts := append([]int(nil), valids...)
		costOf := make(map[int]int, len(ts))
		for _, t := range ts {
			costOf[t] = cost(invState, mk(t))
		}
		sort.Slice(ts, func(a, b int) bool {
			if costOf[ts[a]] != costOf[ts[b]] {
				return costOf[ts[a]] < costOf[ts[b]]
			}
			return ts[a] < ts[b]
		})
		targets[k] = ts
	}

	// nf starts as identity; valid and non-encoding states are permanently "assigned".
	nf := make([]uint64, packedCount)
	assigned := make([]bool, packedCount)
	for s := 0; s < packedCount; s++ {
		nf[s] = uint64(s)
		assigned[s] = validEnc[s] && isValidState[s] || !validEnc[s]
	}

	// Order the decision variables most-constrained-first is a nice-to-have; index order is
	// fine and deterministic. Backtracking with forward-checking prunes the tree.
	nodes := 0
	budgetHit := false
	var solution []uint64
	var bt func(k int) bool
	bt = func(k int) bool {
		if nodes >= maxSynthNodes {
			budgetHit = true
			return false
		}
		nodes++
		if k == len(invalids) {
			if ccHolds(nf, rawStep, validEnc, isValidState, pairs) {
				solution = append([]uint64(nil), nf...)
				return true
			}
			return false
		}
		inv := invalids[k]
		for _, target := range targets[k] {
			nf[inv] = uint64(target)
			assigned[inv] = true
			if forwardCheck(nf, assigned, rawStep, validEnc, isValidState, pairs) {
				if bt(k + 1) {
					return true
				}
			}
			if budgetHit {
				break
			}
		}
		nf[inv] = uint64(inv)
		assigned[inv] = false
		return false
	}
	found := bt(0)

	out := &Synthesis{
		Convergent: found,
		Exhaustive: !budgetHit,
		Nodes:      nodes,
		r:          r,
	}
	if !found && !budgetHit {
		out.witness = r.impossibilityWitness(rawStep, isValidState, pairs)
	}
	for _, s := range invalids {
		out.invalids = append(out.invalids, uint64(s))
	}
	if found {
		out.nf = solution
		out.step = make([][]uint64, len(r.events))
		for ei := range r.events {
			out.step[ei] = make([]uint64, packedCount)
			for s := 0; s < packedCount; s++ {
				if validEnc[s] {
					out.step[ei][s] = solution[rawStep[ei][s]]
				}
			}
		}
	}
	return out, nil
}

// ccPairs returns the event index pairs to check for CC1, honoring Independent() declarations.
func (r *Registry) ccPairs() [][2]int {
	var pairs [][2]int
	if r.allIndependent {
		for i := 0; i < len(r.events); i++ {
			for j := i + 1; j < len(r.events); j++ {
				pairs = append(pairs, [2]int{i, j})
			}
		}
		return pairs
	}
	for _, p := range r.independent {
		i, j := p[0], p[1]
		if i > j {
			i, j = j, i
		}
		pairs = append(pairs, [2]int{i, j})
	}
	return pairs
}

// ccHolds checks CC1 (order independence, all encodable states) and CC2 (compensation
// absorption, invalid states) for a complete normal-form map.
func ccHolds(nf []uint64, rawStep [][]uint64, validEnc, isValidState []bool, pairs [][2]int) bool {
	step := func(e int, s uint64) uint64 { return nf[rawStep[e][s]] }
	for s := 0; s < len(validEnc); s++ {
		if !validEnc[s] {
			continue
		}
		u := uint64(s)
		for _, p := range pairs {
			if step(p[1], step(p[0], u)) != step(p[0], step(p[1], u)) {
				return false
			}
		}
		if !isValidState[s] {
			for e := range rawStep {
				if step(e, u) != nf[rawStep[e][nf[u]]] {
					return false
				}
			}
		}
	}
	return true
}

// forwardCheck prunes: it fails a partial assignment if any CC constraint whose referenced
// nf lookups are ALL assigned is already violated. Constraints touching an unassigned nf
// entry are skipped (not yet determined).
func forwardCheck(nf []uint64, assigned []bool, rawStep [][]uint64, validEnc, isValidState []bool, pairs [][2]int) bool {
	// eval returns (nf[x], ready): ready is false if nf[x] is not yet assigned.
	eval := func(x uint64) (uint64, bool) {
		if !assigned[x] {
			return 0, false
		}
		return nf[x], true
	}
	step := func(e int, s uint64) (uint64, bool) { return eval(rawStep[e][s]) }
	for s := 0; s < len(validEnc); s++ {
		if !validEnc[s] {
			continue
		}
		u := uint64(s)
		for _, p := range pairs {
			a, ra := step(p[0], u)
			b, rb := step(p[1], u)
			if !ra || !rb {
				continue
			}
			lhs, rl := step(p[1], a)
			rhs, rr := step(p[0], b)
			if rl && rr && lhs != rhs {
				return false
			}
		}
		if !isValidState[s] {
			nu, rnu := eval(u)
			if !rnu {
				continue
			}
			for e := range rawStep {
				l, rlok := step(e, u)
				rv, rrok := step(e, nu)
				if rlok && rrok && l != rv {
					return false
				}
			}
		}
	}
	return true
}

// impossibilityWitness looks for a critical pair that no compensation can reconcile: two
// independent events that, from a valid state, both reach already-VALID states which then
// diverge. Since compensation is the identity on valid states, no repair can close this — a
// concrete, actionable reason to redesign the events. Returns "" if no such witness exists
// (the impossibility is subtler than a valid critical pair).
func (r *Registry) impossibilityWitness(rawStep [][]uint64, isValidState []bool, pairs [][2]int) string {
	names := make([]string, len(r.events))
	for i, ev := range r.events {
		names[i] = ev.name
	}
	for s := 0; s < len(isValidState); s++ {
		if !isValidState[s] {
			continue
		}
		u := uint64(s)
		for _, p := range pairs {
			a, b := rawStep[p[0]][u], rawStep[p[1]][u]
			if !isValidState[a] || !isValidState[b] {
				continue
			}
			c, d := rawStep[p[1]][a], rawStep[p[0]][b]
			if isValidState[c] && isValidState[d] && c != d {
				return fmt.Sprintf("from %s, events %q and %q reach distinct valid states %s vs %s — "+
					"both already valid, so no compensation can reconcile them",
					State{packed: u, vars: r.vars}, names[p[0]], names[p[1]],
					State{packed: c, vars: r.vars}, State{packed: d, vars: r.vars})
			}
		}
	}
	return ""
}

// Machine returns a ready-to-use Machine built from the synthesized compensation, or nil if
// no convergent compensation was found.
func (s *Synthesis) Machine() *Machine {
	if !s.Convergent {
		return nil
	}
	m := &Machine{name: s.r.name, vars: s.r.vars, events: make(map[string]int), step: s.step, nf: s.nf}
	for i, ev := range s.r.events {
		m.events[ev.name] = i
	}
	return m
}

// Repairs returns the synthesized repair for each invalid state as sorted {invalid, target}
// pairs, for inspection. Empty if no convergent compensation was found. (State carries a
// variable list and is not comparable, so this is a slice, not a map.)
func (s *Synthesis) Repairs() [][2]State {
	var out [][2]State
	if !s.Convergent {
		return out
	}
	invs := append([]uint64(nil), s.invalids...)
	sort.Slice(invs, func(i, j int) bool { return invs[i] < invs[j] })
	for _, inv := range invs {
		out = append(out, [2]State{{packed: inv, vars: s.r.vars}, {packed: s.nf[inv], vars: s.r.vars}})
	}
	return out
}

// Witness returns, for a provably-impossible synthesis, a concrete critical pair that no
// compensation can reconcile — or "" if none was found or the synthesis is not impossible.
func (s *Synthesis) Witness() string { return s.witness }

// String renders a human-readable synthesis report.
func (s *Synthesis) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Synthesis: %s (%d nodes)\n", s.r.name, s.Nodes)
	switch {
	case s.Convergent:
		fmt.Fprintf(&b, "  CONVERGENT — synthesized a compensation. Representative repair:\n")
		for _, rp := range s.Repairs() {
			fmt.Fprintf(&b, "    repair %s → %s\n", rp[0], rp[1])
		}
	case s.Exhaustive:
		fmt.Fprintf(&b, "  IMPOSSIBLE — no compensation converges (search exhausted).\n")
		if s.witness != "" {
			fmt.Fprintf(&b, "  Witness: %s.\n", s.witness)
		}
		fmt.Fprintf(&b, "  These invariants and events cannot converge under any compensation; redesign the events.\n")
	default:
		fmt.Fprintf(&b, "  UNDETERMINED — no compensation found within the search budget (%d nodes); one may exist.\n", maxSynthNodes)
		fmt.Fprintf(&b, "  A SAT/SMT encoding would settle it.\n")
	}
	return b.String()
}
