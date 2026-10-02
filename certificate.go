package gsm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Certificate is a serializable verdict for a verified (sub-)federation so it can be reused by
// EmbedCertified: the embedding skips re-verifying the subsystem's internal morphisms from their
// closures, re-checks them from the certificate's tables instead, and rebuilds each component
// with Build (so component convergence is re-checked, not trusted). A certificate is validated by
// re-check, never trusted for its recorded verdict; see CERTIFICATE-DESIGN.md.
//
// The digest covers each component's name, serializable policy (PolicyBytes) and the names and
// declared pairs its rules are addressed by (PolicyNames), the extracted morphism tables (see
// MorphismTable), the declared input ports, and the cycle opt-in. It is tamper-complete over those
// declarations: it changes if a component rule, a variable, event or enum-label name, a declared
// Independent pair, the wiring, a recorded morphism image, or the port declaration changes. A
// morphism or resolver closure is bound only through its table, which records its images at one
// representative target, so a closure that differs only at other targets digests the same (it is
// caught at runtime by the FedMachine's image check). A consumer can re-check the federated
// conditions from the tables, independently of the producer's morphism closures, via Verify.
//
// Ports (assume-guarantee, Theorem 2' of the categorical note): a certified subsystem may declare
// input ports at Certify time (Certify(ports...)). An input port is a shared variable that no
// internal morphism writes (it is free inside the sub); once embedded, an outer morphism may write
// it, and Build verifies that boundary morphism at the seam (M1/R2) while still skipping the
// closure-level re-verification of the subsystem's internal morphisms. The certificate is inherently parametric over the input port's whole
// domain because Build already verifies every component and source state exhaustively. A shared
// variable not declared an input port stays sealed, and an inbound morphism to it is rejected.
//
// Verify re-derives validity preservation and acyclicity, matches the digest, confirms declared
// input ports are free (no table writes them), and rebuilds every component with Build to re-check
// its convergence. The strongest form,
// an axiom-free-Coq-extracted oracle that re-checks the federated conditions the way astchecker
// re-checks single-registry rules, is future work (it needs the federation conditions mechanized in
// Coq first).
type Certificate struct {
	Name       string          // the certified sub-federation's name
	Digest     string          // covers component rules, names and pairs, morphism tables, and input ports
	Report     *FedReport      // the verdict from the sub's own Build
	Tables     []MorphismTable // the morphisms and resolvers in extensional form (see MorphismTable)
	Monotone   bool            // whether the sub used AllowMonotoneCycles
	InputPorts []PortRef       // shared variables an outer morphism may write into this subsystem
}

// Port declares a shared variable of a sub-federation component as an input port: a variable an
// outer morphism may write when the subsystem is embedded on certificate. It must be free inside the
// sub (no internal morphism writes it), which Certify verifies.
type Port struct {
	Registry *Registry
	Var      Var
}

// PortRef is a Port in serialized (name) form, as stored in a Certificate.
type PortRef struct {
	Registry string
	Var      string
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
// certificate can then be handed to EmbedCertified on a larger federation. Any input ports passed
// here are the shared variables an outer morphism may later write into the subsystem; each must be
// free (no internal morphism writes it), which Certify checks.
//
// The certificate describes the federation as it was when Certify was called: a morphism,
// component, or resolver added to f while Certify runs (from inside a morphism closure, say) is
// not in it, and a component registry changed while Certify runs is rejected.
func (f *Federation) Certify(inputPorts ...Port) (*Certificate, error) {
	// Build runs morphism closures after it verifies the components, and so does table
	// extraction. Certify works on a frozen copy of the federation's wiring, so a closure that adds
	// a morphism, component, or resolver to f there cannot get it into the certificate, and it
	// compares each component's declarations before building with those it digests, so a closure
	// that declares on a component cannot either.
	g, before := f.frozen()
	// A component changed by a closure is reported over any error the change caused (see
	// Build); the closures run in build and in extractTables.
	_, rep, err := g.build()
	if err != nil {
		if cerr := checkComponentsUnchanged(g.comps, before); cerr != nil {
			return nil, cerr
		}
		return nil, err
	}
	refs, err := g.validateInputPorts(inputPorts)
	if err != nil {
		return nil, err
	}
	tables, err := g.extractTables()
	if cerr := checkComponentsUnchanged(g.comps, before); cerr != nil {
		return nil, cerr
	}
	if err != nil {
		return nil, err
	}
	dig, err := digestComponentsAndTables(g.comps, tables, g.allowCycles, refs)
	if err != nil {
		return nil, err
	}
	return &Certificate{Name: g.name, Digest: dig, Report: rep, Tables: tables, Monotone: g.allowCycles, InputPorts: refs}, nil
}

// validateInputPorts checks each declared input port names a component of this federation and is
// free (no internal morphism writes it), and returns the ports in serialized, deterministic form.
// Freeness is the precondition for the assume-guarantee reuse: a variable an internal morphism
// controls is an output the sub owns, so letting an outer morphism also write it would make the
// target multi-source in a way the certificate never covered.
func (f *Federation) validateInputPorts(ports []Port) ([]PortRef, error) {
	written := map[*Registry]map[int]bool{}
	for _, e := range f.edges {
		if written[e.dst] == nil {
			written[e.dst] = map[int]bool{}
		}
		for _, v := range e.shared {
			written[e.dst][v.index] = true
		}
	}
	refs := make([]PortRef, 0, len(ports))
	for _, p := range ports {
		if _, ok := f.idx[p.Registry]; !ok {
			return nil, fmt.Errorf("gsm: input port %s.%s names a registry not in federation %q", p.Registry.name, p.Var.name, f.name)
		}
		// The variable must be this registry's, by value (sameVar), not merely an index into
		// it: a Var of another registry would otherwise free, or seal, whichever variable
		// sits at its index here.
		if p.Var.index < 0 || p.Var.index >= len(p.Registry.vars) || !sameVar(p.Registry.vars[p.Var.index], p.Var) {
			return nil, fmt.Errorf("gsm: input port %s.%s is not a variable of registry %q", p.Registry.name, p.Var.name, p.Registry.name)
		}
		if written[p.Registry][p.Var.index] {
			return nil, fmt.Errorf("gsm: input port %s.%s is written by an internal morphism; an input port must "+
				"be free (no internal writer)", p.Registry.name, p.Var.name)
		}
		refs = append(refs, PortRef{Registry: p.Registry.name, Var: p.Var.name})
	}
	sortPortRefs(refs)
	return refs, nil
}

// sortPortRefs orders port refs deterministically for digesting.
func sortPortRefs(refs []PortRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Registry != refs[j].Registry {
			return refs[i].Registry < refs[j].Registry
		}
		return refs[i].Var < refs[j].Var
	})
}

// EmbedCertified composes a sub-federation into this one on the strength of its certificate: like
// Embed, but Build does not re-verify the subsystem's internal morphisms from their closures; it
// re-checks them from the certificate's tables (M1/R2, ports, acyclicity), verifies the seam
// (morphisms crossing the boundary) and the whole-graph acyclicity/monotonicity, and rebuilds each
// certified component with Build, which re-checks its WFC and CC. The certificate's recorded
// verdict is never trusted. The certificate's digest
// must match the sub at Build. An outer morphism may read the subsystem (subsystem as source) or
// write one of the subsystem's declared input ports; an inbound morphism to any other (sealed)
// variable is rejected. Use Embed for the full-re-verification form.
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
// matches its certificate (digest recomputed over the sub plus the certificate's declared input
// ports) and that the seam is legal: an inbound morphism into a certified subsystem is allowed only
// when every variable it writes is a declared input port of that subsystem.
func (f *Federation) validateCertificates(subOf map[*Registry]int) error {
	for _, ce := range f.certified {
		if ce.cert == nil {
			return fmt.Errorf("gsm: certified embed of %q has a nil certificate", ce.sub.name)
		}
		tables, err := ce.sub.extractTables()
		if err != nil {
			return fmt.Errorf("gsm: cannot digest certified sub-federation %q: %w", ce.sub.name, err)
		}
		got, err := digestComponentsAndTables(ce.sub.comps, tables, ce.sub.allowCycles, ce.cert.InputPorts)
		if err != nil {
			return err
		}
		if got != ce.cert.Digest {
			return fmt.Errorf("gsm: certificate for %q does not match the embedded sub-federation; "+
				"rebuild the certificate from the current subsystem", ce.sub.name)
		}
		// Re-check the morphism conditions from the tables rather than trusting the
		// certificate's verdict. Component convergence is re-checked when Build builds
		// each certified component (with CC).
		byName := make(map[string]*Registry, len(ce.sub.comps))
		for _, r := range ce.sub.comps {
			byName[r.name] = r
		}
		if err := ce.cert.recheckTables(byName); err != nil {
			return err
		}
	}
	// Seam rule: an inbound morphism (target inside a certified sub, source outside it) is allowed
	// only if every variable it writes is a declared input port; a write to a sealed variable is
	// rejected, because the certificate does not cover an external writer of an internal variable.
	for _, e := range f.edges {
		did, dok := subOf[e.dst]
		if !dok {
			continue
		}
		if sid, sok := subOf[e.src]; sok && sid == did {
			continue // internal edge, covered by the certificate
		}
		ce := f.certified[did]
		for _, v := range e.shared {
			if !ce.isInputPort(e.dst.name, v.name) {
				return fmt.Errorf("gsm: morphism %s→%s writes %q into certified sub-federation %q, which is not a "+
					"declared input port; declare it via Certify(gsm.Port{...}) or embed with Embed for full re-verification",
					e.src.name, e.dst.name, v.name, ce.sub.name)
			}
		}
	}
	return nil
}

// isInputPort reports whether (reg, varName) is a declared input port of this certified embed.
func (ce *certifiedEmbed) isInputPort(reg, varName string) bool {
	for _, p := range ce.cert.InputPorts {
		if p.Registry == reg && p.Var == varName {
			return true
		}
	}
	return false
}

// hasSeamIncoming reports whether any of a target's incoming edges comes from outside the target's
// own certified sub (an external writer). Such a target must be re-verified at the seam rather than
// trusted, because the certificate only covers its internal sources.
func hasSeamIncoming(subOf map[*Registry]int, tid int, edges []edgeDef) bool {
	for _, e := range edges {
		if sid, ok := subOf[e.src]; !ok || sid != tid {
			return true
		}
	}
	return false
}

// digestComponentsAndTables computes the certificate digest from the component registries, the
// morphism tables, and the declared input ports. Domain-separated and deterministic: components
// sorted by name, tables and ports sorted by their canonical serialization. Used both to produce a
// certificate (Certify) and to re-check one against a consumer's own components (Certificate.Verify).
func digestComponentsAndTables(comps []*Registry, tables []MorphismTable, allowCycles bool, inputPorts []PortRef) (string, error) {
	h := sha256.New()
	h.Write([]byte("gsm-fedcert-v3\n"))

	cs := append([]*Registry(nil), comps...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	for _, r := range cs {
		b, err := r.PolicyBytes()
		if err != nil {
			return "", fmt.Errorf("gsm: component %q: %w", r.name, err)
		}
		names, err := r.PolicyNames()
		if err != nil {
			return "", fmt.Errorf("gsm: component %q: %w", r.name, err)
		}
		h.Write([]byte(fmt.Sprintf("comp %s\n", r.name)))
		h.Write(b)
		h.Write(names)
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

	prefs := append([]PortRef(nil), inputPorts...)
	sortPortRefs(prefs)
	for _, p := range prefs {
		h.Write([]byte("inport " + p.Registry + "." + p.Var + "\n"))
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
// the target, so a representative valid target suffices. Must be called on a structurally valid
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
		var t MorphismTable
		var err error
		if resolver, ok := f.resolvers[target]; ok {
			t, err = extractResolverTable(target, resolver, edges)
		} else {
			t, err = extractEdgeTable(edges[0])
		}
		if err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, nil
}

// extractEdgeTable reifies a single-source morphism. It returns an error if an image is not
// a state of the target (domainCheck): the closure may be an embedded sub's, which
// Federation.Build does not otherwise run, and the table must not record a value outside a
// variable's range (or read one from a state of another layout).
func extractEdgeTable(e edgeDef) (MorphismTable, error) {
	dstRep := representativeTarget(e.dst)
	dom := newDomainCheck(e.dst.vars)
	t := MorphismTable{Target: e.dst.name, Sources: []string{e.src.name}}
	for _, v := range e.shared {
		t.Shared = append(t.Shared, v.name)
	}
	for _, sa := range e.src.validStates() {
		img := e.mapFn(sa, dstRep)
		if err := dom.imageError(e.describe, e.dst.name, dstRep, img); err != nil {
			return MorphismTable{}, err
		}
		row := TableRow{SourceIDs: []uint64{sa.packed}}
		for _, v := range e.shared {
			row.Values = append(row.Values, img.getRaw(v))
		}
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

// extractResolverTable reifies a multi-source resolver over every valid source combination,
// checking each merge as extractEdgeTable checks each image.
func extractResolverTable(target *Registry, resolver Resolver, edges []edgeDef) (MorphismTable, error) {
	sources, sharedVars := resolverInputs(edges)
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
			return t, nil // a source with no valid states leaves the table empty
		}
	}
	dstRep := representativeTarget(target)
	dom := newDomainCheck(target.vars)
	if err := forEachCombo(srcValids, func(cs []State) error {
		combo := make(map[string]State, len(sources))
		ids := make([]uint64, len(sources))
		for k, s := range sources {
			combo[s.name] = cs[k]
			ids[k] = cs[k].packed
		}
		merged := resolver(dstRep, combo)
		if err := dom.imageError(resolverName(target.name), target.name, dstRep, merged); err != nil {
			return err
		}
		row := TableRow{SourceIDs: ids}
		for _, v := range sharedVars {
			row.Values = append(row.Values, merged.getRaw(v))
		}
		t.Rows = append(t.Rows, row)
		return nil
	}); err != nil {
		return MorphismTable{}, err
	}
	return t, nil
}

// Verify is the independent, differential re-checker a consumer runs on a certificate it received,
// given its own copies of the component registries (which it can digest-match). It re-derives the
// federated conditions from the certificate's morphism tables, NOT from the producer's morphism
// closures, so it confirms the composition without trusting the producer's code:
//
//   - names: comps is keyed by registry name, so each key must equal its registry's name, and no
//     registry may declare two events or two variables with the same name (the tables and the
//     runtime address both by name);
//   - tamper check: the digest recomputed from the provided components and the certificate's tables
//     must equal the certificate's digest;
//   - validity preservation (M1 for single-source, R2 for resolvers): for every table row, writing
//     the recorded shared values into every valid target state must keep the target valid;
//   - input-port freeness: no morphism table writes a declared input port (so the port is genuinely
//     free for an outer morphism to drive);
//   - acyclicity: unless the certificate is marked Monotone, the morphism graph must be acyclic;
//   - component convergence: every provided component is rebuilt with Build, which re-checks WFC
//     and CC exhaustively. The certificate's recorded verdict (Report) is never trusted: a
//     certificate issued by an earlier, weaker verifier for a component that does not converge is
//     refused here even though its digest still matches.
//
// The component invariants are evaluated from the provided registries (part of the shared, digest
// covered definition); only the morphism closures are replaced by the tables. The monotonicity of a
// cyclic (Monotone) certificate is not re-derived here and is left to the component-level oracle.
func (c *Certificate) Verify(comps map[string]*Registry) error {
	// The digest names each component by its registry name and the tables name
	// their targets the same way, so a key must be its registry's name; otherwise
	// a table would be re-checked against a different registry than the digest
	// covers under that name.
	keys := make([]string, 0, len(comps))
	for k := range comps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	list := make([]*Registry, 0, len(comps))
	for _, k := range keys {
		r := comps[k]
		if r.name != k {
			return fmt.Errorf("gsm: certificate %q: component key %q names registry %q", c.Name, k, r.name)
		}
		if err := r.checkNames(); err != nil {
			return err
		}
		list = append(list, r)
	}
	dig, err := digestComponentsAndTables(list, c.Tables, c.Monotone, c.InputPorts)
	if err != nil {
		return err
	}
	if dig != c.Digest {
		return fmt.Errorf("gsm: certificate %q digest does not match the provided components, tables, and ports", c.Name)
	}
	if err := c.recheckTables(comps); err != nil {
		return err
	}
	return c.recheckComponents(comps)
}

// recheckComponents rebuilds every component with Build (exhaustive WFC and CC), in name order.
func (c *Certificate) recheckComponents(comps map[string]*Registry) error {
	names := make([]string, 0, len(comps))
	for n := range comps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, _, err := comps[n].Build(); err != nil {
			return fmt.Errorf("gsm: certificate %q: component %q does not converge on re-check: %w", c.Name, n, err)
		}
	}
	return nil
}

// recheckTables re-derives the federated conditions from the certificate's tables: input-port
// freeness, validity preservation (M1/R2) against the provided target registries, and acyclicity
// unless Monotone. It trusts nothing the producer computed except the tables themselves, which the
// digest binds to the live subsystem.
func (c *Certificate) recheckTables(comps map[string]*Registry) error {
	// Input ports must be free: no morphism table may write a declared input port.
	for _, p := range c.InputPorts {
		for _, t := range c.Tables {
			if t.Target != p.Registry {
				continue
			}
			for _, sv := range t.Shared {
				if sv == p.Var {
					return fmt.Errorf("gsm: certificate %q declares input port %s.%s but a morphism table writes it; "+
						"an input port must be free", c.Name, p.Registry, p.Var)
				}
			}
		}
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
			// The values are data, not closure results: each must be a value of its variable,
			// or writing it would leave the target's domain (or, past the field, corrupt others).
			if len(row.Values) != len(sharedVars) {
				return fmt.Errorf("gsm: certificate table for %q has a row with %d values for %d shared variables",
					t.Target, len(row.Values), len(sharedVars))
			}
			for i, v := range sharedVars {
				if row.Values[i] >= uint64(v.domain) {
					return fmt.Errorf("gsm: certificate table for %q records %s for %q, outside %s",
						t.Target, v.rawLabel(row.Values[i]), v.name, v.describeDomain())
				}
			}
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
