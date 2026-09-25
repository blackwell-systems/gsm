package gsm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Certificate is a serializable verdict for a verified (sub-)federation so it can be reused by
// EmbedCertified as a black box: the embedding checks only the boundary and skips re-verifying
// the subsystem's internals. See CERTIFICATE-DESIGN.md.
//
// The digest covers each component's serializable policy AND the extracted morphism tables (see
// MorphismTable), so it is tamper-complete: it changes if a component rule, the wiring, or any
// morphism's behavior changes. A consumer can re-check the federated conditions from the tables,
// independently of the producer's morphism closures, via Verify.
//
// Remaining scope:
//   - Reuse is restricted to the output-port case: an outer morphism may read a certified
//     subsystem (the subsystem as a source) but may not write into it. Writing in needs the
//     input-port (assume-guarantee) extension and is rejected at Build for now.
//   - Verify re-derives validity preservation and acyclicity, and matches the digest, using the
//     component invariants. The strongest form, an axiom-free-Coq-extracted oracle that re-checks
//     the federated conditions the way astchecker re-checks single-registry rules, is future work
//     (it requires mechanizing the federation conditions in Coq first).
type Certificate struct {
	Name     string          // the certified sub-federation's name
	Digest   string          // covers component policies AND the extracted morphism tables (tamper-complete)
	Report   *FedReport      // the verdict from the sub's own Build
	Tables   []MorphismTable // the morphisms and resolvers in extensional form (see MorphismTable)
	Monotone bool            // whether the sub used AllowMonotoneCycles
}

// MorphismTable is a morphism or resolver in extensional form: for each valid source state (or,
// for a resolver, each valid combination of source states) it records the shared-component values
// written into the target. Because state domains are finite and the shared image is
// source-determined (R1, verified at Build), every morphism closure has an exact finite table, so
// the table captures the morphism's full semantics without the opaque Go function. A verifier
// re-checks the federated conditions (validity preservation, M1/R2) from these tables against the
// component registries, independently of the producer's morphism closures. See CERTIFICATE-DESIGN.md.
type MorphismTable struct {
	Target  string     // target registry name
	Sources []string   // source registry names (one entry = single-source morphism)
	Shared  []string   // shared variable names the morphism/resolver writes
	Rows    []TableRow // one row per valid source (combination), in ascending source-id order
}

// TableRow is one entry of a MorphismTable: the packed source state id per source (aligned with
// MorphismTable.Sources) and the raw shared values written (aligned with MorphismTable.Shared).
type TableRow struct {
	SourceIDs []uint64
	Values    []uint64
}

// certifiedEmbed records a sub-federation embedded on certificate: its component set (to classify
// edges as internal versus seam), the certificate, and the live sub for digest recomputation.
type certifiedEmbed struct {
	comps map[*Registry]bool
	cert  *Certificate
	sub   *Federation
}

// Certify builds the federation, verifies it converges, and returns a certificate naming it. The
// certificate can then be handed to EmbedCertified on a larger federation.
func (f *Federation) Certify() (*Certificate, error) {
	_, rep, err := f.Build()
	if err != nil {
		return nil, err
	}
	tables, err := f.extractTables()
	if err != nil {
		return nil, err
	}
	dig, err := digestComponentsAndTables(f.comps, tables, f.allowCycles)
	if err != nil {
		return nil, err
	}
	return &Certificate{Name: f.name, Digest: dig, Report: rep, Tables: tables, Monotone: f.allowCycles}, nil
}

// EmbedCertified composes a sub-federation into this one on the strength of its certificate: like
// Embed, but Build does not re-verify the subsystem's internals. Only the seam (morphisms crossing
// the boundary) plus the whole-graph acyclicity/monotonicity check run. The certificate's digest
// must match the sub at Build, and (first-cut limitation) no outer morphism may target a component
// inside the sub. Use Embed for the full-re-verification form.
func (f *Federation) EmbedCertified(sub *Federation, cert *Certificate) *Federation {
	ce := &certifiedEmbed{comps: make(map[*Registry]bool, len(sub.comps)), cert: cert, sub: sub}
	for _, r := range sub.comps {
		f.register(r)
		ce.comps[r] = true
	}
	f.edges = append(f.edges, sub.edges...)
	for r, res := range sub.resolvers {
		f.resolvers[r] = res
	}
	if sub.allowCycles {
		f.allowCycles = true
	}
	f.certified = append(f.certified, ce)
	return f
}

// subOf maps each component to the index of the certified embed it belongs to, leaving components
// that are not part of any certified subsystem absent.
func (f *Federation) subOf() map[*Registry]int {
	m := make(map[*Registry]int)
	for id, ce := range f.certified {
		for r := range ce.comps {
			m[r] = id
		}
	}
	return m
}

// internalEdge reports whether both endpoints of e belong to the same certified sub-federation,
// so its verification is covered by that certificate.
func internalEdge(subOf map[*Registry]int, e edgeDef) bool {
	sid, sok := subOf[e.src]
	did, dok := subOf[e.dst]
	return sok && dok && sid == did
}

// validateCertificates checks, before any component is built, that every certified embed still
// matches its certificate (digest) and that the seam obeys the output-port restriction (no outer
// morphism writes into a certified subsystem).
func (f *Federation) validateCertificates(subOf map[*Registry]int) error {
	for _, ce := range f.certified {
		if ce.cert == nil {
			return fmt.Errorf("gsm: certified embed of %q has a nil certificate", ce.sub.name)
		}
		got, err := ce.sub.subDigest()
		if err != nil {
			return fmt.Errorf("gsm: cannot digest certified sub-federation %q: %w", ce.sub.name, err)
		}
		if got != ce.cert.Digest {
			return fmt.Errorf("gsm: certificate for %q does not match the embedded sub-federation; "+
				"rebuild the certificate from the current subsystem", ce.sub.name)
		}
	}
	// Output-port restriction: reject any morphism whose target is inside a certified sub but whose
	// source is outside that same sub (writing into the subsystem).
	for _, e := range f.edges {
		did, dok := subOf[e.dst]
		if !dok {
			continue
		}
		if sid, sok := subOf[e.src]; !sok || sid != did {
			return fmt.Errorf("gsm: morphism %s→%s writes into certified sub-federation %q; only reading a "+
				"certified subsystem is supported (use Embed to write into it)",
				e.src.name, e.dst.name, f.certified[did].sub.name)
		}
	}
	return nil
}

// reportFor returns the certificate's per-component report for the named registry, or nil.
func (ce *certifiedEmbed) reportFor(name string) *Report {
	if ce.cert == nil || ce.cert.Report == nil {
		return nil
	}
	for _, c := range ce.cert.Report.Components {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// subDigest is a stable digest over the federation's component policies and the extracted morphism
// tables, so it covers both the components and the full semantics of every morphism and resolver
// (the tables are their exact extensional form). It is tamper-complete: changing a component rule,
// a wiring edge, or a morphism's behavior changes the digest.
func (f *Federation) subDigest() (string, error) {
	tables, err := f.extractTables()
	if err != nil {
		return "", err
	}
	return digestComponentsAndTables(f.comps, tables, f.allowCycles)
}

// digestComponentsAndTables computes the certificate digest from the component registries and the
// morphism tables. Domain-separated and deterministic: components sorted by name, tables sorted by
// their canonical serialization. Used both to produce a certificate (Certify) and to re-check one
// against a consumer's own components (Certificate.Verify).
func digestComponentsAndTables(comps []*Registry, tables []MorphismTable, allowCycles bool) (string, error) {
	h := sha256.New()
	h.Write([]byte("gsm-fedcert-v2\n"))

	cs := append([]*Registry(nil), comps...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	for _, r := range cs {
		b, err := r.PolicyBytes()
		if err != nil {
			return "", fmt.Errorf("gsm: component %q: %w", r.name, err)
		}
		h.Write([]byte(fmt.Sprintf("comp %s\n", r.name)))
		h.Write(b)
		h.Write([]byte{'\n'})
	}

	serialized := make([]string, 0, len(tables))
	for _, t := range tables {
		serialized = append(serialized, t.serialize())
	}
	sort.Strings(serialized)
	for _, s := range serialized {
		h.Write([]byte(s))
	}
	if allowCycles {
		h.Write([]byte("allowcycles\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// serialize renders a table to a canonical, deterministic string for digesting.
func (t MorphismTable) serialize() string {
	var b strings.Builder
	fmt.Fprintf(&b, "table %s <- [%s] shared=[%s]\n", t.Target, strings.Join(t.Sources, ","), strings.Join(t.Shared, ","))
	for _, row := range t.Rows {
		fmt.Fprintf(&b, "  %v => %v\n", row.SourceIDs, row.Values)
	}
	return b.String()
}

// extractTables reifies every morphism and resolver into its extensional table by enumerating the
// valid source states. Source-determinacy (verified at Build) makes the shared image independent of
// the target, so a representative (zero) target suffices. Must be called on a structurally valid
// federation (single-source targets have exactly one incoming edge, multi-source targets have a
// resolver); Certify and the Build-time validation both guarantee that.
func (f *Federation) extractTables() ([]MorphismTable, error) {
	inEdges := make(map[*Registry][]edgeDef)
	for _, e := range f.edges {
		inEdges[e.dst] = append(inEdges[e.dst], e)
	}
	var tables []MorphismTable
	for _, target := range f.comps { // f.comps order is deterministic
		edges := inEdges[target]
		if len(edges) == 0 {
			continue
		}
		if resolver, ok := f.resolvers[target]; ok {
			tables = append(tables, extractResolverTable(target, resolver, edges))
		} else {
			tables = append(tables, extractEdgeTable(edges[0]))
		}
	}
	return tables, nil
}

// extractEdgeTable reifies a single-source morphism.
func extractEdgeTable(e edgeDef) MorphismTable {
	dstRep := State{vars: e.dst.vars}
	t := MorphismTable{Target: e.dst.name, Sources: []string{e.src.name}}
	for _, v := range e.shared {
		t.Shared = append(t.Shared, v.name)
	}
	for _, sa := range e.src.validStates() {
		img := e.mapFn(sa, dstRep)
		row := TableRow{SourceIDs: []uint64{sa.packed}}
		for _, v := range e.shared {
			row.Values = append(row.Values, img.getRaw(v))
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// extractResolverTable reifies a multi-source resolver over every valid source combination.
func extractResolverTable(target *Registry, resolver Resolver, edges []edgeDef) MorphismTable {
	var sources []*Registry
	seen := map[*Registry]bool{}
	sharedSeen := map[int]bool{}
	var sharedVars []Var
	for _, e := range edges {
		if !seen[e.src] {
			seen[e.src] = true
			sources = append(sources, e.src)
		}
		for _, v := range e.shared {
			if !sharedSeen[v.index] {
				sharedSeen[v.index] = true
				sharedVars = append(sharedVars, v)
			}
		}
	}
	t := MorphismTable{Target: target.name}
	for _, s := range sources {
		t.Sources = append(t.Sources, s.name)
	}
	for _, v := range sharedVars {
		t.Shared = append(t.Shared, v.name)
	}
	srcValids := make([][]State, len(sources))
	for i, s := range sources {
		srcValids[i] = s.validStates()
		if len(srcValids[i]) == 0 {
			return t // a source with no valid states leaves the table empty
		}
	}
	dstRep := State{vars: target.vars}
	idx := make([]int, len(sources))
	for {
		combo := make(map[string]State, len(sources))
		ids := make([]uint64, len(sources))
		for k, s := range sources {
			st := srcValids[k][idx[k]]
			combo[s.name] = st
			ids[k] = st.packed
		}
		merged := resolver(dstRep, combo)
		row := TableRow{SourceIDs: ids}
		for _, v := range sharedVars {
			row.Values = append(row.Values, merged.getRaw(v))
		}
		t.Rows = append(t.Rows, row)

		k := len(sources) - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < len(srcValids[k]) {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			break
		}
	}
	return t
}

// Verify is the independent, differential re-checker a consumer runs on a certificate it received,
// given its own copies of the component registries (which it can digest-match). It re-derives the
// federated conditions from the certificate's morphism tables, NOT from the producer's morphism
// closures, so it confirms the composition without trusting the producer's code:
//
//   - tamper check: the digest recomputed from the provided components and the certificate's tables
//     must equal the certificate's digest;
//   - validity preservation (M1 for single-source, R2 for resolvers): for every table row, writing
//     the recorded shared values into every valid target state must keep the target valid;
//   - acyclicity: unless the certificate is marked Monotone, the morphism graph must be acyclic.
//
// The component invariants are evaluated from the provided registries (part of the shared, digest
// covered definition); only the morphism closures are replaced by the tables. The monotonicity of a
// cyclic (Monotone) certificate is not re-derived here and is left to the component-level oracle.
func (c *Certificate) Verify(comps map[string]*Registry) error {
	list := make([]*Registry, 0, len(comps))
	for _, r := range comps {
		list = append(list, r)
	}
	dig, err := digestComponentsAndTables(list, c.Tables, c.Monotone)
	if err != nil {
		return err
	}
	if dig != c.Digest {
		return fmt.Errorf("gsm: certificate %q digest does not match the provided components and tables", c.Name)
	}

	for _, t := range c.Tables {
		target, ok := comps[t.Target]
		if !ok {
			return fmt.Errorf("gsm: certificate names target registry %q not present in the provided components", t.Target)
		}
		sharedVars, err := varsByName(target, t.Shared)
		if err != nil {
			return err
		}
		dstValid := target.validStates()
		for _, row := range t.Rows {
			for _, dv := range dstValid {
				s := dv
				for i, v := range sharedVars {
					s = s.setRaw(v, row.Values[i])
				}
				if !target.allInvariantsHold(s) {
					return fmt.Errorf("gsm: certificate table for %q violates validity preservation (M1/R2): "+
						"a recorded shared image makes the target invalid", t.Target)
				}
			}
		}
	}

	if !c.Monotone && tablesHaveCycle(c.Tables) {
		return fmt.Errorf("gsm: certificate %q morphism graph has a cycle but is not marked monotone", c.Name)
	}
	return nil
}

// varsByName resolves shared-variable names to the target's Var handles.
func varsByName(target *Registry, names []string) ([]Var, error) {
	byName := make(map[string]Var, len(target.vars))
	for _, v := range target.vars {
		byName[v.name] = v
	}
	out := make([]Var, len(names))
	for i, n := range names {
		v, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("gsm: shared variable %q not found on registry %q", n, target.name)
		}
		out[i] = v
	}
	return out, nil
}

// tablesHaveCycle reports whether the morphism graph implied by the tables (an edge from each
// source to each target) contains a cycle, via Kahn's algorithm on registry names.
func tablesHaveCycle(tables []MorphismTable) bool {
	indeg := map[string]int{}
	adj := map[string][]string{}
	nodes := map[string]bool{}
	for _, t := range tables {
		nodes[t.Target] = true
		for _, s := range t.Sources {
			nodes[s] = true
			adj[s] = append(adj[s], t.Target)
			indeg[t.Target]++
		}
	}
	queue := make([]string, 0, len(nodes))
	for n := range nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	seen := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		seen++
		for _, m := range adj[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	return seen != len(nodes)
}
