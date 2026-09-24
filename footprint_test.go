package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// TestFootprint_RejectsUndeclaredWrite: an event that writes a variable it did not
// declare in Writes() must be rejected by BuildCompositional.
func TestFootprint_RejectsUndeclaredWrite(t *testing.T) {
	r := gsm.NewRegistry("undeclared-write")
	a := r.Int("a", 0, 7)
	b := r.Int("b", 0, 7)
	r.Invariant("acap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, 5) }).Add()
	r.Invariant("bcap").Watches(b).
		Holds(func(s gsm.State) bool { return s.GetInt(b) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(b, 5) }).Add()
	// Declares Writes(a) but also mutates b.
	r.Event("inca").Writes(a).
		Apply(func(s gsm.State) gsm.State {
			s = s.SetInt(a, s.GetInt(a)+1)
			return s.SetInt(b, s.GetInt(b)+1)
		}).Add()

	_, _, err := r.BuildCompositional()
	if err == nil {
		t.Fatal("expected rejection: event writes undeclared variable b")
	}
	if !strings.Contains(err.Error(), "outside its declared footprint") {
		t.Fatalf("expected a footprint error, got: %v", err)
	}
}

// TestFootprint_RejectsUndeclaredRead: an event whose declared output depends on a
// variable outside its footprint must be rejected.
func TestFootprint_RejectsUndeclaredRead(t *testing.T) {
	r := gsm.NewRegistry("undeclared-read")
	a := r.Int("a", 0, 7)
	b := r.Int("b", 0, 7)
	r.Invariant("acap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, 5) }).Add()
	r.Invariant("bcap").Watches(b).
		Holds(func(s gsm.State) bool { return s.GetInt(b) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(b, 5) }).Add()
	// Declares Writes(a) with footprint {a}, but computes a from b.
	r.Event("copyb").Writes(a).
		Apply(func(s gsm.State) gsm.State { return s.SetInt(a, s.GetInt(b)) }).Add()

	_, _, err := r.BuildCompositional()
	if err == nil {
		t.Fatal("expected rejection: event reads undeclared variable b")
	}
	if !strings.Contains(err.Error(), "outside its declared footprint") {
		t.Fatalf("expected a footprint error, got: %v", err)
	}
}

// TestFootprint_AcceptsConformingClosures: closures that respect their declared
// footprints pass, and the report records that footprints were verified.
func TestFootprint_AcceptsConformingClosures(t *testing.T) {
	r := gsm.NewRegistry("conforming")
	a := r.Int("a", 0, 7)
	b := r.Int("b", 0, 7)
	r.Invariant("acap").Watches(a).
		Holds(func(s gsm.State) bool { return s.GetInt(a) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(a, 5) }).Add()
	r.Invariant("bcap").Watches(b).
		Holds(func(s gsm.State) bool { return s.GetInt(b) <= 5 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(b, 5) }).Add()
	r.Event("inca").Writes(a).Apply(func(s gsm.State) gsm.State { return s.SetInt(a, s.GetInt(a)+1) }).Add()
	r.Event("incb").Writes(b).Apply(func(s gsm.State) gsm.State { return s.SetInt(b, s.GetInt(b)+1) }).Add()

	_, rep, err := r.BuildCompositional()
	if err != nil {
		t.Fatalf("conforming machine rejected: %v", err)
	}
	if !rep.FootprintChecked {
		t.Fatal("expected FootprintChecked to be true")
	}
}
