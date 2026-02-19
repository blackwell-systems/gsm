package gsm_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// buildOrderMachine constructs the order fulfillment example from the
// normalization confluence paper.
func buildOrderMachine(t *testing.T) (*gsm.Machine, *gsm.Report) {
	t.Helper()

	b := gsm.NewBuilder("order_fulfillment")

	// State variables
	status := b.Enum("status", "pending", "paid", "shipped", "cancelled")
	paid := b.Bool("paid")
	inventory := b.Int("inventory", 0, 5)

	// Invariants with repair (priority order)
	b.Invariant("no_ship_unpaid").
		Watches(status, paid).
		Holds(func(s gsm.State) bool {
			return s.Get(status) != "shipped" || s.GetBool(paid)
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.Set(status, "pending")
		}).
		Add()

	b.Invariant("stock_non_negative").
		Watches(inventory).
		Holds(func(s gsm.State) bool {
			return s.GetInt(inventory) >= 0
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(inventory, 0)
		}).
		Add()

	// Events
	b.Event("place_order").
		Writes(status, paid).
		Apply(func(s gsm.State) gsm.State {
			return s.Set(status, "pending").SetBool(paid, false)
		}).
		Add()

	b.Event("process_payment").
		Writes(status, paid).
		Guard(func(s gsm.State) bool {
			return s.Get(status) == "pending"
		}).
		Apply(func(s gsm.State) gsm.State {
			return s.Set(status, "paid").SetBool(paid, true)
		}).
		Add()

	b.Event("ship_item").
		Writes(status, inventory).
		Guard(func(s gsm.State) bool {
			return s.Get(status) == "paid" && s.GetInt(inventory) > 0
		}).
		Apply(func(s gsm.State) gsm.State {
			return s.Set(status, "shipped").SetInt(inventory, s.GetInt(inventory)-1)
		}).
		Add()

	b.Event("cancel_order").
		Writes(status).
		Guard(func(s gsm.State) bool {
			return s.Get(status) != "shipped"
		}).
		Apply(func(s gsm.State) gsm.State {
			return s.Set(status, "cancelled")
		}).
		Add()

	b.Event("restock").
		Writes(inventory).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(inventory, s.GetInt(inventory)+1)
		}).
		Add()

	// Only independent events need to commute.
	// Restock comes from a different source than order lifecycle events.
	// But ship_item and restock both write inventory — not independent.
	b.OnlyDeclaredPairs()
	b.Independent("place_order", "restock")
	b.Independent("process_payment", "restock")
	b.Independent("cancel_order", "restock")

	machine, report, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v\n%s", err, report)
	}

	return machine, report
}

func TestBuildPasses(t *testing.T) {
	_, report := buildOrderMachine(t)
	t.Logf("\n%s", report)

	if !report.WFC {
		t.Fatal("expected WFC to pass")
	}
	if !report.CC {
		t.Fatal("expected CC to pass")
	}
}

func TestApplyOrderIndependence(t *testing.T) {
	m, _ := buildOrderMachine(t)

	s0 := m.NewState()

	// Order 1: place → restock → pay → ship
	s1 := m.Apply(s0, "place_order")
	s1 = m.Apply(s1, "restock")
	s1 = m.Apply(s1, "process_payment")
	s1 = m.Apply(s1, "ship_item")

	// Order 2: restock → place → pay → ship
	s2 := m.Apply(s0, "restock")
	s2 = m.Apply(s2, "place_order")
	s2 = m.Apply(s2, "process_payment")
	s2 = m.Apply(s2, "ship_item")

	if s1.ID() != s2.ID() {
		t.Fatalf("order dependence detected:\n  order1: %s\n  order2: %s", s1, s2)
	}
	t.Logf("both orders → %s", s1)
}

func TestCompensationFires(t *testing.T) {
	b := gsm.NewBuilder("test_compensation")

	status := b.Enum("status", "pending", "paid", "shipped")
	paid := b.Bool("paid")

	b.Invariant("no_ship_unpaid").
		Watches(status, paid).
		Holds(func(s gsm.State) bool {
			return s.Get(status) != "shipped" || s.GetBool(paid)
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.Set(status, "pending")
		}).
		Add()

	b.Event("force_ship").
		Writes(status).
		Apply(func(s gsm.State) gsm.State {
			return s.Set(status, "shipped")
		}).
		Add()

	b.OnlyDeclaredPairs() // single event, no pairs

	machine, report, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v\n%s", err, report)
	}

	// Ship without paying: compensation should roll back
	s := machine.NewState()
	s = machine.Apply(s, "force_ship")

	if s.Get(status) != "pending" {
		t.Fatalf("expected compensation to roll back, got %s", s)
	}
	t.Logf("compensation fired correctly: %s", s)
}

func TestCCFailureDetected(t *testing.T) {
	// Genuine CC violation:
	//   x: int[0, 4], invariant x <= 3, repair: x = 0
	//   Event A: x += 1, Event B: x += 2
	//
	//   From x=2:
	//     A→B: 2→3(valid)→5→clamp4→repair→0. Result: 0
	//     B→A: 2→4→repair→0→1.               Result: 1
	//     0 ≠ 1

	b := gsm.NewBuilder("bad_machine")

	x := b.Int("x", 0, 4)

	b.Invariant("x_bounded").
		Watches(x).
		Holds(func(s gsm.State) bool {
			return s.GetInt(x) <= 3
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(x, 0)
		}).
		Add()

	b.Event("inc_one").
		Writes(x).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(x, s.GetInt(x)+1)
		}).
		Add()

	b.Event("inc_two").
		Writes(x).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(x, s.GetInt(x)+2)
		}).
		Add()

	// Default: check all pairs
	_, report, err := b.Build()
	if err == nil {
		t.Fatal("expected CC failure, got success")
	}

	if report.CC {
		t.Fatal("expected CC to fail")
	}

	t.Logf("correctly detected CC failure:\n%s", report)
}

func TestWFCFailureDetected(t *testing.T) {
	// WFC violation: compensation that cycles.
	//   Invariant 1: x != 1, repair: x = 2
	//   Invariant 2: x != 2, repair: x = 1
	//   From x=1: repair1→2, repair2→1, cycle

	b := gsm.NewBuilder("cycling_machine")

	x := b.Int("x", 0, 2)

	b.Invariant("not_one").
		Watches(x).
		Holds(func(s gsm.State) bool {
			return s.GetInt(x) != 1
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(x, 2)
		}).
		Add()

	b.Invariant("not_two").
		Watches(x).
		Holds(func(s gsm.State) bool {
			return s.GetInt(x) != 2
		}).
		Repair(func(s gsm.State) gsm.State {
			return s.SetInt(x, 1)
		}).
		Add()

	b.Event("set_one").
		Writes(x).
		Apply(func(s gsm.State) gsm.State {
			return s.SetInt(x, 1)
		}).
		Add()

	b.OnlyDeclaredPairs()

	_, report, err := b.Build()
	if err == nil {
		t.Fatal("expected WFC failure, got success")
	}

	if report.WFC {
		t.Fatal("expected WFC to fail")
	}

	t.Logf("correctly detected WFC failure:\n%s", report)
}

func TestStateString(t *testing.T) {
	m, _ := buildOrderMachine(t)
	s := m.NewState()
	str := s.String()
	t.Logf("zero state: %s", str)
	if str == "" {
		t.Fatal("expected non-empty string representation")
	}
}

func TestNormalize(t *testing.T) {
	m, _ := buildOrderMachine(t)
	s := m.NewState()
	n := m.Normalize(s)
	if s.ID() != n.ID() {
		t.Fatalf("normalize changed valid state: %s → %s", s, n)
	}
}

func TestIsValid(t *testing.T) {
	m, _ := buildOrderMachine(t)
	s := m.NewState()
	if !m.IsValid(s) {
		t.Fatalf("zero state should be valid")
	}
}

func TestDisjointFootprintOptimization(t *testing.T) {
	_, report := buildOrderMachine(t)
	// restock only touches inventory; most order events touch status/paid
	// So restock pairs should be proved by disjointness where footprints don't overlap
	t.Logf("disjoint: %d, brute-force: %d", report.PairsDisjoint, report.PairsBrute)
}

func TestExport(t *testing.T) {
	machine, _ := buildOrderMachine(t)

	tmpfile := t.TempDir() + "/order.gsm.json"
	if err := machine.Export(tmpfile); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var export map[string]interface{}
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// Verify key fields
	if export["name"] != "order_fulfillment" {
		t.Errorf("wrong name: %v", export["name"])
	}
	if export["version"].(float64) != 1 {
		t.Errorf("wrong version: %v", export["version"])
	}

	vars := export["vars"].([]interface{})
	if len(vars) != 3 {
		t.Errorf("wrong var count: %d", len(vars))
	}

	events := export["events"].([]interface{})
	if len(events) != 5 {
		t.Errorf("wrong event count: %d", len(events))
	}

	nf := export["nf"].([]interface{})
	step := export["step"].([]interface{})
	// NF table includes bitpacked padding states (64 = 2^6 bits)
	if len(nf) < 48 {
		t.Errorf("state count too small: %d", len(nf))
	}
	if len(step) != 5 {
		t.Errorf("wrong step table size: %d", len(step))
	}

	t.Logf("Exported %d bytes to %s", len(data), tmpfile)
}
