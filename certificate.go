package gsm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
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
// Independent pair, the wiring, a recorded morphism image, or the port declaration changes. The
// digest binds a morphism or resolver closure only through its table, which records its images at
// one representative target, so a closure that differs only at other targets digests the same.
// EmbedCertified does not rely on the digest for that: the FedMachine executes the certificate's
// tables, not the closures, for the sub's internal morphisms (see EmbedCertified), and at Build it
// also re-runs the live sub's closures over every valid source and target state (write mask,
// validity, source-determinacy), which ties each closure to its table on the whole verified
// domain for the checks and paths that still evaluate closures. A
// consumer can re-check the federated conditions from the tables, independently of the
// producer's morphism closures, via Verify. The digest is an unkeyed SHA-256 over public data: it
// binds the tables to the components (integrity), it does not authenticate who produced them.
//
// Ports (assume-guarantee, Theorem 2' of the categorical note): a certified subsystem may declare
// input ports at Certify time (Certify(ports...)). An input port is a shared variable that no
// internal morphism writes (it is free inside the sub); once embedded, an outer morphism may write
// it, and Build verifies that boundary morphism at the seam (M1/R2) while still skipping the
// closure-level re-verification of the subsystem's internal morphisms. The certificate is inherently parametric over the input port's whole
// domain because Build already verifies every component and source state exhaustively. A shared
// variable not declared an input port stays sealed, and an inbound morphism to it is rejected.
//
// Verify re-derives validity preservation, table completeness, and acyclicity (or, for a cyclic
// Monotone certificate, monotonicity), matches the digest, confirms declared input ports are free
// (no table writes them), and rebuilds every component with Build to re-check its convergence.
// The strongest form,
// an axiom-free-Coq-extracted oracle that re-checks the federated conditions the way astchecker
// re-checks single-registry rules, is future work (it needs the federation conditions mechanized in
// Coq first).
type Certificate struct {
	Name       string          // the certified sub-federation's name; used in messages only, not covered by the digest
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
		if p.Var.index >= len(p.Registry.vars) || !sameVar(p.Registry.vars[p.Var.index], p.Var) {
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
// Embed, but Build also pins the subsystem to its certificate. It re-checks the certificate's
// tables (M1/R2, completeness, ports, acyclicity or monotonicity), re-verifies the live sub's
// internal morphism and resolver closures over every valid source and target state so they
// cannot differ from the tables anywhere on that domain, verifies the seam (morphisms crossing
// the boundary) and the whole-graph acyclicity/monotonicity, and rebuilds each certified
// component with Build, which re-checks its WFC and CC. The certificate's recorded Report is
// never trusted. The certificate's digest must match the sub at Build. An outer morphism may read the subsystem (subsystem as source) or
// write one of the subsystem's declared input ports; an inbound morphism to any other (sealed)
// variable is rejected. Use Embed for the full-re-verification form. As with Embed, the sub's
// AllowMonotoneCycles does not carry over to f, and a second resolver for a target panics.
//
// At run time the machine executes the certificate's verified tables for the sub's internal
// morphisms and resolvers: Apply, Normalize, IsValid and SharedProjection repair each internal
// target by a lookup in the table Build checked (copied at Build, so a later change to cert does
// not reach the machine), and the sub's Map and Resolver closures are not called. The result is
// the same as running the closures on every state the certificate covers (Build binds the
// closures to the tables there), but a side effect in a closure (logging, a counter) does not
// happen at run time. A state outside the certificate's domain (a source or target that is not a
// valid state of its component) makes Normalize and Apply panic, naming it. The exceptions, which
// keep the closures Build verified, are a cyclic network (Kleene iteration evaluates repair on
// states the tables do not cover), a target that also has a writer outside the sub (an input
// port), and SharedProjection along a morphism into a resolved target (the certificate tabulates
// the resolver, not each morphism into it). FedReport.Runtime says which applies.
func (f *Federation) EmbedCertified(sub *Federation, cert *Certificate) *Federation {
	ce := &certifiedEmbed{comps: make(map[*Registry]bool, len(sub.comps)), cert: cert, sub: sub}
	for _, r := range sub.comps {
		f.register(r)
		ce.comps[r] = true
	}
	f.edges = append(f.edges, sub.edges...)
	f.embedResolvers(sub, "EmbedCertified")
	f.noteMonotoneSub(sub)
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
//
// It returns, per certified embed, the snapshot of the certificate's tables it checked: the
// FedMachine executes those tables (compileCertTables), so they are copied once here, bound to the
// digest, and re-checked, and nothing a closure does to the Certificate later in Build (or after
// it) reaches the machine.
func (f *Federation) validateCertificates(subOf map[*Registry]int) ([][]MorphismTable, error) {
	snaps := make([][]MorphismTable, len(f.certified))
	for id, ce := range f.certified {
		if ce.cert == nil {
			return nil, fmt.Errorf("gsm: certified embed of %q has a nil certificate", ce.sub.name)
		}
		snap := snapshotTables(ce.cert.Tables)
		// The tables that will run must be the ones the digest covers. Anyone can recompute an
		// unkeyed digest, so this binds them; the re-check below is what validates them.
		got, err := digestComponentsAndTables(ce.sub.comps, snap, ce.sub.allowCycles, ce.cert.InputPorts)
		if err != nil {
			return nil, err
		}
		if got != ce.cert.Digest {
			return nil, fmt.Errorf("gsm: certificate for %q does not match the embedded sub-federation (its digest does "+
				"not cover its tables over these components); rebuild the certificate from the current subsystem", ce.sub.name)
		}
		// Early mismatch diagnostic: the live sub's closures, reified the same way, must give
		// the same digest. Correctness does not rest on it (the tables run, not the closures),
		// but a sub whose closures were edited after certification is almost always a mistake.
		tables, err := ce.sub.extractTables()
		if err != nil {
			return nil, fmt.Errorf("gsm: cannot digest certified sub-federation %q: %w", ce.sub.name, err)
		}
		got, err = digestComponentsAndTables(ce.sub.comps, tables, ce.sub.allowCycles, ce.cert.InputPorts)
		if err != nil {
			return nil, err
		}
		if got != ce.cert.Digest {
			return nil, fmt.Errorf("gsm: certificate for %q does not match the embedded sub-federation; "+
				"rebuild the certificate from the current subsystem", ce.sub.name)
		}
		// Re-check the morphism conditions from the tables rather than trusting the
		// certificate's verdict. Component convergence is re-checked when Build builds
		// each certified component (with CC).
		byName := make(map[string]*Registry, len(ce.sub.comps))
		for _, r := range ce.sub.comps {
			byName[r.name] = r
		}
		checked := *ce.cert
		checked.Tables = snap
		if err := checked.recheckTables(byName); err != nil {
			return nil, err
		}
		// Closure binding: the live closures must agree with the tables on every valid source
		// and target state. The tables are what Apply runs, so this is not needed for the
		// certified runtime; it is kept because the build-time checks that still evaluate
		// closures (cross-registry order, the monotone-cycle checks) and the closures that
		// still run (a cyclic network's Kleene iteration, SharedProjection along a morphism into
		// a resolved target) then speak for the tables too, and because it names the closure
		// that differs.
		if err := ce.sub.bindClosures(); err != nil {
			return nil, fmt.Errorf("gsm: certified sub-federation %q: an internal morphism closure does not match "+
				"its certificate: %w", ce.sub.name, err)
		}
		if err := f.checkCertifiedWiring(id, ce, subOf); err != nil {
			return nil, err
		}
		snaps[id] = snap
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
				return nil, fmt.Errorf("gsm: morphism %s→%s writes %q into certified sub-federation %q, which is not a "+
					"declared input port; declare it via Certify(gsm.Port{...}) or embed with Embed for full re-verification",
					e.src.name, e.dst.name, v.name, ce.sub.name)
			}
		}
	}
	return snaps, nil
}

// bindClosures re-verifies the live sub's internal morphism and resolver closures over every
// valid source (combination) and every valid target state: each must write only its Shared()
// variables, keep the target valid, and be source-determined. The digest already matched the
// tables, which record each closure's images at the representative target; source-determinacy
// at every valid target then makes the closure agree with its table everywhere on the verified
// domain, so a closure that matches the certificate at one target and differs elsewhere is
// refused here instead of being trusted. The cost is that of verifying the sub with Embed. The
// FedMachine runs the tables, so this is a diagnostic for the certified runtime; it is what
// makes the build-time checks that evaluate closures, and the paths that still run them, agree
// with the tables (see validateCertificates).
func (f *Federation) bindClosures() error {
	inEdges := make(map[*Registry][]edgeDef)
	for _, e := range f.edges {
		inEdges[e.dst] = append(inEdges[e.dst], e)
	}
	for _, target := range f.comps {
		edges := inEdges[target]
		if len(edges) == 0 {
			continue
		}
		if resolver, ok := f.resolvers[target]; ok {
			if err := f.verifyResolved(target, resolver, edges); err != nil {
				return err
			}
			continue
		}
		if err := f.verifyEdge(edges[0]); err != nil {
			return err
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
		h.Write([]byte(fmt.Sprintf("comp %s\n", strconv.Quote(r.name))))
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
		h.Write([]byte("inport " + strconv.Quote(p.Registry) + " " + strconv.Quote(p.Var) + "\n"))
	}
	if allowCycles {
		h.Write([]byte("allowcycles\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// serialize renders a table to a canonical, deterministic string for digesting.
func (t MorphismTable) serialize() string {
	var b strings.Builder
	// Every name is quoted, so no name can carry the framing of another.
	fmt.Fprintf(&b, "table %s <- [%s] shared=[%s]\n", strconv.Quote(t.Target), quoteAll(t.Sources), quoteAll(t.Shared))
	for _, row := range t.Rows {
		fmt.Fprintf(&b, "  %v => %v\n", row.SourceIDs, row.Values)
	}
	return b.String()
}

// quoteAll renders names as space-separated quoted strings.
func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = strconv.Quote(n)
	}
	return strings.Join(q, " ")
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
//   - completeness: every table has exactly one row per valid source state (or combination), every
//     source id is a valid state of its source, and no target has two tables;
//   - input-port freeness: no morphism table writes a declared input port (so the port is genuinely
//     free for an outer morphism to drive);
//   - acyclicity: unless the certificate is marked Monotone, the morphism graph must be acyclic;
//     a cyclic Monotone certificate must have monotone tables (re-derived from the rows);
//   - component convergence: every provided component is rebuilt with Build, which re-checks WFC
//     and CC exhaustively. The certificate's recorded verdict (Report) is never trusted: a
//     certificate issued by an earlier, weaker verifier for a component that does not converge is
//     refused here even though its digest still matches.
//
// The component invariants are evaluated from the provided registries (part of the shared, digest
// covered definition); only the morphism closures are replaced by the tables.
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
// freeness, validity preservation (M1/R2) against the provided target registries, completeness
// (one row per valid source state or combination), and acyclicity unless Monotone, in which case
// a cycle requires monotone tables. It trusts nothing the producer computed except the tables themselves, which the
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
		if err := c.checkTableRows(t, comps); err != nil {
			return err
		}
	}

	// One table per target: a target written by several sources has one resolver table, so a
	// second table for the same target is a writer the certificate's conditions never covered.
	seenTarget := make(map[string]bool, len(c.Tables))
	for _, t := range c.Tables {
		if seenTarget[t.Target] {
			return fmt.Errorf("gsm: certificate %q has more than one table for target %q", c.Name, t.Target)
		}
		seenTarget[t.Target] = true
	}

	if tablesHaveCycle(c.Tables) {
		if !c.Monotone {
			return fmt.Errorf("gsm: certificate %q morphism graph has a cycle but is not marked monotone", c.Name)
		}
		// A cyclic network converges only under monotone repair (Federation.Build checks it
		// with verifyMonotone); re-derive it from the tables rather than trust the flag.
		if err := c.recheckMonotone(comps); err != nil {
			return err
		}
	}
	return nil
}

// checkTableRows checks that t is a complete extensional table: every source names a provided
// registry, every row has one id per source, each id is a valid state of its source, no source
// (combination) appears twice, and every valid source (combination) has a row. M1/R2 "from the
// tables" means nothing for a source state the table leaves out, and the digest alone cannot
// tell a truncated table from a faithful one (anyone can recompute it).
func (c *Certificate) checkTableRows(t MorphismTable, comps map[string]*Registry) error {
	valid := make([]map[uint64]bool, len(t.Sources))
	want := 1
	for i, name := range t.Sources {
		src, ok := comps[name]
		if !ok {
			return fmt.Errorf("gsm: certificate table for %q names source registry %q not present in the provided components",
				t.Target, name)
		}
		vs := src.validStates()
		valid[i] = make(map[uint64]bool, len(vs))
		for _, s := range vs {
			valid[i][s.packed] = true
		}
		if want > 0 && len(vs) > 0 && want > maxStateSpace/len(vs) {
			return fmt.Errorf("gsm: certificate table for %q: source space exceeds %d combinations", t.Target, maxStateSpace)
		}
		want *= len(vs)
	}
	if len(t.Sources) == 0 {
		want = 0
	}
	seen := make(map[string]bool, len(t.Rows))
	for _, row := range t.Rows {
		if len(row.SourceIDs) != len(t.Sources) {
			return fmt.Errorf("gsm: certificate table for %q has a row with %d source ids for %d sources",
				t.Target, len(row.SourceIDs), len(t.Sources))
		}
		for i, id := range row.SourceIDs {
			if !valid[i][id] {
				return fmt.Errorf("gsm: certificate table for %q has a row with source id %d, which is not a valid state of %q",
					t.Target, id, t.Sources[i])
			}
		}
		key := fmt.Sprint(row.SourceIDs)
		if seen[key] {
			return fmt.Errorf("gsm: certificate table for %q has two rows for source ids %v", t.Target, row.SourceIDs)
		}
		seen[key] = true
	}
	if len(t.Rows) != want {
		return fmt.Errorf("gsm: certificate table for %q has %d rows but its sources have %d valid states (combinations); "+
			"a table must have one row per valid source state", t.Target, len(t.Rows), want)
	}
	return nil
}

// recheckMonotone re-derives, from the tables, the monotonicity hypothesis of the Monotone
// Convergence Despite Cycles theorem: for every two rows whose source states are ordered
// componentwise (P ⊑ P'), the recorded shared values must be ordered too. It is the table form
// of verifyMonotone, under the same combination bound. Rows are complete (checkTableRows), so
// this covers every pair of valid source combinations.
func (c *Certificate) recheckMonotone(comps map[string]*Registry) error {
	for _, t := range c.Tables {
		if len(t.Rows) > monotoneGuard {
			return fmt.Errorf("gsm: certificate %q: monotonicity re-check for %q: source space exceeds %d combinations",
				c.Name, t.Target, monotoneGuard)
		}
		points := make([][]State, len(t.Rows))
		for k, row := range t.Rows {
			points[k] = make([]State, len(t.Sources))
			for i, name := range t.Sources {
				points[k][i] = State{packed: row.SourceIDs[i], vars: comps[name].vars}
			}
		}
		for a := range t.Rows {
			for b := range t.Rows {
				if pointsLE(points[a], points[b]) && !rawLE(t.Rows[a].Values, t.Rows[b].Values) {
					return fmt.Errorf("gsm: certificate %q: the table for %q is not monotone (source ids %v ⊑ %v but "+
						"values %v ⋢ %v); a cyclic certificate requires monotone morphisms/resolvers",
						c.Name, t.Target, t.Rows[a].SourceIDs, t.Rows[b].SourceIDs, t.Rows[a].Values, t.Rows[b].Values)
				}
			}
		}
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
