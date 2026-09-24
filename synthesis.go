package gsm

import (
	"fmt"
	"sort"
	"strings"
)

// maxSynthAssignments bounds the brute-force synthesis search. The candidate space is
// |valid|^|invalid|, which grows fast; beyond this, synthesis returns an error rather than
// hang. (A SAT/SMT encoding would lift this ceiling — a natural next step.)
const maxSynthAssignments = 1 << 20

// Synthesis is the result of Registry.Synthesize: whether a convergent compensation exists
// for the registry's invariants and events, how many distinct ones the search found, and a
// representative one (as a ready-to-use Machine and an inspectable repair map).
type Synthesis struct {
	Convergent   bool // does any compensation make this registry converge?
	Alternatives int  // number of distinct convergent compensations found
	Searched     int  // candidate assignments examined

	r        *Registry
	nf       []uint64 // representative convergent normal-form table (nil if not convergent)
	step     [][]uint64
	invalids []uint64 // packed invalid states (for Repairs())
}

// Synthesize searches for a compensation (a normal-form map on invalid states) that makes
// the registry converge — satisfying WFC and CC — from the invariants' validity predicates
// and the events alone. Any Repair functions already on the invariants are IGNORED: the
// point is to generate one. It returns a representative convergent compensation if one
// exists, reports how many alternatives were found, or reports that none exists (the
// invariants and events cannot converge under any compensation).
//
// Convergent does not mean desirable: the synthesized repair merely makes orderings agree.
// Inspect Repairs() (or enumerate alternatives) and judge whether the repair is acceptable;
// if the only convergent repairs are unacceptable, the events — not the compensation — need
// redesign.
//
// The search is brute force over |valid|^|invalid| assignments, bounded by an internal cap;
// it errors rather than hang on larger registries.
func (r *Registry) Synthesize() (*Synthesis, error) {
	if r.totalBits > 20 {
		return nil, fmt.Errorf("gsm: state space too large (%d bits, max 20)", r.totalBits)
	}
	packedCount := 1 << r.totalBits
	mk := func(id int) State { return State{packed: uint64(id), vars: r.vars} }

	// Partition the encodable states into valid and invalid.
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

	// Bound the search: |valids|^|invalids|.
	total := 1
	for range invalids {
		if total > maxSynthAssignments/len(valids) {
			return nil, fmt.Errorf("gsm: synthesis space too large (%d invalid states over %d valid); "+
				"brute-force synthesis is bounded at %d assignments", len(invalids), len(valids), maxSynthAssignments)
		}
		total *= len(valids)
	}

	// Precompute the raw post-event state for every (event, encodable state) — independent of
	// the candidate compensation.
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

	// Search every assignment of invalid states to valid repair targets.
	var repr []uint64
	working := 0
	idx := make([]int, len(invalids))
	nf := make([]uint64, packedCount)
	for {
		// Build candidate nf: identity everywhere, then reroute invalid states.
		for s := 0; s < packedCount; s++ {
			nf[s] = uint64(s)
		}
		for k, inv := range invalids {
			nf[inv] = uint64(valids[idx[k]])
		}

		if ccHolds(nf, rawStep, validEnc, isValidState, pairs) {
			working++
			if repr == nil {
				repr = append([]uint64(nil), nf...)
			}
		}

		// advance mixed-radix counter over invalid states
		k := len(invalids) - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < len(valids) {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			break
		}
	}

	out := &Synthesis{Convergent: repr != nil, Alternatives: working, Searched: total, r: r}
	for _, s := range invalids {
		out.invalids = append(out.invalids, uint64(s))
	}
	if repr != nil {
		out.nf = repr
		out.step = make([][]uint64, len(r.events))
		for ei := range r.events {
			out.step[ei] = make([]uint64, packedCount)
			for s := 0; s < packedCount; s++ {
				if validEnc[s] {
					out.step[ei][s] = repr[rawStep[ei][s]]
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

// ccHolds checks CC1 (order independence, all encodable states, declared pairs) and CC2
// (compensation absorption, invalid states) for a candidate normal-form map.
func ccHolds(nf []uint64, rawStep [][]uint64, validEnc, isValidState []bool, pairs [][2]int) bool {
	step := func(e int, s uint64) uint64 { return nf[rawStep[e][s]] }
	for s := 0; s < len(validEnc); s++ {
		if !validEnc[s] {
			continue
		}
		u := uint64(s)
		// CC1
		for _, p := range pairs {
			if step(p[1], step(p[0], u)) != step(p[0], step(p[1], u)) {
				return false
			}
		}
		// CC2 on invalid states
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

// Machine returns a ready-to-use Machine built from the synthesized compensation, or nil if
// the registry has no convergent compensation.
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

// Repairs returns the synthesized repair for each invalid state as {invalid, target} pairs,
// sorted, for inspection. Empty if not convergent. (State is not comparable — it carries a
// variable list — so this is a slice, not a map.)
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

// String renders a human-readable synthesis report.
func (s *Synthesis) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Synthesis: %s\n", s.r.name)
	if !s.Convergent {
		fmt.Fprintf(&b, "  IMPOSSIBLE — no compensation converges (searched %d assignments).\n", s.Searched)
		fmt.Fprintf(&b, "  These invariants and events cannot converge under any compensation; redesign the events.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  CONVERGENT — %d of %d compensations work. Representative repair:\n", s.Alternatives, s.Searched)
	for _, rp := range s.Repairs() {
		fmt.Fprintf(&b, "    repair %s → %s\n", rp[0], rp[1])
	}
	return b.String()
}
