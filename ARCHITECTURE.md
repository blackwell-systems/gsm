# Architecture

This document explains the internal design of gsm, how convergence verification works, and why runtime event application
is O(1) with zero compensation overhead.

For other documentation:
- **Usage guide**: [README.md](README.md)
- **Conceptual introduction**: [CONCEPTS.md](CONCEPTS.md)
- **Mathematical foundations**: [THEORY.md](THEORY.md)

## Overview

gsm separates verification (build-time) from execution (runtime):

- **Build time**: Exhaustive state-space enumeration verifies WFC and CC properties
- **Runtime**: O(1) table lookups apply events and return normal forms

All compensation is precomputed during `Build()`. The runtime `Machine` contains only lookup tables.

There is also a build-time *synthesis* path (`synthesis.go`, `Registry.Synthesize`): the
inverse of verification. Instead of checking a compensation you supplied, it searches — via
backtracking with forward-checking — for a normal-form map on invalid states that satisfies CC,
generating a convergent compensation (or proving none exists, with a witness). It shares the
state-space enumeration and CC machinery with `Build`.

Two later sections cover paths that extend this core: [Compositional
Verification](#compositional-verification-buildcompositional) verifies machines whose global
state space is too large to enumerate by checking each footprint component independently, and
[Differential Testing via Extracted Oracles](#differential-testing-via-extracted-oracles)
re-certifies a built machine against checkers extracted from an axiom-free Coq proof.

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

**Optimization:** If events have disjoint write sets AND their triggered invariants have disjoint footprints,
skip exhaustive check (proven by structure).

#### CC2: Compensation Stability (Implicit)

The normal form computation already ensures:

```
NF(apply(e, NF(s))) == NF(apply(e, s))
```

Because `NF(s)` is a valid state and applying `e` then normalizing produces the same result regardless of whether we
start from valid or invalid states.

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

Runtimes in Python, JavaScript, Rust, etc. can load this JSON and implement O(1) event application with the same
convergence guarantees.

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

## Compositional Verification (BuildCompositional)

The Phase 1 and Phase 3 algorithms above enumerate the **global** state space, which caps
`Build` at the 20-bit ceiling. But WFC and CC are local properties: a repair and an event effect
touch only their declared footprint, and events with disjoint footprints commute by structure
(the mechanized `disjoint_events_commute` result). So a registry partitions into
footprint-connected **components** that never interact, and it suffices to verify each component
over the subspace of its own variables.

`Registry.BuildCompositional` does exactly this:

1. **Partition** variables into footprint-connected components using union-find: two variables
   join the same component when some invariant footprint or event write set mentions both.
2. **Verify each component locally.** Enumerate only that component's subspace and run the WFC
   and CC checks over it. A component of `k` variables costs the product of *those* domains, not
   the whole machine's.
3. **Skip cross-component pairs.** Events whose footprints land in different components commute by
   disjointness, so no exhaustive check is needed across components; only same-component pairs are
   brute-forced (within their small subspace).

**Complexity:** exponential in the *largest component*, not in the whole machine. A registry of
many independent small invariants certifies even when its global state space is astronomically
large.

**Trade-off:** `BuildCompositional` returns a **lazy** `Machine` that computes `Apply` and
`Normalize` at runtime from the rules (there are no global tables to precompute), so `Export` is
unavailable (it needs the flat step tables that only global `Build` materializes). Runtime is no
longer a single array lookup; it evaluates the rules for the touched component.

**Preconditions:** every invariant declares its footprint, every event declares its write set
(both automatic when rules are written with the combinator vocabulary, see "Rule layers" below),
and the zero state is valid. No single component may exceed the enumeration budget.

## Differential Testing via Extracted Oracles

gsm's own verification is written in Go. To guard against the class of bug where a hand-written
verifier wrongly accepts a non-convergent machine, a built machine can be re-certified by
**checkers extracted from an axiom-free Coq proof** (the sibling `normalization-confluence` repo,
under `coq/extraction`). Because the checking logic is extracted from a machine-checked proof, a
bug in gsm's Go cannot make a non-convergent machine pass. There are two, at different trust
boundaries:

1. **Table oracle** (`Machine.WriteConvergenceTables`, cross-checked in `oracle_test.go` when
   `GSM_CONVERGENCE_CHECKER` points at the built binary). gsm emits the machine's step tables;
   the checker re-verifies that the per-event step functions commute and stay in range. It trusts
   that gsm computed the tables, and independently confirms those tables converge.

2. **Rules oracle** (`Registry.WriteMachineAST`, cross-checked in `astoracle_test.go` when
   `GSM_AST_CHECKER` points at the built binary). gsm serializes the combinator **rules**
   themselves (as S-expressions); the checker recomputes each event's step function by evaluating
   the rule AST (apply the event, then normalize by iterated repair) and confirms convergence from
   scratch. It trusts neither gsm's enumeration nor its tables: it re-derives the verdict straight
   from the declarations.

If either oracle ever disagrees with gsm's Go verdict, one of them has a bug, and the extracted,
proof-derived one is the reference. See the extraction README in `normalization-confluence` for
the file formats and how to build the binaries.

### Rule layers

Combinator rules (see README and CONCEPTS) come in two spellings that both lower to the **same**
primitive AST: an ergonomic surface (`AtMost`, `Inc`, `SetTo`, and the `Rule`/`On` builders) for
humans, and the primitive combinators (`Le`, `Set`, `Add`, `DeclInvariant`, `DeclEvent`) that are
serialized, tested, and handed to the oracle. A test pins that the two spellings serialize to
byte-identical rules, so the friendly layer cannot carry any semantics the analyzable core (and
therefore the verifier) does not see. Only rules built from this vocabulary are serializable;
closure-based invariants and events cannot be exported, and `WriteMachineAST` returns an error
rather than emit something the oracle would misread.

`Registry.PolicyBytes` exposes those serialized bytes and `Registry.PolicyDigest` a stable,
domain-separated SHA-256 over them, so a policy is a portable, nameable artifact: the digested
bytes are exactly the oracle's input, so an anchored digest and the re-checked artifact cannot
diverge. This is what lets an external audit layer commit a policy's identity in a log and hand
the same bytes to the rules oracle.

## Related Work

This design is based on the theory in:

[*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18677400) (Blackwell, 2026)

The paper proves:

1. **WFC** ensures compensation terminates (well-founded ordering)
2. **CC** ensures event orderings converge (commutativity modulo normalization)
3. Together, WFC + CC guarantee eventual consistency

This library implements the verification algorithm and provides a practical runtime based on those proofs.
