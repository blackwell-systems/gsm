package gsm_test

import (
	"fmt"
	"os"
	"runtime"

	"github.com/blackwell-systems/gsm"
)

// Example demonstrates the complete workflow: define state variables,
// invariants, and events, then verify convergence guarantees at build time.
// Runtime event application is O(1) table lookup with zero overhead.
func Example() {
	b := gsm.NewRegistry("counter")

	// State variable with bounded domain
	count := b.Int("count", 0, 10)

	// Business rule: count cannot exceed maximum
	b.Invariant("cap_at_10").
		Watches(count).
		Holds(func(s gsm.State) bool {
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

// ExampleRegistry_Build shows the verification process and report output.
// The registry exhaustively enumerates the state space and verifies
// WFC (well-founded compensation) and CC (compensation commutativity).
func ExampleRegistry_Build() {
	b := gsm.NewRegistry("counter")

	// Single integer variable
	count := b.Int("count", 0, 10)

	// Invariant: count must stay <= 10
	b.Invariant("cap_at_10").
		Watches(count).
		Holds(func(s gsm.State) bool {
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
	b := gsm.NewRegistry("light")

	// State machine: off -> on -> off
	power := b.Bool("power")

	b.Event("toggle").
		Writes(power).
		Apply(func(s gsm.State) gsm.State {
			return s.SetBool(power, !s.GetBool(power))
		}).
		Add()

	machine, _, err := b.Build()
	if err != nil {
		panic(err)
	}

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
	b := gsm.NewRegistry("example")

	status := b.Enum("status", "pending", "active", "done")
	count := b.Int("count", 0, 100)
	enabled := b.Bool("enabled")

	machine, _, err := b.Build()
	if err != nil {
		panic(err)
	}

	s := machine.NewState()
	s = s.Set(status, "active")
	s = s.SetInt(count, 42)
	s = s.SetBool(enabled, true)

	fmt.Println(s)
	// Output: {status=active, count=42, enabled=true}
}

// ExampleRegistry_Invariant demonstrates the priority-ordered compensation system.
// When multiple invariants are violated, repairs fire in declaration order.
func ExampleRegistry_Invariant() {
	b := gsm.NewRegistry("stock")

	qty := b.Int("qty", 0, 100)
	reserved := b.Int("reserved", 0, 100)

	// Invariant 1: reserved cannot exceed quantity (higher priority)
	b.Invariant("reserved_lte_qty").
		Watches(qty, reserved).
		Holds(func(s gsm.State) bool {
			return s.GetInt(reserved) <= s.GetInt(qty)
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(reserved, s.GetInt(qty))
		}).
		Add()

	// Invariant 2: quantity cannot be negative (lower priority)
	b.Invariant("qty_gte_zero").
		Watches(qty).
		Holds(func(s gsm.State) bool {
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

	machine, _, err := b.Build()
	if err != nil {
		panic(err)
	}

	s := machine.NewState()
	s = s.SetInt(qty, 5)
	s = s.SetInt(reserved, 3)

	s = machine.Apply(s, "reduce") // qty becomes -5, triggers compensation

	fmt.Printf("qty=%d, reserved=%d\n", s.GetInt(qty), s.GetInt(reserved))
	// Output: qty=0, reserved=0
}

// ExampleMachine_Export demonstrates JSON export for multi-language runtimes.
func ExampleMachine_Export() {
	b := gsm.NewRegistry("simple")
	state := b.Bool("state")

	b.Event("toggle").
		Writes(state).
		Apply(func(s gsm.State) gsm.State {
			return s.SetBool(state, !s.GetBool(state))
		}).
		Add()

	machine, _, err := b.Build()
	if err != nil {
		panic(err)
	}

	// Export to JSON (verification tables + metadata)
	tmpDir := "/tmp"
	if runtime.GOOS == "windows" {
		tmpDir = os.Getenv("TEMP")
	}
	path := fmt.Sprintf("%s/simple.json", tmpDir)
	err = machine.Export(path)
	if err != nil {
		panic(err)
	}

	fmt.Println("Machine exported successfully")
	// Output: Machine exported successfully
}

// ExampleFederation shows a worked federated registry network: two independently-governed
// registries connected by a directed morphism. A manufacturer is authoritative over a
// supplier's product listing (the "shared" component), while the supplier keeps its own
// local state (whether the product is "featured"). The morphism re-derives the listing from
// the manufacturer's status on every step, so the supplier can never override it — but the
// supplier's local choices survive. gsm proves the whole network converges at build time.
func ExampleFederation() {
	// Manufacturer (authoritative source): a product's lifecycle status.
	mfr := gsm.NewRegistry("manufacturer")
	status := mfr.Enum("status", "draft", "active", "discontinued")
	mfr.Event("publish").
		Guard(func(s gsm.State) bool { return s.Get(status) == "draft" }).
		Apply(func(s gsm.State) gsm.State { return s.Set(status, "active") }).
		Add()

	// Supplier (target): `listing` is controlled by the manufacturer (shared); `featured`
	// is the supplier's own local flag (not controlled by the morphism).
	sup := gsm.NewRegistry("supplier")
	listing := sup.Enum("listing", "unlisted", "listed", "delisted")
	featured := sup.Bool("featured")
	sup.Event("feature").Writes(featured).
		Apply(func(s gsm.State) gsm.State { return s.SetBool(featured, true) }).
		Add()
	sup.Event("mark_stale").Writes(listing).
		Apply(func(s gsm.State) gsm.State { return s.Set(listing, "delisted") }).
		Add()

	// Morphism: the manufacturer's status deterministically fixes the supplier's listing.
	image := map[string]string{"draft": "unlisted", "active": "listed", "discontinued": "delisted"}
	fed := gsm.NewFederation("catalog").
		Morphism(mfr, sup).
		Shared(listing).
		Map(func(srcNF, dst gsm.State) gsm.State { return dst.Set(listing, image[srcNF.Get(status)]) }).
		Add()

	m, _, err := fed.Build() // proves the whole network converges
	if err != nil {
		panic(err)
	}

	show := func(label string, fs gsm.FedState) {
		fmt.Printf("%-27s status=%-12s listing=%-9s featured=%v\n",
			label, m.Of(fs, mfr).Get(status), m.Of(fs, sup).Get(listing), m.Of(fs, sup).GetBool(featured))
	}

	s := m.NewState()
	show("initial:", s)

	s = m.Apply(s, sup, "feature") // supplier's own local choice
	show("supplier features:", s)

	s = m.Apply(s, sup, "mark_stale") // supplier tries to delist...
	show("supplier delists:", s)      // ...ignored: manufacturer is authoritative (still draft)

	s = m.Apply(s, mfr, "publish") // manufacturer publishes → listing becomes listed
	show("manufacturer publishes:", s)

	// Output:
	// initial:                    status=draft        listing=unlisted  featured=false
	// supplier features:          status=draft        listing=unlisted  featured=true
	// supplier delists:           status=draft        listing=unlisted  featured=true
	// manufacturer publishes:     status=active       listing=listed    featured=true
}

// ExampleNewCollection declares the rules of one product (the template), verifies them
// once, and runs them at every key of a catalog: by symmetry, the check of one product
// covers a catalog of any size.
func ExampleNewCollection() {
	item := gsm.NewRegistry("product")
	stock := item.Int("stock", 0, 5)
	reserved := item.Int("reserved", 0, 5)
	backordered := item.Bool("backordered")

	// A rule sees one product's State: no key, no other product.
	short := func(s gsm.State) bool { return s.GetInt(reserved) > s.GetInt(stock) }
	item.Invariant("backorder_flag").
		Watches(stock, reserved, backordered).
		Holds(func(s gsm.State) bool { return s.GetBool(backordered) == short(s) }).
		Repair(func(s gsm.State) gsm.State { return s.SetBool(backordered, short(s)) }).
		Add()
	item.Event("restock").Writes(stock).
		Apply(func(s gsm.State) gsm.State { return s.SetInt(stock, s.GetInt(stock)+1) }).Add()
	item.Event("reserve").Writes(reserved).
		Apply(func(s gsm.State) gsm.State { return s.SetInt(reserved, s.GetInt(reserved)+1) }).Add()

	type ProductID string
	catalog, report, err := gsm.NewCollection[ProductID]("ProductID", item).Build()
	if err != nil {
		panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
	}
	fmt.Println(report.Symmetry)

	s := catalog.NewState()
	catalog.Apply(s, "widget", "reserve") // reserved before any stock: backordered
	catalog.Apply(s, "gadget", "restock") // another key: widget is unaffected
	fmt.Println(s.Item("widget").GetBool(backordered), s.Item("gadget").GetInt(stock))
	catalog.Apply(s, "widget", "restock") // the stock arrives: compensation clears the flag
	fmt.Println(s.Item("widget").GetBool(backordered))

	// Output:
	// Verified by symmetry over ProductID (items independent; cutoff 1)
	// true 1
	// false
}

func ExampleRegistry_Abstract() {
	r := gsm.NewRegistry("inventory")
	// Ranges far too wide to enumerate: 2^60 states.
	stock := r.Int("stock", 0, 1_000_000)
	shipA := r.Int("ship_a", 0, 1_000_000) // the level shipment A restocks to
	shipB := r.Int("ship_b", 0, 1_000_000)

	// The rules only compare and copy values and the declared constant 5.
	r.Rule("cap").Require(gsm.AtMost(stock, 5)).RepairWith(gsm.SetTo(stock, 5)).Add()
	r.On("receive_a").OnlyIf(gsm.BelowVar(stock, shipA)).Does(gsm.Copy(stock, shipA)).Add()
	r.On("receive_b").OnlyIf(gsm.BelowVar(stock, shipB)).Does(gsm.Copy(stock, shipB)).Add()

	m, report, err := r.Abstract(5).Build() // checks 7^3 representative states
	if err != nil {
		panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
	}
	fmt.Println(report.Abstraction)

	s := m.NewState().SetInt(shipA, 3).SetInt(shipB, 900_000)
	ab := m.Apply(m.Apply(s, "receive_a"), "receive_b")
	ba := m.Apply(m.Apply(s, "receive_b"), "receive_a")
	fmt.Println(ab.GetInt(stock), ba.GetInt(stock))

	// Output:
	// Verified by abstraction over stock, ship_a, ship_b (rules compare values only; constants {5}; 7 representatives)
	// 5 5
}
