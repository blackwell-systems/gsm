package gsm

import (
	"errors"
	"strings"
	"testing"
)

// Regression tests for the certificate and projection trust points: EmbedCertified binding the
// live sub's closures, Certificate.Verify checking table completeness, source ids and (for cyclic
// Monotone certificates) monotonicity, and MergeProjectionAfter refusing stale projections.

// embedNonSharedSubs returns a certificate for a clean src→dst Map and a second sub over the same
// registries whose Map agrees at the representative target (local=false) but, when local is
// set, also clears local (a non-shared variable) and zeroes y.
func embedNonSharedSubs(t *testing.T) (good, bad *Federation, cert *Certificate) {
	t.Helper()
	src := NewRegistry("src")
	x := src.Int("x", 0, 2)
	src.DeclEvent("inc", Inc(x))
	dst := NewRegistry("dst")
	y := dst.Int("y", 0, 2)
	loc := dst.Bool("local")
	dst.DeclEvent("touch", Raise(loc))

	good = NewFederation("sub")
	good.Morphism(src, dst).Shared(y).Map(func(s, d State) State { return d.SetInt(y, s.GetInt(x)) }).Add()
	cert, err := good.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	bad = NewFederation("sub")
	bad.Morphism(src, dst).Shared(y).Map(func(s, d State) State {
		out := d.SetInt(y, s.GetInt(x))
		if d.GetBool(loc) {
			out = out.SetBool(loc, false).SetInt(y, 0)
		}
		return out
	}).Add()
	return good, bad, cert
}

// TestEmbedCertified_ClosureWritingNonSharedRejected (audit #6, probe t8_embed): a Map that
// matches its certificate at the representative target but writes a non-shared variable at
// another target is refused at Build, as plain Build refuses it.
func TestEmbedCertified_ClosureWritingNonSharedRejected(t *testing.T) {
	good, bad, cert := embedNonSharedSubs(t)
	if _, _, err := bad.Build(); err == nil || !strings.Contains(err.Error(), "non-shared variable") {
		t.Fatalf("plain Build of the bad sub: got %v, want a non-shared write rejection", err)
	}
	_, _, err := NewFederation("outer").EmbedCertified(bad, cert).Build()
	if err == nil || !strings.Contains(err.Error(), "does not match its certificate") ||
		!strings.Contains(err.Error(), "non-shared variable") {
		t.Fatalf("EmbedCertified(bad, cert).Build(): got %v, want a non-shared write rejection", err)
	}
	if _, _, err := NewFederation("outer").EmbedCertified(good, cert).Build(); err != nil {
		t.Fatalf("EmbedCertified(good, cert).Build(): %v", err)
	}
}

// TestFedMachine_RuntimeWriteMask (audit #6): the FedMachine refuses, where it computes it, an
// image that differs from the target state outside the target's shared variables.
func TestFedMachine_RuntimeWriteMask(t *testing.T) {
	good, _, _ := embedNonSharedSubs(t)
	fm, _, err := good.Build()
	if err != nil {
		t.Fatal(err)
	}
	j := fm.idx[good.comps[1]]
	c := fm.comps[j]
	dst := c.Normalize(c.NewState())
	what := func() string { return "morphism src→dst Map" }
	// Writing the shared variable is allowed.
	if out := fm.mustBeTarget(j, what, dst, dst.setRaw(c.vars[0], 2)); out.getRaw(c.vars[0]) != 2 {
		t.Fatalf("shared write lost: %s", out)
	}
	msg := catchPanic(func() { fm.mustBeTarget(j, what, dst, dst.setRaw(c.vars[1], 1)) })
	if !strings.Contains(msg, `modifies non-shared variable "local"`) {
		t.Fatalf("non-shared write not refused at runtime: %q", msg)
	}
}

// srcDstCert returns a certificate with one complete table (src x 0..3 → dst y) and the
// components it covers.
func srcDstCert(t *testing.T) (*Certificate, *Registry, *Registry) {
	t.Helper()
	src := NewRegistry("src")
	x := src.Int("x", 0, 3)
	src.DeclEvent("inc", Inc(x))
	dst := NewRegistry("dst")
	y := dst.Int("y", 0, 3)
	dst.DeclEvent("noop", Copy(y, y))
	f := NewFederation("sub")
	f.Morphism(src, dst).Shared(y).Map(func(s, d State) State { return d.SetInt(y, s.GetInt(x)) }).Add()
	cert, err := f.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	if len(cert.Tables) != 1 || len(cert.Tables[0].Rows) != 4 {
		t.Fatalf("want one 4-row table, got %+v", cert.Tables)
	}
	return cert, src, dst
}

// forge returns a copy of cert with the given tables and the digest recomputed over them, as
// anyone can (the digest is unkeyed).
func forge(t *testing.T, cert *Certificate, comps []*Registry, tables []MorphismTable) *Certificate {
	t.Helper()
	out := *cert
	out.Tables = tables
	dig, err := digestComponentsAndTables(comps, tables, out.Monotone, out.InputPorts)
	if err != nil {
		t.Fatal(err)
	}
	out.Digest = dig
	return &out
}

// TestCertificateVerify_TableCompleteness (audit #10, probe t7_cert): Verify refuses a table cut
// to fewer rows than valid source states, a row with an unknown source id, a duplicated row,
// and a second table for the same target, even with the digest recomputed.
func TestCertificateVerify_TableCompleteness(t *testing.T) {
	cert, src, dst := srcDstCert(t)
	comps := map[string]*Registry{"src": src, "dst": dst}
	regs := []*Registry{src, dst}
	if err := cert.Verify(comps); err != nil {
		t.Fatalf("Verify(original): %v", err)
	}
	orig := cert.Tables[0]
	withRows := func(rows []TableRow) MorphismTable {
		tb := orig
		tb.Rows = rows
		return tb
	}

	cases := []struct {
		name   string
		tables []MorphismTable
		want   string
	}{
		{"truncated", []MorphismTable{withRows(orig.Rows[:1])}, "has 1 rows but its sources have 4 valid states"},
		{"bogus source id", []MorphismTable{withRows([]TableRow{{SourceIDs: []uint64{999}, Values: []uint64{0}}})},
			"source id 999, which is not a valid state"},
		{"duplicate row", []MorphismTable{withRows([]TableRow{orig.Rows[0], orig.Rows[0], orig.Rows[2], orig.Rows[3]})},
			"two rows for source ids"},
		{"wrong arity", []MorphismTable{withRows([]TableRow{{SourceIDs: []uint64{0, 0}, Values: []uint64{0}}})},
			"2 source ids for 1 sources"},
		{"two tables for one target", []MorphismTable{orig, orig}, "more than one table for target"},
	}
	for _, tc := range cases {
		forged := forge(t, cert, regs, tc.tables)
		err := forged.Verify(comps)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Verify = %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}

// cyclicCert builds a cyclic Monotone certificate over A(a) ⇄ B(b) whose B→A table is negation
// when negate is set (not monotone) and identity otherwise.
func cyclicCert(t *testing.T, negate bool) (*Certificate, map[string]*Registry) {
	t.Helper()
	a := NewRegistry("A")
	a.Bool("a")
	b := NewRegistry("B")
	b.Bool("b")
	back := []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{0}}, {SourceIDs: []uint64{1}, Values: []uint64{1}}}
	if negate {
		back = []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{1}}, {SourceIDs: []uint64{1}, Values: []uint64{0}}}
	}
	tables := []MorphismTable{
		{Target: "B", Sources: []string{"A"}, Shared: []string{"b"},
			Rows: []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{0}}, {SourceIDs: []uint64{1}, Values: []uint64{1}}}},
		{Target: "A", Sources: []string{"B"}, Shared: []string{"a"}, Rows: back},
	}
	cert := forge(t, &Certificate{Name: "cyc", Monotone: true}, []*Registry{a, b}, tables)
	return cert, map[string]*Registry{"A": a, "B": b}
}

// TestCertificateVerify_MonotoneRechecked (audit #10): a cyclic certificate marked Monotone must
// have monotone tables; the flag alone no longer suffices.
func TestCertificateVerify_MonotoneRechecked(t *testing.T) {
	cert, comps := cyclicCert(t, true)
	if err := cert.Verify(comps); err == nil || !strings.Contains(err.Error(), "is not monotone") {
		t.Fatalf("Verify(cyclic negation marked Monotone) = %v, want a monotonicity rejection", err)
	}
	cert, comps = cyclicCert(t, false)
	if err := cert.Verify(comps); err != nil {
		t.Fatalf("Verify(cyclic identity marked Monotone) = %v, want nil", err)
	}
}

// TestCertify_MonotoneCyclicRoundTrip: a real AllowMonotoneCycles certificate still verifies and
// embeds (into a parent that opts in itself) after the completeness and monotonicity re-checks.
func TestCertify_MonotoneCyclicRoundTrip(t *testing.T) {
	a := NewRegistry("A")
	av := a.Bool("a")
	a.DeclEvent("raise_a", Raise(av))
	b := NewRegistry("B")
	bv := b.Bool("b")
	b.DeclEvent("raise_b", Raise(bv))
	sub := NewFederation("cyc").AllowMonotoneCycles().
		Morphism(a, b).Shared(bv).Map(func(s, d State) State { return d.SetBool(bv, s.GetBool(av)) }).Add().
		Morphism(b, a).Shared(av).Map(func(s, d State) State { return d.SetBool(av, s.GetBool(bv)) }).Add()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	if err := cert.Verify(map[string]*Registry{"A": a, "B": b}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// The sub's AllowMonotoneCycles opt-in does not carry over to the parent: the parent
	// must opt in itself for the embedded cycle to build.
	if _, _, err := NewFederation("outer").EmbedCertified(sub, cert).Build(); err == nil ||
		!strings.Contains(err.Error(), "AllowMonotoneCycles") {
		t.Fatalf("EmbedCertified Build without the parent's opt-in = %v, want a cycle rejection", err)
	}
	if _, _, err := NewFederation("outer").AllowMonotoneCycles().EmbedCertified(sub, cert).Build(); err != nil {
		t.Fatalf("EmbedCertified Build: %v", err)
	}
}

// TestMergeProjectionAfter_StaleRejected (audit #8): the source sends p1 (listed) then p2
// (stale); the transport reorders them. MergeProjection lets the older p1 win; with versions,
// MergeProjectionAfter refuses p1 after p2 and the target keeps the newer value.
func TestMergeProjectionAfter_StaleRejected(t *testing.T) {
	m, mfr, sup, mstate, sstate := buildManufacturerSupplier(t)
	srcM, dstM := m.Component(mfr), m.Component(sup)

	active := srcM.Apply(srcM.NewState(), "epub")
	p1, err := m.SharedProjection(active, mfr, sup)
	if err != nil {
		t.Fatal(err)
	}
	p1.Version = 1
	p2, err := m.SharedProjection(active.Set(mstate, "suspended"), mfr, sup)
	if err != nil {
		t.Fatal(err)
	}
	p2.Version = 2

	// Unversioned merge: the reordered, older p1 silently wins.
	s, err := dstM.MergeProjection(dstM.NewState(), p2)
	if err != nil {
		t.Fatal(err)
	}
	if s, err = dstM.MergeProjection(s, p1); err != nil {
		t.Fatal(err)
	}
	if s.Get(sstate) != "listed" {
		t.Fatalf("baseline: want the stale value to win under MergeProjection, got %s", s.Get(sstate))
	}

	// Versioned merge: p2 applies, then p1 is refused and the state is unchanged.
	var last uint64
	s, err = dstM.MergeProjectionAfter(dstM.NewState(), p2, last)
	if err != nil {
		t.Fatal(err)
	}
	last = p2.Version
	s2, err := dstM.MergeProjectionAfter(s, p1, last)
	if !errors.Is(err, ErrStaleProjection) {
		t.Fatalf("MergeProjectionAfter(older p1) = %v, want ErrStaleProjection", err)
	}
	if s2.ID() != s.ID() || s2.Get(sstate) != "stale" {
		t.Fatalf("a refused projection changed the state: %s", s2.Get(sstate))
	}
	// A redelivered p2 is a duplicate and is refused too.
	if _, err := dstM.MergeProjectionAfter(s, p2, last); !errors.Is(err, ErrStaleProjection) {
		t.Fatalf("MergeProjectionAfter(duplicate p2) = %v, want ErrStaleProjection", err)
	}
}

// TestMergeProjectionAfter_Provenance (audit #8): an unversioned projection and one addressed to
// another target are refused.
func TestMergeProjectionAfter_Provenance(t *testing.T) {
	m, mfr, sup, _, _ := buildManufacturerSupplier(t)
	dstM := m.Component(sup)
	p, err := m.SharedProjection(m.Component(mfr).NewState(), mfr, sup)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 0 {
		t.Fatalf("SharedProjection Version = %d, want 0", p.Version)
	}
	if _, err := dstM.MergeProjectionAfter(dstM.NewState(), p, 0); err == nil || !strings.Contains(err.Error(), "no Version") {
		t.Fatalf("unversioned: got %v", err)
	}
	p.Version = 1
	if _, err := dstM.MergeProjectionAfter(dstM.NewState(), p, 0); err != nil {
		t.Fatalf("fresh versioned projection: %v", err)
	}
	p.To = "manufacturer"
	if _, err := dstM.MergeProjectionAfter(dstM.NewState(), p, 0); err == nil || !strings.Contains(err.Error(), "addressed to") {
		t.Fatalf("misaddressed: got %v", err)
	}
}
