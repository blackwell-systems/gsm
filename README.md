# gsm — Governed State Machines

[![Go Reference](https://pkg.go.dev/badge/github.com/blackwell-systems/gsm.svg)](https://pkg.go.dev/github.com/blackwell-systems/gsm)

**Build event-driven systems with mathematical convergence guarantees.**

`gsm` is a Go library for constructing state machines where events may arrive out of order and violate business rules, but automatic compensation ensures all replicas converge to the same valid state. Convergence is **verified at build time** via exhaustive state-space enumeration. Runtime event application is **O(1) table lookup** with zero compensation overhead.

Based on: [*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18671870) (Blackwell, 2026)

## Quick Example

```go
package main

import (
    "fmt"
    "github.com/blackwell-systems/gsm"
)

func main() {
    b := gsm.NewBuilder("order_system")

    // State variables (finite domains)
    status := b.Enum("status", "pending", "paid", "shipped")
    paid := b.Bool("paid")

    // Business rule: can't ship unpaid orders
    b.Invariant("no_ship_unpaid").
        Over(status, paid).
        Check(func(s gsm.State) bool {
            return s.Get(status) != "shipped" || s.GetBool(paid)
        }).
        Repair(func(s gsm.State) gsm.State {
            return s.Set(status, "pending") // Roll back
        }).
        Add()

    // Events
    b.Event("pay").
        Writes(status, paid).
        Apply(func(s gsm.State) gsm.State {
            return s.Set(status, "paid").SetBool(paid, true)
        }).
        Add()

    b.Event("ship").
        Writes(status).
        Apply(func(s gsm.State) gsm.State {
            return s.Set(status, "shipped")
        }).
        Add()

    // Build with verification
    machine, report, err := b.Build()
    if err != nil {
        panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
    }

    fmt.Printf("%s", report)
    // Machine: order_system
    //   Variables: 2
    //   States: 6
    //   Events: 2
    //   WFC: PASS (max repair depth: 1)
    //   CC:  PASS (1 pairs: 1 disjoint, 0 brute-force)
    //   Convergence: GUARANTEED

    // Runtime usage (O(1) lookups)
    s := machine.NewState()
    s = machine.Apply(s, "ship") // Arrives before payment
    s = machine.Apply(s, "pay")  // Arrives after shipment

    fmt.Printf("Final: %s\n", s) // {status=paid, paid=true}
    // Compensation fired automatically - shipment was rolled back
}
```

## The Problem

You're building an event-driven system where:
- Events arrive **out of order** (network delays, async processing, replays)
- Events can **violate business rules** (ship before payment, withdraw beyond balance)
- You have **compensation logic** that fixes violations (rollbacks, adjustments)
- You need **replicas to converge** (same events → same final state, regardless of order)

Traditional solutions:
- **Total ordering** (Kafka partitions, sequential processing) - slow, not fault-tolerant
- **CRDTs** - require operations to commute (doesn't fit business rules)
- **Hope and pray** - test heavily, debug divergence in production

`gsm` gives you a different solution: **build-time proof that your compensation strategy converges**.

## How It Works

### Build Time (Verification)

When you call `builder.Build()`:

1. **Enumerate state space** - All combinations of variable values (must be finite)
2. **Compute normal forms** - For every state, apply compensation until valid
3. **Verify WFC** - Compensation terminates and reaches valid states
4. **Build step table** - For every (event, state) pair, precompute the normal form after applying the event
5. **Verify CC** - Different event orderings reach the same normal form (with footprint optimization)

If verification passes, you get an immutable `Machine` with precomputed lookup tables.

If verification fails, you get a detailed report showing:
- Which events violate CC
- At which state the violation occurs
- The divergent traces (order 1 vs order 2)

### Runtime (O(1) Execution)

```go
machine.Apply(state, "ship_item")
```

This does **one table lookup**: `step[event_index][state_id]` returns the precomputed normal form.

**No compensation logic runs at runtime.** All the complexity is resolved at build time.

## When to Use gsm

✅ **Use gsm when:**
- Building event-sourced systems with out-of-order events
- Operations can violate invariants (need compensation/repair)
- State space is finite and enumerable (< ~1M states)
- You want mathematical convergence guarantees
- You're using Go

❌ **Don't use gsm when:**
- Operations already commute (use CRDTs instead)
- Operations preserve invariants in all orderings (use invariant confluence)
- State space is unbounded or infinite
- Real-time latency requirements conflict with build-time verification cost

## API Overview

### Building Machines

```go
b := gsm.NewBuilder("machine_name")

// Declare state variables (finite domains required)
status := b.Enum("status", "draft", "active", "archived")
count := b.Int("count", 0, 100)           // range [0, 100] inclusive
enabled := b.Bool("enabled")

// Declare invariants with compensation
b.Invariant("count_positive").
    Over(count).                           // Footprint: which vars this constrains
    Check(func(s gsm.State) bool {
        return s.GetInt(count) >= 0
    }).
    Repair(func(s gsm.State) gsm.State {
        return s.SetInt(count, 0)         // Clamp to zero
    }).
    Add()

// Declare events
b.Event("increment").
    Writes(count).                         // Which vars this modifies
    Guard(func(s gsm.State) bool {        // Optional precondition
        return s.GetInt(count) < 100
    }).
    Apply(func(s gsm.State) gsm.State {
        return s.SetInt(count, s.GetInt(count)+1)
    }).
    Add()

// Optional: declare which event pairs are independent
// (if omitted, all pairs are checked)
b.DeclaredIndependence()
b.Independent("increment", "enable")
b.Independent("increment", "disable")

// Build with verification
machine, report, err := b.Build()
```

### Using Machines

```go
// Create initial state (all variables at min/first value)
s := machine.NewState()

// Apply events (returns new state, original unchanged)
s = machine.Apply(s, "increment")
s = machine.Apply(s, "increment")

// Check validity
if machine.IsValid(s) {
    fmt.Println("State satisfies all invariants")
}

// Manually normalize (usually not needed - Apply does this)
s = machine.Normalize(s)

// Get event list
events := machine.Events() // ["increment", "enable", "disable"]
```

### Reading State

```go
// Enum variables
status := s.Get(statusVar)           // returns string

// Bool variables
enabled := s.GetBool(enabledVar)     // returns bool

// Int variables
count := s.GetInt(countVar)          // returns int (adjusted for min offset)
```

### Writing State

```go
// Enum
s = s.Set(statusVar, "active")

// Bool
s = s.SetBool(enabledVar, true)

// Int (automatically clamped to declared range)
s = s.SetInt(countVar, 42)
```

## Verification Report

The `Report` returned by `Build()` shows:

```
Machine: order_fulfillment
  Variables: 3
  States: 48
  Events: 5

  WFC: PASS (max repair depth: 1)
  CC:  PASS (3 pairs: 3 disjoint, 0 brute-force)

  Convergence: GUARANTEED
Checked in: 234µs
```

**WFC (Well-Founded Compensation)**: Compensation terminates from every state. The report shows the maximum number of repair steps needed.

**CC (Compensation Commutativity)**: Different event orderings converge to the same normal form. The report shows:
- **Disjoint pairs** - Proved by footprint analysis (no exhaustive check needed)
- **Brute-force pairs** - Checked by testing all states

If verification fails, you get a counterexample:

```
CC:  FAIL
  Events: (grant_read, grant_write)
  State:  {can_read=false, can_write=false}
  grant_read→grant_write: {can_read=true, can_write=true}
  grant_write→grant_read: {can_read=true, can_write=false}
```

This shows the exact state and event pair where CC fails, plus the divergent traces.

## Performance

### Build Time

Verification cost depends on state space size:

| States | Variables | Build Time | Notes |
|--------|-----------|------------|-------|
| 6 | 2 bools | < 1ms | Instant |
| 48 | 1 enum(5) + 1 bool + 1 int[0..5] | < 1ms | Instant |
| 1,024 | 10 bools | ~5ms | Very fast |
| 1M | ~20 bits total | ~500ms | Acceptable |

State space grows as the **product** of variable domains: 5 enums × 100 ints = 500 states.

Default limit: 2²⁰ ≈ 1M states. Configurable but exhaustive verification becomes slow beyond this.

### Runtime

Event application: **O(1)** - single array lookup, no computation.

Memory: One `uint64` per state for normal form table, plus one `uint64` per (event, state) pair for step table. For 1M states × 10 events = ~80MB.

## Design Decisions

### Why Finite State Spaces?

Verification requires exhaustively checking all states. Unbounded domains (arbitrary strings, uncapped integers, lists) make this impossible.

Finite domains force you to model "what actually matters" for convergence. An inventory count doesn't need the full range of `int64` - it needs `[0..maxStock]`.

### Why Immutable States?

States are values (bitpacked `uint64`), not mutable objects. This enables:
- Use as table indices (identity = value)
- Safe concurrent access
- Functional event application (`Apply` returns new state, doesn't mutate)

### Why Build-Time Verification?

The alternative is runtime verification: check CC dynamically by recording traces and detecting divergence. Problems:
- No upfront guarantee (discover failures in production)
- Performance overhead (tracking, comparison)
- Incomplete coverage (only checks observed traces)

Build-time verification costs more upfront but gives complete coverage and zero runtime overhead.

### Why Table Lookups?

The step table precomputes **every possible (event, state) → normal form** transition. This seems expensive (space), but:
- Avoids repeated compensation logic at runtime
- Makes event application deterministic and fast
- Enables proof of correctness (table is the witness)

For small-to-medium state spaces (< 1M states, < 100 events), the table is tractable (~80MB).

## Relationship to the Paper

This library implements the **single-registry governance model** from Section 3 of the paper:

- **Registry** = the machine definition (variables, invariants, compensation, events)
- **WFC (Definition 4.1)** = well-founded measure on compensation depth
- **CC (Definition 4.3)** = compensation commutativity (CC1 + CC2)
- **Theorem 5.1** = WFC + CC ⟹ unique normal forms (proven via Newman's Lemma)
- **Section 9** = verification calculus with footprint optimization (implemented in `verify.go`)

The paper proves: **if WFC and CC hold, all processors consuming the same events converge to the same valid state regardless of application order.**

This library verifies: **does your machine satisfy WFC and CC?**

## Example: Order Fulfillment

Full example from the paper (see `gsm_test.go`):

```go
b := gsm.NewBuilder("order_fulfillment")

status := b.Enum("status", "pending", "paid", "shipped", "cancelled")
paid := b.Bool("paid")
inventory := b.Int("inventory", 0, 5)

// Invariant: can't ship unpaid orders
b.Invariant("no_ship_unpaid").
    Over(status, paid).
    Check(func(s gsm.State) bool {
        return s.Get(status) != "shipped" || s.GetBool(paid)
    }).
    Repair(func(s gsm.State) gsm.State {
        return s.Set(status, "pending")
    }).
    Add()

// Invariant: inventory can't go negative
b.Invariant("stock_non_negative").
    Over(inventory).
    Check(func(s gsm.State) bool {
        return s.GetInt(inventory) >= 0
    }).
    Repair(func(s gsm.State) gsm.State {
        return s.SetInt(inventory, 0)
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

b.Event("restock").
    Writes(inventory).
    Apply(func(s gsm.State) gsm.State {
        return s.SetInt(inventory, s.GetInt(inventory)+1)
    }).
    Add()

// Only check independent pairs (restock comes from different source)
b.DeclaredIndependence()
b.Independent("process_payment", "restock")
b.Independent("ship_item", "restock")

machine, report, err := b.Build()
// Convergence: GUARANTEED
```

## Limitations

- **Finite state spaces only** - Cannot model unbounded domains (arbitrary strings, lists)
- **Build-time cost** - Large state spaces (> 1M states) verification becomes slow
- **Go only** - Not a cross-language protocol (though principles apply elsewhere)
- **Single-registry** - Federation (Section 7 of paper) not yet implemented
- **No runtime monitoring** - Once built, machine is immutable (cannot add events/invariants dynamically)

## Multi-Language Support

While verification requires Go, **runtime is portable** to any language. Use `Machine.Export()` to serialize the verified machine to JSON:

```go
machine, _, err := builder.Build()
if err != nil {
    log.Fatal(err)
}

machine.Export("order.gsm.json")
```

The exported JSON contains:
- Variable definitions (types, domains)
- Event names (ordered)
- Normal form table: `nf[stateID] → normalized stateID`
- Step table: `step[eventID][stateID] → normalized result stateID`

### Runtime Implementation (Python Example)

```python
import json

class Machine:
    def __init__(self, path):
        with open(path) as f:
            d = json.load(f)
        self.events = {n: i for i, n in enumerate(d['events'])}
        self.step = d['step']
        self.nf = d['nf']

    def apply(self, state, event):
        """O(1) event application via table lookup"""
        return self.step[self.events[event]][state]

    def normalize(self, state):
        return self.nf[state]

# Use it
m = Machine('order.gsm.json')
s = 0
s = m.apply(s, 'ship_item')
s = m.apply(s, 'process_payment')
```

That's it - ~20 lines of code for a complete runtime. The same pattern works in JavaScript, Rust, Java, or any language that can:
1. Load JSON
2. Index arrays

**Verification complexity** stays in Go. **Runtime simplicity** is universal.

## Installation

```bash
go get github.com/blackwell-systems/gsm
```

## Testing

```bash
go test -v
```

Tests cover:
- WFC verification (termination, cycles, depth)
- CC verification (disjoint footprints, brute force, failures)
- Event order independence
- Compensation behavior
- State encoding/decoding

## License

MIT License - see LICENSE file

## Citation

```bibtex
@techreport{blackwell2026nc,
  author = {Blackwell, Dayna},
  title = {Normalization Confluence in Federated Registry Networks},
  year = {2026},
  publisher = {Zenodo},
  doi = {10.5281/zenodo.18671870},
  url = {https://doi.org/10.5281/zenodo.18671870}
}
```

## Related Tools

- **[nccheck](https://github.com/blackwell-systems/nccheck)** - YAML-based verifier for registry specs (reference implementation from the paper)
- **gsm** (this library) - Go library for building verified convergent state machines

## Further Reading

- [Paper: Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18671870)
- [Newman's Lemma](https://en.wikipedia.org/wiki/Newman%27s_lemma) - Foundation for confluence proofs
- [CRDTs](https://crdt.tech/) - Alternative approach via operation commutativity
- [Invariant Confluence](https://www.bailis.org/papers/hat-hotos2013.pdf) - Alternative approach via invariant preservation
