package gsm_test

import (
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// mustPanic runs fn and fails unless it panics with a message containing `want`.
func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q, got none", want)
		}
		if msg, ok := r.(string); ok {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic %q does not contain %q", msg, want)
			}
			return
		}
		if err, ok := r.(error); ok {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("panic %q does not contain %q", err.Error(), want)
			}
			return
		}
		t.Fatalf("panic of unexpected type %T: %v", r, r)
	}()
	fn()
}

// tinyMachine builds a one-enum, one-event machine for panic/edge probing.
func tinyMachine(t *testing.T) (*gsm.Machine, gsm.Var) {
	t.Helper()
	r := gsm.NewRegistry("tiny")
	color := r.Enum("color", "red", "green")
	r.Event("go").Writes(color).
		Apply(func(s gsm.State) gsm.State { return s.Set(color, "green") }).Add()
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	return m, color
}

func TestPanic_ApplyUnknownEvent(t *testing.T) {
	m, _ := tinyMachine(t)
	mustPanic(t, "unknown event", func() { m.Apply(m.NewState(), "nope") })
}

func TestPanic_SetInvalidEnumValue(t *testing.T) {
	m, color := tinyMachine(t)
	mustPanic(t, "no value", func() { m.NewState().Set(color, "purple") })
}

func TestPanic_ForeignVariable(t *testing.T) {
	m1, color := tinyMachine(t) // color belongs to m1
	// A second, structurally different machine.
	r2 := gsm.NewRegistry("other")
	r2.Bool("enabled")
	m2, _, err := r2.Build()
	if err != nil {
		t.Fatal(err)
	}
	_ = m1
	// Using m1's Var against m2's state must be caught.
	mustPanic(t, "does not belong", func() { m2.NewState().Get(color) })
}

func TestPanic_EnumTooFewValues(t *testing.T) {
	r := gsm.NewRegistry("bad")
	mustPanic(t, "at least 2 values", func() { r.Enum("solo", "only") })
}

func TestPanic_IntMaxLessThanMin(t *testing.T) {
	r := gsm.NewRegistry("bad")
	mustPanic(t, "max < min", func() { r.Int("range", 5, 1) })
}

func TestPanic_InvariantMissingHolds(t *testing.T) {
	r := gsm.NewRegistry("bad")
	v := r.Bool("v")
	mustPanic(t, "no check function", func() {
		r.Invariant("x").Watches(v).Repair(func(s gsm.State) gsm.State { return s }).Add()
	})
}

// Note: an invariant may be declared without a Repair (that's what Synthesize generates);
// Build rejects a missing Repair with a clear error — see TestBuild_MissingRepairErrors.

func TestPanic_EventMissingEffect(t *testing.T) {
	r := gsm.NewRegistry("bad")
	v := r.Bool("v")
	mustPanic(t, "no effect function", func() { r.Event("e").Writes(v).Add() })
}

func TestPanic_IndependentUnknownEvent(t *testing.T) {
	r := gsm.NewRegistry("bad")
	v := r.Bool("v")
	r.Event("real").Writes(v).Apply(func(s gsm.State) gsm.State { return s }).Add()
	mustPanic(t, "unknown event", func() { r.Independent("real", "ghost") })
}

// --- Non-panic behavioral edges ---

func TestTrySet(t *testing.T) {
	m, color := tinyMachine(t)
	s, err := m.NewState().TrySet(color, "green")
	if err != nil {
		t.Fatalf("TrySet valid value: %v", err)
	}
	if s.Get(color) != "green" {
		t.Fatalf("TrySet result = %s, want green", s.Get(color))
	}
	if _, err := m.NewState().TrySet(color, "purple"); err == nil {
		t.Fatal("TrySet invalid value should error")
	}
}

func TestSetIntClamping(t *testing.T) {
	r := gsm.NewRegistry("clamp")
	n := r.Int("n", 0, 3)
	r.Event("noop").Writes(n).Apply(func(s gsm.State) gsm.State { return s }).Add()
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.NewState().SetInt(n, 99).GetInt(n); got != 3 {
		t.Fatalf("above-max clamp = %d, want 3", got)
	}
	if got := m.NewState().SetInt(n, -99).GetInt(n); got != 0 {
		t.Fatalf("below-min clamp = %d, want 0", got)
	}
}

func TestIntNegativeMinOffset(t *testing.T) {
	r := gsm.NewRegistry("temps")
	temp := r.Int("temp", -5, 5)
	r.Event("noop").Writes(temp).Apply(func(s gsm.State) gsm.State { return s }).Add()
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.NewState().SetInt(temp, -3).GetInt(temp); got != -3 {
		t.Fatalf("negative-min round-trip = %d, want -3", got)
	}
}

// TestGuardNoOp confirms a guarded-out event is a no-op (the state is unchanged).
func TestGuardNoOp(t *testing.T) {
	r := gsm.NewRegistry("guarded")
	open := r.Bool("open")
	r.Event("close").Writes(open).
		Guard(func(s gsm.State) bool { return s.GetBool(open) }). // only fires if open
		Apply(func(s gsm.State) gsm.State { return s.SetBool(open, false) }).Add()
	m, _, err := r.Build()
	if err != nil {
		t.Fatal(err)
	}
	// open starts false → guard fails → close is a no-op.
	start := m.NewState()
	if got := m.Apply(start, "close"); got.ID() != start.ID() {
		t.Fatalf("guarded-out event changed state: %s → %s", start, got)
	}
}

// TestBuildRejectsOversizedStateSpace confirms Build errors (not panics) when the packed
// state exceeds the 20-bit ceiling.
func TestBuildRejectsOversizedStateSpace(t *testing.T) {
	r := gsm.NewRegistry("huge")
	// Three 8-bit ints = 24 bits > 20.
	for _, name := range []string{"x", "y", "z"} {
		v := r.Int(name, 0, 255)
		r.Event("touch_" + name).Writes(v).Apply(func(s gsm.State) gsm.State { return s }).Add()
	}
	if _, _, err := r.Build(); err == nil {
		t.Fatal("expected Build to reject a >20-bit state space")
	}
}

func TestNameAccessors(t *testing.T) {
	m, color := tinyMachine(t)
	if m.Name() != "tiny" {
		t.Fatalf("Machine.Name() = %q, want tiny", m.Name())
	}
	if color.Name() != "color" {
		t.Fatalf("Var.Name() = %q, want color", color.Name())
	}
}
