package gsm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A certificate issued by the pre-fix verifier (v0.11.0) for a non-convergent
// component must be refused by the fixed code on load, because the re-check finds
// the non-convergence, not because of a version or digest mismatch.
//
// testdata/payship-cert-v0.11.0.json was produced by Federation.Certify at
// origin/main f6feaf2 (the v0.11.0 verifier), JSON-encoded. Its component
// pay_ship is the naive pay/ship machine (ship guarded on paid), which the old
// Build certified as convergent through the unchecked disjointness shortcut
// ("PairsDisjoint": 1). The registries below are the same rules, so the
// certificate's digest still matches.

func payShipSub() (sub *Federation, ps, audit *Registry) {
	ps = NewRegistry("pay_ship")
	paid := ps.Bool("paid")
	shipped := ps.Bool("shipped")
	ps.DeclEvent("pay", Do(Set(paid, Lit(1))))
	ps.DeclEventGuarded("ship", Eq(V(paid), Lit(1)), Do(Set(shipped, Lit(1))))
	audit = NewRegistry("audit")
	seen := audit.Bool("seen_shipped")
	sub = NewFederation("pay-ship-sub").
		Morphism(ps, audit).Shared(seen).
		Map(func(src, d State) State { return d.SetBool(seen, src.GetBool(shipped)) }).Add()
	return sub, ps, audit
}

func loadPreFixCert(t *testing.T) *Certificate {
	t.Helper()
	b, err := os.ReadFile("testdata/payship-cert-v0.11.0.json")
	if err != nil {
		t.Fatal(err)
	}
	var cert Certificate
	if err := json.Unmarshal(b, &cert); err != nil {
		t.Fatal(err)
	}
	if !cert.Report.Components[0].CC || cert.Report.Components[0].PairsDisjoint != 1 {
		t.Fatal("test data is not the pre-fix certificate (expected a CC pass by disjointness)")
	}
	return &cert
}

// TestPreFixCertificate_DigestStillMatches pins the premise: the refusal below
// is not a digest or version mismatch.
func TestPreFixCertificate_DigestStillMatches(t *testing.T) {
	cert := loadPreFixCert(t)
	sub, _, _ := payShipSub()
	tables, err := sub.extractTables()
	if err != nil {
		t.Fatal(err)
	}
	got, err := digestComponentsAndTables(sub.comps, tables, sub.allowCycles, cert.InputPorts)
	if err != nil {
		t.Fatal(err)
	}
	if got != cert.Digest {
		t.Fatalf("digest mismatch (%s vs %s): the refusal tests would not isolate the re-check", got, cert.Digest)
	}
}

// TestPreFixCertificate_VerifyRefuses: Certificate.Verify re-checks each
// component's convergence and refuses the certificate.
func TestPreFixCertificate_VerifyRefuses(t *testing.T) {
	cert := loadPreFixCert(t)
	_, ps, audit := payShipSub()
	err := cert.Verify(map[string]*Registry{"pay_ship": ps, "audit": audit})
	if err == nil {
		t.Fatal("Verify accepted a certificate for a non-convergent component")
	}
	if !strings.Contains(err.Error(), "Compensation Commutativity") || strings.Contains(err.Error(), "digest") {
		t.Fatalf("want a refusal from the convergence re-check, got: %v", err)
	}
}

// TestPreFixCertificate_EmbedRefuses: EmbedCertified re-checks a certified
// component's CC when it builds, so the composed federation does not build.
func TestPreFixCertificate_EmbedRefuses(t *testing.T) {
	cert := loadPreFixCert(t)
	sub, _, audit := payShipSub()
	outer := NewRegistry("outer")
	mirror := outer.Bool("mirror")
	seen := audit.vars[0]
	f := NewFederation("system").
		EmbedCertified(sub, cert).
		Morphism(audit, outer).Shared(mirror).
		Map(func(src, d State) State { return d.SetBool(mirror, src.GetBool(seen)) }).Add()
	_, _, err := f.Build()
	if err == nil {
		t.Fatal("EmbedCertified built a federation on a certificate for a non-convergent component")
	}
	if !strings.Contains(err.Error(), "Compensation Commutativity") || strings.Contains(err.Error(), "digest") {
		t.Fatalf("want a refusal from the convergence re-check, got: %v", err)
	}
}
