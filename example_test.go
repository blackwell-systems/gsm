package gsm_test

import (
	"fmt"

	"github.com/blackwell-systems/gsm"
)

// Example demonstrates the complete workflow: define state variables,
// invariants, and events, then verify convergence guarantees at build time.
// Runtime event application is O(1) table lookup with zero overhead.
func Example() {
	b := gsm.NewBuilder("counter")

	// State variable with bounded domain
	count := b.Int("count", 0, 10)

	// Business rule: count cannot exceed maximum
	b.Invariant("cap_at_10").
		Over(count).
		Check(func(s gsm.State) bool {
			return s.GetInt(count) <= 10
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(count, 10) // Clamp to maximum
		}).
		Add()

	// Event: increment counter
	b.Event("increment").
		Writes(count).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(count, s.GetInt(count)+1)
		}).
		Add()

	// Build with verification
	machine, report, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
	}

	fmt.Printf("Convergence: %v\n", report.WFC && report.CC)

	// Runtime usage - increment beyond limit
	s := machine.NewState()
	for i := 0; i < 15; i++ {
		s = machine.Apply(s, "increment")
	}

	fmt.Printf("Count: %d\n", s.GetInt(count))
	// Output:
	// Convergence: true
	// Count: 10
}

// ExampleBuilder_Build shows the verification process and report output.
// The builder exhaustively enumerates the state space and verifies
// WFC (well-founded compensation) and CC (compensation commutativity).
func ExampleBuilder_Build() {
	b := gsm.NewBuilder("counter")

	// Single integer variable
	count := b.Int("count", 0, 10)

	// Invariant: count must stay <= 10
	b.Invariant("cap_at_10").
		Over(count).
		Check(func(s gsm.State) bool {
			return s.GetInt(count) <= 10
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(count, 10) // Clamp to max
		}).
		Add()

	// Increment event
	b.Event("increment").
		Writes(count).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(count, s.GetInt(count)+1)
		}).
		Add()

	machine, report, err := b.Build()
	if err != nil {
		panic(err)
	}

	fmt.Printf("States verified: %d\n", report.StateCount)
	fmt.Printf("WFC: %v\n", report.WFC)
	fmt.Printf("CC: %v\n", report.CC)

	// Test the machine
	s := machine.NewState()
	for i := 0; i < 15; i++ {
		s = machine.Apply(s, "increment")
	}
	fmt.Printf("Count after 15 increments: %d\n", s.GetInt(count))

	// Output:
	// States verified: 11
	// WFC: true
	// CC: true
	// Count after 15 increments: 10
}

// ExampleMachine_Apply demonstrates O(1) event application at runtime.
// All compensation is precomputed during Build() - no runtime overhead.
func ExampleMachine_Apply() {
	b := gsm.NewBuilder("light")

	// State machine: off -> on -> off
	power := b.Bool("power")

	b.Event("toggle").
		Writes(power).
		Apply(func(s gsm.State) gsm.State {
			return s.SetBool(power, !s.GetBool(power))
		}).
		Add()

	machine, _, _ := b.Build()

	s := machine.NewState()
	fmt.Printf("Initial: power=%v\n", s.GetBool(power))

	s = machine.Apply(s, "toggle")
	fmt.Printf("After toggle: power=%v\n", s.GetBool(power))

	s = machine.Apply(s, "toggle")
	fmt.Printf("After toggle: power=%v\n", s.GetBool(power))

	// Output:
	// Initial: power=false
	// After toggle: power=true
	// After toggle: power=false
}

// ExampleState_String shows the human-readable state representation.
func ExampleState_String() {
	b := gsm.NewBuilder("example")

	status := b.Enum("status", "pending", "active", "done")
	count := b.Int("count", 0, 100)
	enabled := b.Bool("enabled")

	machine, _, _ := b.Build()

	s := machine.NewState()
	s = s.Set(status, "active")
	s = s.SetInt(count, 42)
	s = s.SetBool(enabled, true)

	fmt.Println(s)
	// Output: {status=active, count=42, enabled=true}
}

// ExampleBuilder_Invariant demonstrates the priority-ordered compensation system.
// When multiple invariants are violated, repairs fire in declaration order.
func ExampleBuilder_Invariant() {
	b := gsm.NewBuilder("stock")

	qty := b.Int("qty", 0, 100)
	reserved := b.Int("reserved", 0, 100)

	// Invariant 1: reserved cannot exceed quantity (higher priority)
	b.Invariant("reserved_lte_qty").
		Over(qty, reserved).
		Check(func(s gsm.State) bool {
			return s.GetInt(reserved) <= s.GetInt(qty)
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(reserved, s.GetInt(qty))
		}).
		Add()

	// Invariant 2: quantity cannot be negative (lower priority)
	b.Invariant("qty_gte_zero").
		Over(qty).
		Check(func(s gsm.State) bool {
			return s.GetInt(qty) >= 0
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(qty, 0)
		}).
		Add()

	b.Event("reduce").
		Writes(qty).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(qty, s.GetInt(qty)-10)
		}).
		Add()

	machine, _, _ := b.Build()

	s := machine.NewState()
	s = s.SetInt(qty, 5)
	s = s.SetInt(reserved, 3)

	s = machine.Apply(s, "reduce") // qty becomes -5, triggers compensation

	fmt.Printf("qty=%d, reserved=%d\n", s.GetInt(qty), s.GetInt(reserved))
	// Output: qty=0, reserved=0
}

// ExampleMachine_Export demonstrates JSON export for multi-language runtimes.
func ExampleMachine_Export() {
	b := gsm.NewBuilder("simple")
	state := b.Bool("state")

	b.Event("toggle").
		Writes(state).
		Apply(func(s gsm.State) gsm.State {
			return s.SetBool(state, !s.GetBool(state))
		}).
		Add()

	machine, _, _ := b.Build()

	// Export to JSON (verification tables + metadata)
	err := machine.Export("/tmp/simple.json")
	if err != nil {
		panic(err)
	}

	fmt.Println("Exported to /tmp/simple.json")
	// Output: Exported to /tmp/simple.json
}
