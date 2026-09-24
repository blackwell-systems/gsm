# gsm - Governed State Machines

[![Blackwell Systems™](https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg)](https://github.com/blackwell-systems)
[![Go Reference](https://pkg.go.dev/badge/github.com/blackwell-systems/gsm.svg)](https://pkg.go.dev/github.com/blackwell-systems/gsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/blackwell-systems/gsm)](https://goreportcard.com/report/github.com/blackwell-systems/gsm)
[![CI](https://github.com/blackwell-systems/gsm/actions/workflows/test.yml/badge.svg)](https://github.com/blackwell-systems/gsm/actions/workflows/test.yml)
[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.18677400.svg)](https://doi.org/10.5281/zenodo.18677400)

**Act like UDP, receive like TCP.**

What if distributed systems don't have to coordinate - because they agree on the rules ahead of time, so ordering and compensations are deterministic? The underlying theory - normalization confluence - proves that compensation is sufficient for convergence when two algebraic properties hold, without the expressiveness limits of CRDTs or the latency cost of consensus.

`gsm` is a Go library for constructing state machines where events may arrive out of order and violate business rules, but automatic compensation ensures all replicas converge to the same valid state. Convergence is **verified at build time** via exhaustive state-space enumeration. Runtime event application is **O(1) table lookup** with zero compensation overhead.

CRDTs solve convergence by requiring operations to commute. But when your operations can violate business invariants - shipping an unpaid order, overdrawing an account - commutativity alone isn't enough. `gsm` provides convergence through **compensation**: declare what valid means and how to repair violations, and the library proves that all event orderings converge to the same valid state.

Registries also **federate**: connect independently-governed machines with directed morphisms that encode cross-organizational constraints - a manufacturer's status constrains a supplier's listing, a regulator's rules constrain a bank - and `gsm` proves the *whole network* converges. Same build-time guarantee, now across organizational boundaries. See [Federated Registries](#federated-registries).

## Example: Order Fulfillment

Full example from the paper (see `gsm_test.go`):

```go
r := gsm.NewRegistry("order_fulfillment")

status := r.Enum("status", "pending", "paid", "shipped", "cancelled")
paid := r.Bool("paid")
inventory := r.Int("inventory", 0, 5)

// Invariant: can't ship unpaid orders
r.Invariant("no_ship_unpaid").
    Watches(status, paid).
    Holds(func(s gsm.State) bool {
        return s.Get(status) != "shipped" || s.GetBool(paid)
    }).
    Repair(func(s gsm.State) gsm.State {
        return s.Set(status, "pending")
    }).
    Add()

// Invariant: inventory can't go negative
r.Invariant("stock_non_negative").
    Watches(inventory).
    Holds(func(s gsm.State) bool {
        return s.GetInt(inventory) >= 0
    }).
    Repair(func(s gsm.State) gsm.State {
        return s.SetInt(inventory, 0)
    }).
    Add()

r.Event("process_payment").
    Writes(status, paid).
    Guard(func(s gsm.State) bool {
        return s.Get(status) == "pending"
    }).
    Apply(func(s gsm.State) gsm.State {
        return s.Set(status, "paid").SetBool(paid, true)
    }).
    Add()

r.Event("ship_item").
    Writes(status, inventory).
    Guard(func(s gsm.State) bool {
        return s.Get(status) == "paid" && s.GetInt(inventory) > 0
    }).
    Apply(func(s gsm.State) gsm.State {
        return s.Set(status, "shipped").SetInt(inventory, s.GetInt(inventory)-1)
    }).
    Add()

r.Event("restock").
    Writes(inventory).
    Apply(func(s gsm.State) gsm.State {
        return s.SetInt(inventory, s.GetInt(inventory)+1)
    }).
    Add()

// Only check independent pairs (restock comes from different source)
r.Independent("process_payment", "restock")
r.Independent("ship_item", "restock")

machine, report, err := r.Build() // Verifies convergence
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}

// Runtime: O(1) table lookup, no compensation logic runs
s := machine.NewState()
s = machine.Apply(s, "ship_item")       // Arrives before payment
s = machine.Apply(s, "process_payment") // Arrives after shipment
// Compensation fired automatically - converges to valid state
```

> **New to convergent systems?** See [CONCEPTS.md](CONCEPTS.md) for foundational definitions, theory explanations, and a glossary mapping paper terms to code.
>
> **Want rigorous mathematical foundations?** See [THEORY.md](THEORY.md) for formal definitions, proofs, and connections to rewriting theory.
>
> **Research paper:** [*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18677400) (Blackwell, 2026)

## When to Use gsm

**Use gsm when:**
- Building event-sourced systems with out-of-order events
- Operations can violate invariants (need compensation/repair)
- State space is finite and enumerable (< ~1M states)
- You want mathematical convergence guarantees
- Multiple registries share constraints across organizational boundaries (federated registry networks)
- You're using Go

**Don't use gsm when:**
- Operations already commute (use CRDTs instead)
- Operations preserve invariants in all orderings (use invariant confluence)
- State space is unbounded or infinite
- Real-time latency requirements conflict with build-time verification cost

## How It Works

### Build Time (Verification)

When you call `registry.Build()`:

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

## Core Concepts

### Invariants

An **invariant** is a property that must always hold on valid states. Each invariant has three parts:

- **`Watches(vars...)`**: Declares which variables the invariant depends on (its "footprint"). The repair function can only modify these variables.
- **`Holds(func)`**: The boolean condition that must be true. When this returns false, compensation fires.
- **`Repair(func)`**: How to fix states where the invariant is violated. This is the compensation function.

Invariants fire in **declaration order** (priority). If multiple invariants are violated, the first one repairs first, then the next, until all hold.

### Compensation

**Compensation** is the automatic repair process that fires when events violate invariants:

1. Event modifies state (e.g., "withdraw $100")
2. Invariant check fails (e.g., balance becomes -50)
3. Repair function fires (e.g., set balance to 0)
4. Repeat until all invariants hold

For convergence, compensation must be:

- **Well-Founded (WFC)**: Repairs must eventually terminate (no infinite loops)
- **Commutative (CC)**: Event order shouldn't matter after compensation runs

The library **verifies both properties at build time** by exhaustively checking all possible states and event orderings.

### Events

**Events** are operations that modify state. Each event declares:

- **`Writes(vars...)`**: Which variables this event modifies
- **`Guard(func)`**: Optional precondition - if false, event is a no-op
- **`Apply(func)`**: The effect function that transforms the state

Events can arrive **in any order**. The library verifies that different orderings converge to the same final state.

### Independence Declarations

By default, gsm checks **all event pairs** for commutativity. For large systems, you can optimize by declaring which pairs are independent:

```go
// Calling Independent() automatically switches to declared-only mode
r.Independent("deposit", "send_notification")
r.Independent("withdraw", "send_notification")
```

**Independent events** can arrive in either order (they're not causally related). Only declared pairs will be checked for commutativity.

**Tip**: Events with disjoint `Writes()` sets and non-overlapping invariant footprints are automatically proved commutative via footprint analysis (no exhaustive checking needed).

## API Overview

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
// Enum (panics if value not in declared set)
s = s.Set(statusVar, "active")

// Bool
s = s.SetBool(enabledVar, true)

// Int (silently clamped to declared range - SetInt(countVar, 999) on [0,100] becomes 100)
s = s.SetInt(countVar, 42)
```

## Federated Registries

A single registry governs one machine. Real systems span **multiple** registries with constraints across boundaries. `gsm` composes them into a **federation** connected by directed **morphisms**, and proves the whole network converges - the same build-time guarantee, one level up.

```go
// Two independently-governed registries (events/invariants elided)...
mfr := gsm.NewRegistry("manufacturer")
mstate := mfr.Enum("mstate", "draft", "active", "suspended")

sup := gsm.NewRegistry("supplier")
sstate := sup.Enum("sstate", "idle", "listed", "stale", "err")

// ...linked by a morphism: the manufacturer's status fixes the supplier's listing.
image := map[string]string{"draft": "idle", "active": "listed", "suspended": "stale"}
fed := gsm.NewFederation("mfr-sup").
    Morphism(mfr, sup).
    Shared(sstate). // the target variables this morphism controls
    Map(func(srcNF, dst gsm.State) gsm.State { // source normal form → target's shared component
        return dst.Set(sstate, image[srcNF.Get(mstate)])
    }).
    Add()

m, report, err := fed.Build() // verifies the WHOLE network converges
if err != nil {
    panic(fmt.Sprintf("federation not guaranteed to converge: %v\n%s", err, report))
}

s := m.NewState()
s = m.Apply(s, sup, "eexp") // supplier acts...
s = m.Apply(s, mfr, "epub") // ...manufacturer acts; morphism repair re-derives the listing
```

**Coordination-free conflict resolution.** In a morphism `A → B`, source `A` is **authoritative** over `B`'s shared component: `A`'s normal form deterministically fixes it. When a source event and a target event race, the source wins - no locking, no consensus.

**The build-time contract, extended.** `Build()` refuses any federation that cannot converge: the network must be a tree/forest (no cycles; at most one source per target), component names must be distinct, and every morphism must preserve target validity when it overwrites the shared component (the *M1* condition). A federated machine exists only if the whole network is proven convergent. The federated normal form is *constructive* - each source normalizes independently, then shared components propagate along morphisms in topological order - so federation never materializes the product state space.

**Multi-source targets (beyond the paper).** When a target has *two* sources, the authority argument doesn't pick a winner — so the target declares a `Resolver` that deterministically merges its sources (priority, AND/OR, most-restrictive, etc.), relaxing the tree requirement to any acyclic DAG:

```go
fed.Morphism(hr, door).Shared(access).Map(...).Add().
    Morphism(security, door).Shared(access).Map(...).Add().
    Resolve(door, func(dst gsm.State, src map[string]gsm.State) gsm.State {
        if src["hr"].GetBool(employed) && src["security"].GetBool(cleared) {
            return dst.Set(access, "granted") // AND: both sources must agree
        }
        return dst.Set(access, "denied")
    })
```

This extends **past the paper's tree-only theorem** (§8, Remark 8.15 leaves it open because the merge is domain logic, not math). gsm makes it safe the way it handles single registries — not by a general proof, but by **exhaustively verifying at build time** that, for *this* federation, the resolver writes only shared variables, preserves target validity for every reachable source combination, and depends only on the sources. A resolver that could diverge is rejected. A multi-source target *without* a resolver is still rejected.

> Federation implements **Section 8** (Federated Convergence) of the paper for tree-shaped networks; multi-source DAGs are a gsm extension, guaranteed per-federation by exhaustive build-time verification rather than by the paper's proof.

## Verification Report

The `Report` returned by `Build()` shows:

```
Machine: order_fulfillment
  Variables: 3
  States: 48
  Events: 5

  WFC: PASS (max repair depth: 1)
  CC (Compensation Commutativity): PASS (3 pairs: 3 disjoint, 0 brute-force)

  Convergence: GUARANTEED
Checked in: 234µs
```

**WFC (Well-Founded Compensation)**: Compensation terminates from every state. The report shows the maximum number of repair steps needed.

**CC (Compensation Commutativity)**: Different event orderings converge to the same normal form. The report shows:
- **Disjoint pairs** - Proved by footprint analysis (no exhaustive check needed)
- **Brute-force pairs** - Checked by testing all states

If verification fails, you get a counterexample:

```
CC (Compensation Commutativity): FAIL
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

## Relationship to the Paper

This library implements both the **single-registry governance model** (Section 3) and the **federated convergence model** (Section 8) of the paper:

- **Registry** = the machine definition (variables, invariants, compensation, events)
- **WFC (Definition 4.1)** = well-founded measure on compensation depth
- **CC (Definition 4.3)** = compensation commutativity (CC1 + CC2)
- **Theorem 5.1** = WFC + CC ⟹ unique normal forms (proven via Newman's Lemma)
- **Section 9** = verification calculus with footprint optimization (implemented in `verify.go`)
- **Federation (Section 8)** = registry morphisms, the authority argument, and the constructive federated normalizer ρ_Fed (implemented in `federation.go` as `Federation` / `FedMachine`)

The paper proves: **if WFC and CC hold, all processors consuming the same events converge to the same valid state regardless of application order** - and that this extends to a tree-shaped network of registries connected by validity-preserving morphisms.

This library verifies: **does your machine satisfy WFC and CC?**

## Limitations

- **Finite state spaces only** - Cannot model unbounded domains (arbitrary strings, lists)
- **Build-time cost** - Large state spaces (> 1M states) verification becomes slow
- **Verification requires Go** - Runtime portable via JSON export, but verification engine is Go-only
- **Federation** - Tree-shaped networks are covered by the paper's proof (Section 8); multi-source DAGs are supported via user-declared `Resolver`s, guaranteed per-federation by exhaustive build-time verification rather than by a general theorem. Cyclic networks and multi-source targets without a resolver are rejected at build
- **No runtime monitoring** - Once built, machine is immutable (cannot add events/invariants dynamically)

## Multi-Language Support

While verification requires Go, **runtime is portable** to any language. Use `Machine.Export()` to serialize the verified machine to JSON:

```go
machine, _, err := registry.Build()
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
  doi = {10.5281/zenodo.18677400},
  url = {https://doi.org/10.5281/zenodo.18677400}
}
```

## Related Tools

- **[nccheck](https://github.com/blackwell-systems/nccheck)** - YAML-based verifier for registry specs (reference implementation from the paper)
- **gsm** (this library) - Go library for building verified convergent state machines

## Further Reading

- [Paper: Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)
- [Newman's Lemma](https://en.wikipedia.org/wiki/Newman%27s_lemma) - Foundation for confluence proofs
- [CRDTs](https://crdt.tech/) - Alternative approach via operation commutativity
- [Coordination Avoidance in Database Systems](https://dl.acm.org/doi/10.14778/2735508.2735509) - Invariant confluence (Bailis et al., VLDB 2014)
