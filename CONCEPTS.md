# Foundational Concepts

This document explains the theoretical foundations of governed state machines and how they map to the `gsm` library. It's designed for readers who want to understand **why** the library works, not just **how** to use it.

If you're looking for:
- **Quick start**: See [README.md](README.md)
- **API reference**: See [README.md#api-overview](README.md#api-overview)
- **Mathematical foundations**: See [THEORY.md](THEORY.md)
- **Implementation details**: See [ARCHITECTURE.md](ARCHITECTURE.md)
- **Academic paper**: See [Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)

## Table of Contents

- [What Problem Are We Solving?](#what-problem-are-we-solving)
- [Core Definitions](#core-definitions)
  - [State Space](#state-space)
  - [Normal Forms](#normal-forms)
  - [Compensation](#compensation)
  - [Convergence](#convergence)
- [The Two Properties](#the-two-properties)
  - [WFC: Well-Founded Compensation](#wfc-well-founded-compensation)
  - [CC: Compensation Commutativity](#cc-compensation-commutativity)
- [Why These Properties Guarantee Convergence](#why-these-properties-guarantee-convergence)
- [Visual Examples](#visual-examples)
- [Glossary: Paper Terms → Code](#glossary-paper-terms--code)
- [Common Misconceptions](#common-misconceptions)
- [Further Reading](#further-reading)

---

## What Problem Are We Solving?

**The central challenge**: In distributed systems, events arrive out of order. When events can violate business rules, we need compensation (repair) to restore validity. But compensation itself can behave differently depending on event order, leading to **divergence** — replicas seeing the same events end up in different states.

**Example**: Two replicas process events `[ship, pay]` and `[pay, ship]`:

```
Replica A: ship → (invalid: shipped unpaid) → repair → pending
           pay  → paid
           Final: paid

Replica B: pay  → paid
           ship → shipped
           Final: shipped
```

**Different final states!** This is divergence.

`gsm` solves this by verifying at build time that your compensation strategy guarantees convergence: **all replicas reach the same valid state regardless of event order**.

---

## Core Definitions

### State Space

The **state space** is the set of all possible states your system can be in.

In `gsm`, states are defined by **finite-domain variables**:

```go
status := b.Enum("status", "pending", "paid", "shipped")  // 3 values
paid := b.Bool("paid")                                     // 2 values
count := b.Int("count", 0, 5)                              // 6 values

// Total state space: 3 × 2 × 6 = 36 states
```

Each state is a unique assignment of values to all variables:
- State 0: `{status=pending, paid=false, count=0}`
- State 1: `{status=pending, paid=false, count=1}`
- ...
- State 35: `{status=shipped, paid=true, count=5}`

**Why finite?** Because we enumerate all states at build time to verify convergence properties exhaustively.

**Internally**: States are bitpacked into a single `uint64`, enabling O(1) table lookups. See [ARCHITECTURE.md#state-representation](ARCHITECTURE.md#state-representation) for details.

---

### Normal Forms

A **normal form** is the unique valid state reached by repeatedly applying compensation until all invariants hold.

**Definition**: For any state `s`, its normal form `NF(s)` is:
1. Reachable from `s` via zero or more repair steps
2. Valid (all invariants hold)
3. Stable (further repairs don't change it)

**Example**:

```
State: {status=shipped, paid=false}  ← Invalid (violates "no ship unpaid")
  ↓ repair (set status=pending)
State: {status=pending, paid=false}  ← Valid (no violations)
  ↓ repair is no-op
NF = {status=pending, paid=false}
```

**In code**: The `nf` table precomputes normal forms for every state:

```go
nf[4] = 0  // State 4 (shipped, unpaid) → State 0 (pending, unpaid)
```

**Key property**: If `s` is already valid, then `NF(s) = s` (repairs are identity on valid states).

---

### Compensation

**Compensation** is the automatic repair process that fires when events violate invariants.

#### Structure

An **invariant** has three parts:

1. **Footprint** (via `Watches`): Which variables it constrains
2. **Check** (via `Holds`): Boolean predicate that must be true
3. **Repair** (via `Repair`): Function to restore validity

```go
b.Invariant("no_overdraft").
    Watches(balance).             // Footprint: {balance}
    Holds(func(s State) bool {
        return s.GetInt(balance) >= 0
    }).
    Repair(func(s State) State {
        return s.SetInt(balance, 0)  // Clamp to zero
    }).
    Add()
```

#### Execution

When an event produces an invalid state:
1. Check all invariants in declaration order
2. If one fails, apply its repair
3. Repeat from step 1 until all hold (or detect non-termination)

**Priority**: Invariants fire in declaration order. The first violated invariant repairs first.

#### Constraints

- Repairs must only modify variables in the invariant's footprint
- Repairs must be **identity on valid states** (if `s` is valid, `repair(s) = s`)
- Repairs must **terminate** (reach a valid state in finite steps)

---

### Convergence

**Convergence** means all replicas consuming the same events (in any order) reach the same valid final state.

**Formally**: For any two event sequences that differ only in ordering:
```
s₀ --e₁--> s₁ --e₂--> s₂  →  NF(s₂)
s₀ --e₂--> s₁' --e₁--> s₂'  →  NF(s₂')

Convergence: NF(s₂) = NF(s₂')
```

**Without convergence**:
```
State 0 --ship--> State A --pay--> State B → NF = State X
State 0 --pay--> State C --ship--> State D → NF = State Y

If X ≠ Y, replicas diverge!
```

**With convergence** (verified by `gsm`):
```
State 0 --ship--> State A --pay--> State B → NF = State X
State 0 --pay--> State C --ship--> State D → NF = State X

X = X, replicas converge!
```

`gsm` **verifies convergence at build time** by checking all possible event orderings exhaustively.

---

## The Two Properties

`gsm` verifies two mathematical properties that together guarantee convergence:

### WFC: Well-Founded Compensation

**Well-Founded Compensation** ensures that compensation always terminates and reaches a valid state.

#### What It Checks

For every state `s`:
1. Apply repairs until all invariants hold
2. Track visited states to detect cycles
3. Fail if:
   - Same state visited twice (cycle detected)
   - Depth exceeds total state count (impossible in terminating system)

#### Why It Matters

Without WFC, compensation could loop forever:

```
State A (violates inv1) --repair1--> State B (violates inv2)
State B (violates inv2) --repair2--> State A (violates inv1)
← Infinite loop!
```

WFC ensures this never happens.

#### Example: WFC Violation

```go
b.Invariant("force_even").
    Watches(x).
    Holds(func(s State) bool {
        return s.GetInt(x) % 2 == 0
    }).
    Repair(func(s State) State {
        return s.SetInt(x, s.GetInt(x) + 1)  // Make odd
    }).
    Add()

b.Invariant("force_odd").
    Watches(x).
    Holds(func(s State) bool {
        return s.GetInt(x) % 2 == 1
    }).
    Repair(func(s State) State {
        return s.SetInt(x, s.GetInt(x) + 1)  // Make even
    }).
    Add()

// Build() fails: WFC violation (repair cycle)
```

#### In the Report

```
WFC: PASS (max repair depth: 2)
```

This means every state reaches validity in at most 2 repair steps.

---

### CC: Compensation Commutativity

**Compensation Commutativity** ensures that different event orderings converge to the same normal form.

#### What It Checks

For every pair of independent events `(e1, e2)` and every valid state `s`:

```
Apply e1 then e2:  s --e1--> s' --e2--> s''  → NF(s'')
Apply e2 then e1:  s --e2--> t' --e1--> t''  → NF(t'')

CC requires: NF(s'') = NF(t'')
```

**Important**: CC is checked **after normalization**. The intermediate states `s''` and `t''` can differ, but their normal forms must be identical.

#### Why It Matters

Events can produce different intermediate states depending on order, but compensation must bring them to the same final state:

```
State: {status=pending, paid=false}

Order 1: ship → {shipped, false} (invalid) → repair → {pending, false}
         pay  → {paid, true}
         Final: {paid, true}

Order 2: pay  → {paid, true}
         ship → {shipped, true}
         Final: {shipped, true}

Different finals! CC fails.
```

To fix: Ensure the `ship` event guards on payment:

```go
b.Event("ship").
    Guard(func(s State) bool {
        return s.GetBool(paid)  // Only ship if paid
    }).
    ...
```

Now both orders converge to `{shipped, true}`.

#### Optimization: Disjoint Footprints

If two events have **disjoint footprints** (no shared variables, no shared invariant footprints), CC is automatically satisfied without exhaustive checking:

```go
b.Event("deposit").Writes(balance)      // Footprint: {balance}
b.Event("send_email").Writes(notified)  // Footprint: {notified}

// Disjoint footprints → automatically commutative
```

#### In the Report

```
CC (Compensation Commutativity): PASS (10 pairs: 7 disjoint, 3 brute-force)
```

This means:
- 10 event pairs checked
- 7 proved by footprint analysis (fast)
- 3 proved by exhaustive state checking (slower)

---

## Why These Properties Guarantee Convergence

**Theorem** (Newman's Lemma, adapted): If a rewrite system is:
1. **Terminating** (every sequence of rewrites reaches a normal form)
2. **Locally confluent** (divergent one-step rewrites can be joined)

Then it is **globally confluent** (all rewrite sequences converge to the same normal form).

**Mapping to gsm**:
- **Terminating** → **WFC**: Compensation reaches validity in finite steps
- **Locally confluent** → **CC**: Event pairs converge after compensation
- **Globally confluent** → **Convergence**: All event orderings reach the same state

**In plain English**:
- WFC ensures compensation doesn't loop forever
- CC ensures any two events can be "joined" to the same result
- Together: no matter what order events arrive, compensation brings you to the same valid state

**Proof**: See Section 5 of the paper.

---

## Visual Examples

### Example 1: Converging Compensation

```
Events: [ship, pay] in either order

Order 1: ship → pay
    {pending, unpaid}
    --ship--> {shipped, unpaid}  [INVALID: violates "no ship unpaid"]
              ↓ repair
              {pending, unpaid}
    --pay--> {paid, paid}
    Final: {paid, paid}

Order 2: pay → ship
    {pending, unpaid}
    --pay--> {paid, paid}
    --ship--> {shipped, paid}
    Final: {shipped, paid}

Both converge to valid states (CC requires they be the same)
```

### Example 2: Diverging Compensation (CC Violation)

```
Events: [grant_read, grant_write]
Invariant: "can't write without read" → repair: revoke write

Order 1: grant_read → grant_write
    {read=false, write=false}
    --grant_read--> {read=true, write=false}
    --grant_write--> {read=true, write=true}
    Final: {read=true, write=true}

Order 2: grant_write → grant_read
    {read=false, write=false}
    --grant_write--> {read=false, write=true}  [INVALID]
                     ↓ repair
                     {read=false, write=false}
    --grant_read--> {read=true, write=false}
    Final: {read=true, write=false}

Different finals! CC violation!
```

To fix: Make `grant_write` guard on `read=true`.

### Example 3: WFC Cycle (Termination Failure)

```
Invariant 1: "x must be even" → repair: x = x + 1
Invariant 2: "x must be odd"  → repair: x = x + 1

State: {x=0} (even)
--event--> {x=1} (odd)
  inv1 fails → repair → {x=2} (even)
  inv2 fails → repair → {x=3} (odd)
  inv1 fails → repair → {x=4} (even)
  ... infinite loop!

WFC failure: compensation does not terminate
```

---

## Glossary: Paper Terms → Code

| Paper Term | Code Equivalent | Meaning |
|------------|----------------|---------|
| **Registry** | `Registry` / `Machine` | The state machine definition |
| **Processor** | Application instance | A replica consuming events |
| **State** | `State` struct | Assignment of values to variables |
| **Event** | `Event` | Operation that modifies state |
| **Invariant** | `Invariant` | Business rule that must hold |
| **Normalization** | `Normalize()` / `nf` table | Applying compensation to reach validity |
| **V_R(s)** | `allInvariantsHold(s)` | All invariants hold on state s |
| **ρ_R(s)** | `applyFirstRepair(s)` | Apply first violated invariant's repair |
| **NF_R(s)** | `nf[s.packed]` | Normal form of state s |
| **WFC** | Well-Founded Compensation | Compensation terminates |
| **CC** | Compensation Commutativity | Event orders converge |
| **CC1** | Event commutativity | e1;e2 ≈ e2;e1 (after normalization) |
| **CC2** | Compensation stability | NF(apply(e, NF(s))) = NF(apply(e, s)) |
| **Footprint** | `Watches(vars...)` | Variables an invariant constrains |
| **Write set** | `Writes(vars...)` | Variables an event modifies |
| **Step table** | `step[e][s]` | Precomputed NF(apply(e, s)) |

---

## Common Misconceptions

### "Events must commute"

**False**. Events themselves don't need to commute. Their **normal forms** after compensation must converge:

```
ship; pay → State A → NF(A) = X
pay; ship → State B → NF(B) = X

A ≠ B is fine, as long as NF(A) = NF(B)
```

This is **compensation commutativity**, not raw operation commutativity (like CRDTs).

---

### "Compensation runs at runtime"

**False**. Compensation is **precomputed at build time**. The `step` table contains the final result after compensation:

```go
machine.Apply(s, "ship")  // O(1) table lookup: step[ship][s]
```

No repair functions execute at runtime. Everything is baked into lookup tables during `Build()`.

---

### "All event pairs must be independent"

**False**. By default, `gsm` checks all pairs. But you can use `OnlyDeclaredPairs()` to check only specific pairs:

```go
b.OnlyDeclaredPairs()
b.Independent("deposit", "notify")  // These two can happen in either order
b.Independent("withdraw", "notify")
// Other pairs not checked
```

Use this when you know some events are causally ordered (e.g., `pay` always before `ship`).

---

### "Finite state spaces are a limitation"

**Perspective**. Finite state spaces enable **exhaustive verification**. You trade:
- Cannot model unbounded domains (arbitrary strings, lists)
- Gain mathematical proof of convergence (can't get this with infinite spaces)

For business logic state machines (order workflows, authorization states, inventory counts), finite domains are natural.

---

### "This is the same as CRDTs"

**Not quite**. CRDTs require operations to **commute directly**. `gsm` allows operations to violate invariants, then **compensates** to restore validity. The compensation brings you to the same state regardless of order.

CRDTs: `op1; op2 = op2; op1` (operations commute)
gsm: `NF(op1; op2) = NF(op2; op1)` (normal forms converge)

---

## Further Reading

### Academic Background

- **[Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)** (Blackwell, 2026)
  The paper this library implements

- **[Newman's Lemma](https://en.wikipedia.org/wiki/Newman%27s_lemma)**
  Foundation for confluence proofs (local confluence + termination → global confluence)

- **[Abstract Rewriting](https://en.wikipedia.org/wiki/Abstract_rewriting_system)**
  General theory of rewrite systems and confluence

### Related Approaches

- **[CRDTs](https://crdt.tech/)** (Conflict-free Replicated Data Types)
  Alternative approach via operation commutativity

- **[Invariant Confluence](https://dl.acm.org/doi/10.14778/2735508.2735509)** (Bailis et al., VLDB 2014)
  Coordination-free execution when operations preserve invariants

- **[TLA+](https://lamport.azurewebsites.net/tla/tla.html)**
  Formal specification and verification (different approach: model checking vs. exhaustive enumeration)

### Implementation Details

- [ARCHITECTURE.md](ARCHITECTURE.md) — How `gsm` implements the theory
- [README.md](README.md) — API reference and usage examples
- [nccheck](https://github.com/blackwell-systems/nccheck) — YAML-based verifier (reference implementation from paper)

---

## Summary

**Key Takeaways**:

1. **Normal forms** are unique valid states reached via compensation
2. **WFC** ensures compensation terminates (no infinite loops)
3. **CC** ensures different event orders converge (same normal form)
4. **Together**: WFC + CC guarantee convergence (proven via Newman's Lemma)
5. **Build time**: Verification exhaustively checks all states and event pairs
6. **Runtime**: O(1) event application via precomputed lookup tables

`gsm` takes a hard problem (distributed convergence) and makes it tractable through:
- Finite state spaces (enables exhaustive checking)
- Separation of concerns (verification at build time, execution at runtime)
- Mathematical foundations (Newman's Lemma gives convergence proof)

When you call `registry.Build()` and it returns `Convergence: GUARANTEED`, that's not marketing — it's a mathematically proven property of your state machine.
