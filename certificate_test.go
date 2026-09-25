package gsm

import (
	"strings"
	"testing"
)

// buildPricingCatalogSub returns the reusable pricing→catalog sub-federation used by the
// certificate tests. Component rules use the combinator surface (DeclEvent) so their policies are
// serializable and digestible; the morphism Map stays a closure (subDigest excludes it).
func buildPricingCatalogSub() (sub *Federation, pricing, catalog *Registry, tier, badge Var) {
	pricing = NewRegistry("pricing")
	tier = pricing.Int("tier", 0, 1)
	pricing.DeclEvent("upgrade", SetTo(tier, 1))
	catalog = NewRegistry("catalog")
	badge = catalog.Int("badge", 0, 1)
	sub = NewFederation("pricing-sub").
		Morphism(pricing, catalog).Shared(badge).
		Map(func(srcNF, d State) State { return d.SetInt(badge, srcNF.GetInt(tier)) }).Add()
	return sub, pricing, catalog, tier, badge
}

// TestEmbedCertified_MatchesEmbed certifies the pricing→catalog subsystem, embeds it on
// certificate into a larger system, connects it to an order registry, and checks the composed
// machine behaves exactly like the full-re-verification Embed: internal and boundary propagation
// both fire and the composed state is federally valid.
func TestEmbedCertified_MatchesEmbed(t *testing.T) {
	sub, pricing, catalog, _, badge := buildPricingCatalogSub()

	cert, err := sub.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	if cert.Digest == "" || cert.Report == nil {
		t.Fatal("certificate must carry a digest and a report")
	}

	order := NewRegistry("order")
	perk := order.Int("perk", 0, 1)
	outer := NewFederation("system").
		EmbedCertified(sub, cert).
		Morphism(catalog, order).Shared(perk).
		Map(func(srcNF, d State) State { return d.SetInt(perk, srcNF.GetInt(badge)) }).Add()

	m, rep, err := outer.Build()
	if err != nil {
		t.Fatalf("certified embed failed to build: %v\n%s", err, rep)
	}
	if len(m.Registries()) != 3 {
		t.Fatalf("composed federation has %d components, want 3", len(m.Registries()))
	}

	s := m.Apply(m.NewState(), pricing, "upgrade")
	if got := m.Of(s, catalog).GetInt(badge); got != 1 {
		t.Fatalf("catalog badge = %d, want 1 (propagated inside the certified subsystem)", got)
	}
	if got := m.Of(s, order).GetInt(perk); got != 1 {
		t.Fatalf("order perk = %d, want 1 (propagated across the boundary)", got)
	}
	if !m.IsValid(s) {
		t.Fatal("composed state is not federally valid")
	}
}

// TestEmbedCertified_DigestMismatchRejected checks Build rejects a certificate that does not match
// the embedded subsystem, so a stale or wrong certificate cannot be silently trusted.
func TestEmbedCertified_DigestMismatchRejected(t *testing.T) {
	sub, _, catalog, _, badge := buildPricingCatalogSub()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	cert.Digest = "0000000000000000000000000000000000000000000000000000000000000000" // tampered

	order := NewRegistry("order")
	perk := order.Int("perk", 0, 1)
	outer := NewFederation("system").
		EmbedCertified(sub, cert).
		Morphism(catalog, order).Shared(perk).
		Map(func(srcNF, d State) State { return d.SetInt(perk, srcNF.GetInt(badge)) }).Add()

	if _, _, err = outer.Build(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected a digest-mismatch error, got %v", err)
	}
}

// TestEmbedCertified_InboundWriteRejected checks the output-port restriction: an outer morphism
// that writes into a certified subsystem is rejected (writing in needs the input-port extension).
func TestEmbedCertified_InboundWriteRejected(t *testing.T) {
	sub, _, catalog, _, badge := buildPricingCatalogSub()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}

	ext := NewRegistry("ext")
	ext.Int("flag", 0, 1)
	outer := NewFederation("system").
		EmbedCertified(sub, cert).
		Morphism(ext, catalog).Shared(badge).
		Map(func(srcNF, d State) State { return d.SetInt(badge, 1) }).Add()

	if _, _, err = outer.Build(); err == nil || !strings.Contains(err.Error(), "not a declared input port") {
		t.Fatalf("expected an inbound-write rejection, got %v", err)
	}
}

// TestEmbedCertified_InputPortAccepted certifies a subsystem that declares an input port (a free
// variable no internal morphism writes), embeds it, and connects an outer morphism that writes that
// port. Build accepts it (verifying the boundary morphism at the seam) and the write propagates.
func TestEmbedCertified_InputPortAccepted(t *testing.T) {
	pricing := NewRegistry("pricing")
	tier := pricing.Int("tier", 0, 1)
	pricing.DeclEvent("upgrade", SetTo(tier, 1))
	catalog := NewRegistry("catalog")
	badge := catalog.Int("badge", 0, 1)
	inbox := NewRegistry("inbox")
	msg := inbox.Int("msg", 0, 1) // free inside the sub: an input port
	sub := NewFederation("sub").
		Add(inbox).
		Morphism(pricing, catalog).Shared(badge).
		Map(func(srcNF, d State) State { return d.SetInt(badge, srcNF.GetInt(tier)) }).Add()

	cert, err := sub.Certify(Port{Registry: inbox, Var: msg})
	if err != nil {
		t.Fatalf("Certify with input port: %v", err)
	}
	if len(cert.InputPorts) != 1 || cert.InputPorts[0].Registry != "inbox" || cert.InputPorts[0].Var != "msg" {
		t.Fatalf("certificate should declare inbox.msg as an input port, got %+v", cert.InputPorts)
	}

	geo := NewRegistry("geo")
	loc := geo.Int("loc", 0, 1)
	geo.DeclEvent("set", SetTo(loc, 1))
	outer := NewFederation("system").
		EmbedCertified(sub, cert).
		Morphism(geo, inbox).Shared(msg).
		Map(func(srcNF, d State) State { return d.SetInt(msg, srcNF.GetInt(loc)) }).Add()

	m, _, err := outer.Build()
	if err != nil {
		t.Fatalf("input-port embed failed to build: %v", err)
	}
	s := m.Apply(m.NewState(), geo, "set")
	if got := m.Of(s, inbox).GetInt(msg); got != 1 {
		t.Fatalf("inbox.msg = %d, want 1 (written through the input port)", got)
	}
	if !m.IsValid(s) {
		t.Fatal("composed state is not federally valid")
	}
}

// TestCertify_InputPortNotFreeRejected checks Certify rejects declaring an input port on a variable
// an internal morphism already writes (it is an output the sub owns, not a free input).
func TestCertify_InputPortNotFreeRejected(t *testing.T) {
	sub, _, catalog, _, badge := buildPricingCatalogSub()
	if _, err := sub.Certify(Port{Registry: catalog, Var: badge}); err == nil || !strings.Contains(err.Error(), "must be free") {
		t.Fatalf("expected an input-port-not-free rejection, got %v", err)
	}
}

// TestCertificate_VerifyFromTables runs the independent differential re-checker: a consumer holding
// the certificate and its own copies of the component registries confirms the federated conditions
// from the extracted tables, without the producer's morphism closures.
func TestCertificate_VerifyFromTables(t *testing.T) {
	sub, _, _, _, _ := buildPricingCatalogSub()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	if len(cert.Tables) != 1 || cert.Tables[0].Target != "catalog" {
		t.Fatalf("expected one table targeting catalog, got %+v", cert.Tables)
	}

	// The consumer's own copies of the components (same definitions), mapped by name.
	cons, _, _, _, _ := buildPricingCatalogSub()
	comps := map[string]*Registry{}
	for _, r := range cons.comps {
		comps[r.name] = r
	}

	if err := cert.Verify(comps); err != nil {
		t.Fatalf("Verify should accept a faithful certificate: %v", err)
	}

	// Tamper a recorded shared value: the digest must no longer match.
	bad := *cert
	bad.Tables = append([]MorphismTable(nil), cert.Tables...)
	badRows := append([]TableRow(nil), bad.Tables[0].Rows...)
	badRows[len(badRows)-1].Values = append([]uint64(nil), badRows[len(badRows)-1].Values...)
	badRows[len(badRows)-1].Values[0] ^= 1 // flip a bit
	bad.Tables[0].Rows = badRows
	if err := bad.Verify(comps); err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("expected a digest-mismatch on a tampered table, got %v", err)
	}
}

// TestCertificate_VerifyCatchesInvalidTable checks the re-checker rejects a table whose recorded
// image violates the target's invariants (validity preservation), even when the digest is made to
// match, so the M1/R2 re-check has teeth independent of the digest.
func TestCertificate_VerifyCatchesInvalidTable(t *testing.T) {
	// Target with an invariant that badge must be 0; a morphism that writes badge=1 violates it.
	pricing := NewRegistry("pricing")
	tier := pricing.Int("tier", 0, 1)
	pricing.DeclEvent("upgrade", SetTo(tier, 1))
	catalog := NewRegistry("catalog")
	badge := catalog.Int("badge", 0, 1)
	catalog.DeclInvariant("badge_zero", Is(badge, 0), SetTo(badge, 0))

	// A morphism that copies tier into badge would break M1 (tier=1 -> badge=1, invalid), so a
	// faithful Build would reject it. Build the certificate for the safe (identity-to-0) morphism
	// instead, then hand Verify a hand-built table that writes the invalid value.
	sub := NewFederation("sub").
		Morphism(pricing, catalog).Shared(badge).
		Map(func(srcNF, d State) State { return d.SetInt(badge, 0) }).Add()
	cert, err := sub.Certify()
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}

	comps := map[string]*Registry{"pricing": pricing, "catalog": catalog}
	// Corrupt a row to the invalid value, then re-align the digest so only the M1 check can catch it.
	cert.Tables[0].Rows[len(cert.Tables[0].Rows)-1].Values[0] = 1
	dig, err := digestComponentsAndTables([]*Registry{pricing, catalog}, cert.Tables, cert.Monotone, cert.InputPorts)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	cert.Digest = dig

	if err := cert.Verify(comps); err == nil || !strings.Contains(err.Error(), "validity preservation") {
		t.Fatalf("expected a validity-preservation rejection, got %v", err)
	}
}
