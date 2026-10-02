package gsm

import "testing"

// TestDuplicateVariableName_VerifyRechecksTheWrongVariable: a certificate table
// names shared variables by name. With two variables named "x" in the target, the
// re-check resolves "x" to the last one while the federated runtime's projection
// merge (MergeProjection) resolves it to the first, so Verify can accept a table
// that breaks validity preservation for the variable the runtime writes.
func TestDuplicateVariableName_VerifyRechecksTheWrongVariable(t *testing.T) {
	s := NewRegistry("s")
	on := s.Bool("on")
	s.DeclEvent("flip", Do(Set(on, Lit(1))))
	target := func(second string) *Registry {
		r := NewRegistry("t")
		first := r.Bool("x")
		r.Bool(second)
		r.DeclInvariant("first_x_clear", Eq(V(first), Lit(0)), Do(Set(first, Lit(0))))
		return r
	}
	tr := target("x")

	// The table writes 1 into "x" for every source state: invalid for the first x
	// (the one MergeProjection writes), harmless for the second.
	tables := []MorphismTable{{
		Target: "t", Sources: []string{"s"}, Shared: []string{"x"},
		Rows: []TableRow{{SourceIDs: []uint64{0}, Values: []uint64{1}}, {SourceIDs: []uint64{1}, Values: []uint64{1}}},
	}}
	// The rules serialization names variables by index, so a twin whose second
	// variable is named "y" has the same policy bytes and so the same digest.
	dig, err := digestComponentsAndTables([]*Registry{s, target("y")}, tables, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cert := &Certificate{Name: "forged", Digest: dig, Tables: tables}
	err = cert.Verify(map[string]*Registry{"s": s, "t": tr})
	if err == nil {
		t.Fatal(`Verify accepted a table that writes 1 into "x" of t, which makes t invalid for the first ` +
			`"x" (the variable the runtime merges into); the re-check resolved "x" to the second variable`)
	}
	if want := `gsm: registry "t": duplicate variable name "x"`; err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
}
