package gsm

import (
	"fmt"
)

// Certified execution: a FedMachine runs an EmbedCertified sub's internal morphisms and resolvers
// from the certificate's verified tables, not from the live Go closures. Build re-checks the
// tables (M1/R2, completeness, ports, acyclicity or monotonicity), binds them to the certificate's
// digest, and compiles them into the lookup below, so what the certificate's checks cover is
// exactly what runs. The component machines were already table driven (Registry.Build's step and
// normal-form tables, re-checked and oracle-gated at Build), so with this a certified sub runs no
// user closure at Apply time on an acyclic network.

// certTable is one certificate table in runtime form: the target's shared bits as a function of
// its source component states. The certificate's rows are compiled into a dense array keyed by the
// sources' valid-state ranks, so a lookup is one rank read per source plus one array read, and the
// rows (one []uint64 per source combination in the certificate) are not kept.
type certTable struct {
	sub    string    // the certified sub-federation's name, for messages
	srcs   []int     // source component indices, aligned with the table's Sources
	rank   [][]int32 // rank[k][packed] = the index of srcs[k]'s state among its valid states, or -1
	stride []int     // stride[k] = the key weight of source k (mixed radix over the valid counts)
	img    []uint64  // img[key] = the target's shared bits for the source combination with that key
	mask   uint64    // the bits of the target's shared variables (the only bits img may set)
}

// certExecLine is the FedReport.Runtime line for a certified sub whose targets run from tables.
const certExecLine = "certified sub-federation %q executes verified tables: %d internal target(s) repaired by " +
	"certificate table lookup; its morphism and resolver closures are not run by Apply"

// snapshotTables deep-copies a certificate's tables, so the copy Build checks is the copy it
// compiles: a closure Build runs between the two cannot change what was checked.
func snapshotTables(ts []MorphismTable) []MorphismTable {
	out := make([]MorphismTable, len(ts))
	for i, t := range ts {
		rows := make([]TableRow, len(t.Rows))
		for k, r := range t.Rows {
			rows[k] = TableRow{
				SourceIDs: append([]uint64(nil), r.SourceIDs...),
				Values:    append([]uint64(nil), r.Values...),
			}
		}
		out[i] = MorphismTable{
			Target:  t.Target,
			Sources: append([]string(nil), t.Sources...),
			Shared:  append([]string(nil), t.Shared...),
			Rows:    rows,
		}
	}
	return out
}

// checkCertifiedWiring rejects wiring the certificate does not describe: a morphism the outer
// federation declares between two components of the same certified sub (it would be classified
// internal, so neither verified at the seam nor in the certificate), and a resolver the outer
// federation declares for a target whose sources are all inside the sub, when the sub itself has
// none. Both would otherwise be silently replaced by the certificate's table at run time.
func (f *Federation) checkCertifiedWiring(id int, ce *certifiedEmbed, subOf map[*Registry]int) error {
	subIn := make(map[*Registry]map[*Registry]int)
	for _, e := range ce.sub.edges {
		if subIn[e.dst] == nil {
			subIn[e.dst] = map[*Registry]int{}
		}
		subIn[e.dst][e.src]++
	}
	got := make(map[*Registry]map[*Registry]int)
	for _, e := range f.edges {
		if !internalEdge(subOf, e) || subOf[e.dst] != id {
			continue
		}
		if got[e.dst] == nil {
			got[e.dst] = map[*Registry]int{}
		}
		got[e.dst][e.src]++
		if got[e.dst][e.src] > subIn[e.dst][e.src] {
			return fmt.Errorf("gsm: morphism %s→%s joins two components of certified sub-federation %q but is not "+
				"one of its morphisms; the certificate does not cover it (declare it in the sub and re-certify, "+
				"or embed with Embed for full re-verification)", e.src.name, e.dst.name, ce.sub.name)
		}
	}
	for r := range ce.comps {
		if _, outer := f.resolvers[r]; !outer {
			continue
		}
		if _, own := ce.sub.resolvers[r]; own {
			continue
		}
		var in []edgeDef
		for _, e := range f.edges {
			if e.dst == r {
				in = append(in, e)
			}
		}
		if !hasSeamIncoming(subOf, id, in) {
			return fmt.Errorf("gsm: a Resolver for %q is declared outside certified sub-federation %q, but every "+
				"source of %q is inside it; the certificate's table, not that resolver, defines %q's shared component",
				r.name, ce.sub.name, r.name, r.name)
		}
	}
	return nil
}

// compileCertTables builds the runtime tables for every target whose incoming morphisms are
// exactly its certified sub's (no seam writer, none removed by coordination), from the checked
// table snapshots validateCertificates returned (one slice per certified embed). It returns, per
// certified embed, how many targets run from tables. A target it does not compile keeps its
// closures, which Build verified (at the seam, or through the binding check).
func (f *Federation) compileCertTables(m *FedMachine, snaps [][]MorphismTable, subOf map[*Registry]int) []int {
	m.certTab = make([]*certTable, len(f.comps))
	counts := make([]int, len(f.certified))
	ranks := make(map[int][]int32)
	rankOf := func(i int) []int32 {
		if r, ok := ranks[i]; ok {
			return r
		}
		c := m.comps[i]
		r := make([]int32, len(c.nf))
		var n int32
		for p := range r {
			if c.isVerifiedState(uint64(p)) {
				r[p] = n
				n++
			} else {
				r[p] = -1
			}
		}
		ranks[i] = r
		return r
	}
	inEdges := make([][]edgeDef, len(f.comps))
	for _, e := range f.edges {
		inEdges[f.idx[e.dst]] = append(inEdges[f.idx[e.dst]], e)
	}
	for id, ce := range f.certified {
		subIn := map[*Registry]int{}
		for _, e := range ce.sub.edges {
			subIn[e.dst]++
		}
		byName := make(map[string]*Registry, len(ce.sub.comps))
		for _, r := range ce.sub.comps {
			byName[r.name] = r
		}
		for _, t := range snaps[id] {
			target, ok := byName[t.Target]
			if !ok {
				continue
			}
			j := f.idx[target]
			if hasSeamIncoming(subOf, id, inEdges[j]) || len(inEdges[j]) != subIn[target] {
				continue
			}
			ct, ok := f.compileCertTable(m, ce.sub.name, t, byName, target, rankOf)
			if !ok {
				continue
			}
			m.certTab[j] = ct
			counts[id]++
		}
	}
	return counts
}

// compileCertTable compiles one checked table. It reports false (the target keeps its closures)
// when a source has no valid state (the table is empty and there is nothing certified to run) or
// the rows do not cover the source combinations one to one, which the table re-check already
// rules out.
func (f *Federation) compileCertTable(m *FedMachine, sub string, t MorphismTable, byName map[string]*Registry,
	target *Registry, rankOf func(int) []int32) (*certTable, bool) {
	ct := &certTable{sub: sub, srcs: make([]int, len(t.Sources)), rank: make([][]int32, len(t.Sources)), stride: make([]int, len(t.Sources))}
	counts := make([]int, len(t.Sources))
	total := 1
	for k, name := range t.Sources {
		src, ok := byName[name]
		if !ok {
			return nil, false
		}
		ct.srcs[k] = f.idx[src]
		ct.rank[k] = rankOf(ct.srcs[k])
		for _, r := range ct.rank[k] {
			if r >= 0 {
				counts[k]++
			}
		}
		if counts[k] == 0 {
			return nil, false
		}
		total *= counts[k]
	}
	if len(t.Sources) == 0 || total != len(t.Rows) {
		return nil, false
	}
	for k := len(t.Sources) - 1; k >= 0; k-- {
		if k == len(t.Sources)-1 {
			ct.stride[k] = 1
		} else {
			ct.stride[k] = ct.stride[k+1] * counts[k+1]
		}
	}
	shared, err := varsByName(target, t.Shared)
	if err != nil {
		return nil, false
	}
	for _, v := range shared {
		ct.mask |= uint64((1<<v.bits)-1) << v.offset
	}
	ct.img = make([]uint64, total)
	filled := make([]bool, total)
	for _, row := range t.Rows {
		if len(row.SourceIDs) != len(ct.srcs) || len(row.Values) != len(shared) {
			return nil, false
		}
		key := 0
		for k, id := range row.SourceIDs {
			if id >= uint64(len(ct.rank[k])) || ct.rank[k][id] < 0 {
				return nil, false
			}
			key += int(ct.rank[k][id]) * ct.stride[k]
		}
		if filled[key] {
			return nil, false
		}
		filled[key] = true
		var img uint64
		for i, v := range shared {
			img |= row.Values[i] << v.offset
		}
		ct.img[key] = img
	}
	return ct, true
}

// isVerifiedState reports whether packed is a valid state of this table machine: an in-domain
// encoding that is its own normal form. These are the states the certificate's tables cover.
func (m *Machine) isVerifiedState(p uint64) bool {
	return p < uint64(len(m.nf)) && m.valid[p] && m.nf[p] == p
}

// lookup returns the target's shared bits for the sources' states in fs, or an error naming the
// first source state the certificate does not cover (one that is not a valid state of its source).
func (m *FedMachine) certLookup(ct *certTable, fs FedState) (uint64, error) {
	key := 0
	for k, si := range ct.srcs {
		p := fs.states[si].packed
		r := ct.rank[k]
		if p >= uint64(len(r)) || r[p] < 0 {
			return 0, fmt.Errorf("source %q is in state %s, which is not a valid state of %q, so it is outside the "+
				"domain the certificate of sub-federation %q covers", m.comps[si].name, fs.states[si], m.comps[si].name, ct.sub)
		}
		key += int(r[p]) * ct.stride[k]
	}
	return ct.img[key], nil
}

// tableRepair is repair for a target that runs from its certificate table: the target's shared
// variables are overwritten with the table's image of its sources. It panics, naming the state,
// when a source or the target is outside the certified domain (not a valid state of its
// component); Phase 1 normalization and M1 keep every state Normalize reaches inside it, so this
// happens only for a FedState that is not a state of this machine.
func (m *FedMachine) tableRepair(ct *certTable, fs FedState, j int) State {
	c := m.comps[j]
	dst := fs.states[j]
	if !c.isVerifiedState(dst.packed) {
		panic(fmt.Sprintf("gsm: federation %q: target %q is in state %s, which is not a valid state of %q, so it is "+
			"outside the domain the certificate of sub-federation %q covers; pass a FedState of this machine",
			m.name, c.name, dst, c.name, ct.sub))
	}
	img, err := m.certLookup(ct, fs)
	if err != nil {
		panic(fmt.Sprintf("gsm: federation %q: repairing %q: %v; pass a FedState of this machine", m.name, c.name, err))
	}
	return State{packed: dst.packed&^ct.mask | img, vars: c.vars}
}

// certRuntime compiles the certified subs' tables into m (acyclic networks only) and returns the
// FedReport.Runtime lines saying what each certified sub runs at Apply time.
func (f *Federation) certRuntime(m *FedMachine, snaps [][]MorphismTable, subOf map[*Registry]int) []string {
	if len(f.certified) == 0 {
		return nil
	}
	lines := make([]string, 0, len(f.certified))
	if m.cyclic {
		// Kleene iteration resets every shared component to bottom and repairs from there, so it
		// evaluates repair on source states that are not valid; the tables cover valid states
		// only. The closures run, bound to the tables on the valid states (bindClosures) and
		// checked by the monotone-cycle checks on the states the iteration visits.
		for _, ce := range f.certified {
			lines = append(lines, fmt.Sprintf("certified sub-federation %q runs its morphism and resolver closures: "+
				"the network is cyclic, and Kleene iteration evaluates repair on states outside the certificate's "+
				"tables (the closures are bound to the tables on every valid state)", ce.sub.name))
		}
		return lines
	}
	counts := f.compileCertTables(m, snaps, subOf)
	for id, ce := range f.certified {
		if counts[id] == len(snaps[id]) {
			lines = append(lines, fmt.Sprintf(certExecLine, ce.sub.name, counts[id]))
			continue
		}
		lines = append(lines, fmt.Sprintf("certified sub-federation %q executes verified tables for %d of its %d internal "+
			"target(s); the rest run closures verified at Build (a target with a seam writer, a coordinated "+
			"morphism, or a source with no valid state)", ce.sub.name, counts[id], len(snaps[id])))
	}
	return lines
}
