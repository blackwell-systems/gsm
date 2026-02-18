# Architecture

This document explains the internal design of gsm, how convergence verification works, and why runtime event application is O(1) with zero compensation overhead.

## Overview

gsm separates verification (build-time) from execution (runtime):

- **Build time**: Exhaustive state-space enumeration verifies WFC and CC properties
- **Runtime**: O(1) table lookups apply events and return normal forms

All compensation is precomputed during `Build()`. The runtime `Machine` contains only lookup tables.

## State Representation

### Bitpacked Encoding

States are stored as `uint64` values with variables bitpacked sequentially:

```
Example: status (2 bits) + paid (1 bit) + count (4 bits)

uint64: ...0000 0011 1101
              ││││ ││└─ count (13)
              ││└┴─────── paid (1)
              └┴────────── status (0)
```

**Benefits:**
- States are contiguous integers: 0, 1, 2, ..., N-1
- Direct use as array indices for O(1) table lookup
- Compact representation (up to 20 bits = ~1M states)

### Variable Layout

Variables are assigned offsets and bit widths during declaration:

```go
status := b.Enum("status", "pending", "paid", "shipped")
// domain=3, bits=2, offset=0

paid := b.Bool("paid")
// domain=2, bits=1, offset=2

count := b.Int("count", 0, 15)
// domain=16, bits=4, offset=3
```

Total state space: 3 × 2 × 16 = 96 states
Total bits: 2 + 1 + 4 = 7 bits

## Build-Time Verification

### Phase 1: Normal Form Computation (WFC Check)

For each state `s`:

1. Start with `s`
2. While any invariant is violated:
   - Apply the first violated invariant's repair
   - Track visited states to detect cycles
   - If cycle detected or depth exceeds state count: **WFC fails**
3. Result is `NF[s]` - the normal form of `s`

**WFC (Well-Founded Compensation)** passes if:
- All states reach a valid fixpoint
- No infinite compensation loops exist
- Maximum repair depth is bounded

Example cycle (WFC failure):

```
s1 --repair1--> s2 --repair2--> s1
```

### Phase 2: Step Table Computation

For each event `e` and state `s`:

```
Step[e][s] = NF(apply(e, s))
```

This precomputes the result of:
1. Applying event `e` to state `s` (may violate invariants)
2. Running compensation to reach the normal form

**Result:** `Step[e][s]` is the final converged state after event `e` from state `s`.

### Phase 3: Compensation Commutativity Check (CC)

For each independent event pair `(e1, e2)`:

#### CC1: Event Commutativity

For all valid states `s`:

```
Step[e1][Step[e2][s]] == Step[e2][Step[e1][s]]
```

Check if different orderings converge to the same normal form.

**Optimization:** If events have disjoint write sets AND their triggered invariants have disjoint footprints, skip exhaustive check (proven by structure).

#### CC2: Compensation Stability (Implicit)

The normal form computation already ensures:

```
NF(apply(e, NF(s))) == NF(apply(e, s))
```

Because `NF(s)` is a valid state and applying `e` then normalizing produces the same result regardless of whether we start from valid or invalid states.

**CC passes if:** All independent event pairs commute in all states.

## Runtime Execution

### Machine Structure

```go
type Machine struct {
    name   string
    vars   []Var
    events map[string]int      // event name → index
    step   [][]uint64          // step[eventIdx][stateID] → normal form
    nf     []uint64            // nf[stateID] → normal form
}
```

### Event Application (O(1))

```go
func (m *Machine) Apply(s State, event string) State {
    ei := m.events[event]           // O(1) map lookup
    newID := m.step[ei][s.packed]   // O(1) array index
    return State{packed: newID, vars: m.vars}
}
```

**Why O(1):**
- No conditionals on state values
- No invariant checking
- No compensation logic
- Single array index: `step[eventIdx][stateID]`

### Example: Order System

State space:

```
status: pending(0), paid(1), shipped(2)
paid: false(0), true(1)

States:
0: {status=pending, paid=false}
1: {status=pending, paid=true}
2: {status=paid, paid=false}
3: {status=paid, paid=true}
4: {status=shipped, paid=false}
5: {status=shipped, paid=true}
```

Normal form table (after compensation):

```
NF[0] = 0  // valid
NF[1] = 1  // valid
NF[2] = 2  // valid
NF[3] = 3  // valid
NF[4] = 0  // invalid: shipped but unpaid → repair to pending
NF[5] = 5  // valid
```

Step tables:

```
Step[pay][0] = NF(apply(pay, 0)) = NF(3) = 3  // pending→paid, paid=true
Step[ship][0] = NF(apply(ship, 0)) = NF(2) = 0  // pending→shipped, unpaid → repair to pending
```

Runtime execution:

```go
s := machine.NewState()          // s.packed = 0
s = machine.Apply(s, "ship")     // s.packed = step[ship][0] = 0
s = machine.Apply(s, "pay")      // s.packed = step[pay][0] = 3
```

Final state: `{status=paid, paid=true}` - converged despite invalid intermediate state.

## Compensation System

### Priority Ordering

Invariants are checked in declaration order. When multiple invariants are violated, the first one fires:

```go
b.Invariant("inv1") // Higher priority
b.Invariant("inv2") // Lower priority
```

If both are violated, `inv1`'s repair fires first. After repair, if `inv2` is still violated, it fires next.

### Footprint Isolation

Each invariant declares its footprint - which variables it reads/writes:

```go
b.Invariant("cap").
    Over(count).  // Footprint: {count}
    Check(...)
    Repair(...)
```

Repairs must only modify variables in the footprint. This enables:
- Independent compensation analysis
- Disjointness optimizations for CC checking
- Clear separation of concerns

### Idempotence on Valid States

A critical property: **compensation must be identity on valid states**.

If `s` is valid (all invariants hold):

```
NF(s) == s
```

This is verified during build. If violated, `Build()` returns an error:

```
gsm: compensation moves valid state {...} — repair must be identity on valid states
```

## Export Format

The `Export()` method serializes verification tables to JSON for multi-language runtimes:

```json
{
  "name": "order_system",
  "version": 1,
  "vars": [
    {"name": "status", "kind": "enum", "domain": 3, "labels": ["pending", "paid", "shipped"]},
    {"name": "paid", "kind": "bool", "domain": 2}
  ],
  "events": ["pay", "ship"],
  "nf": [0, 1, 2, 3, 0, 5],
  "step": [
    [3, 3, 3, 3, 3, 5],  // pay
    [0, 1, 5, 5, 0, 5]   // ship
  ],
  "exported_at": "2026-02-18T07:00:00Z"
}
```

Runtimes in Python, JavaScript, Rust, etc. can load this JSON and implement O(1) event application with the same convergence guarantees.

## Scalability

### State Space Limits

Maximum: 1,048,576 states (2^20)

This is checked during `Build()`:

```go
if b.totalBits > 20 {
    return nil, nil, fmt.Errorf("state space too large")
}
```

Why 20 bits:
- Reasonable for exhaustive verification (< 1 second on modern CPUs)
- Keeps table sizes manageable (~8MB for Step tables with 10 events)
- Covers most business logic state machines

### Memory Usage

For a machine with `N` states and `E` events:

- `NF` table: `N × 8 bytes` (uint64)
- `Step` tables: `E × N × 8 bytes`
- Total: `(E + 1) × N × 8 bytes`

Example:
- 10,000 states, 5 events
- Memory: 6 × 10,000 × 8 = 480 KB

### CC Checking Complexity

Worst case: O(E² × N) where E = number of events, N = state count

**Optimizations:**
1. **Disjoint footprints** - Skip exhaustive check if events don't overlap
2. **Declared independence** - Only check explicitly declared pairs
3. **Early termination** - Stop on first CC violation

In practice:
- Most event pairs are disjoint (different write sets)
- Verification completes in milliseconds for typical systems

## Design Rationale

### Why Exhaustive Verification?

Alternative: Model checking with symbolic exploration

**Chosen approach:** Exhaustive enumeration

**Reasoning:**
- State machines are finite by design (domain-bounded variables)
- Exhaustive check is sound and complete - no false positives/negatives
- Fast enough for practical systems (< 1M states)
- Simpler implementation, easier to debug

### Why Bitpacking?

Alternative: Hash-based state indexing

**Chosen approach:** Bitpacked uint64

**Reasoning:**
- Contiguous indices enable dense array storage
- No hash collisions or bucket management
- Cache-friendly memory layout
- Direct state ID usage without indirection

### Why Precomputed Tables?

Alternative: Runtime invariant checking and compensation

**Chosen approach:** Precompute all transitions

**Reasoning:**
- O(1) runtime with zero branching
- Deterministic performance (no worst-case scenarios)
- Enables multi-language runtimes via JSON export
- Verification is one-time cost during build

### Why Priority-Ordered Compensation?

Alternative: Parallel/concurrent compensation firing

**Chosen approach:** Sequential priority order

**Reasoning:**
- Deterministic repair behavior
- Easier to reason about for developers
- Simpler verification algorithm
- Matches real-world business rule hierarchies

## Related Work

This design is based on the theory in:

[*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18677400) (Blackwell, 2026)

The paper proves:

1. **WFC** ensures compensation terminates (well-founded ordering)
2. **CC** ensures event orderings converge (commutativity modulo normalization)
3. Together, WFC + CC guarantee eventual consistency

This library implements the verification algorithm and provides a practical runtime based on those proofs.
