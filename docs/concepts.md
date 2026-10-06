# Concepts

This document explains the theoretical foundations of governed state machines and how they map to the `gsm` library. It's designed for readers who want to understand **why** the library works, not just **how** to use it.

If you're looking for:
- **Quick start**: See [README.md](../README.md) and [Getting started](getting-started.md)
- **API reference**: See [Reference](reference.md) and [pkg.go.dev](https://pkg.go.dev/github.com/blackwell-systems/gsm)
- **Many registries**: See [Federation](federation.md) (read this page first)
- **Mathematical foundations**: See [Theory](theory.md)
- **Implementation details**: See [ARCHITECTURE.md](design/ARCHITECTURE.md)
- **Academic paper**: See [Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)

## Table of Contents

- [The third way to make distributed state agree](#the-third-way-to-make-distributed-state-agree)
- [What Problem Are We Solving?](#what-problem-are-we-solving)
- [Core Definitions](#core-definitions)
  - [State Space](#state-space)
  - [Normal Forms](#normal-forms)
  - [Invariants and Compensation](#invariants-and-compensation)
  - [Events](#events)
  - [Convergence](#convergence)
- [The Two Properties](#the-two-properties)
  - [WFC: Well-Founded Compensation](#wfc-well-founded-compensation)
  - [CC: Compensation Commutativity](#cc-compensation-commutativity)
- [Why These Properties Guarantee Convergence](#why-these-properties-guarantee-convergence)
- [Visual Examples](#visual-examples)
- [Why you do not have to take the library's word for it](#why-you-do-not-have-to-take-the-librarys-word-for-it)
- [Where CRDTs Fit](#where-crdts-fit)
- [Glossary: Paper Terms → Code](#glossary-paper-terms--code)
- [Common Misconceptions](#common-misconceptions)
- [Further Reading](#further-reading)

---

## The third way to make distributed state agree

Two replicas. The same events. They arrive in different orders. Do they end up in the same state?

If you have ever run a distributed system, you know the two textbook answers, and you know what each one costs you.

**Coordinate first.** Put a consensus protocol in front of every write. Raft, Paxos, a lock service. Now every replica sees the same order, so of course they agree. You paid for it in latency: a write is not done until a quorum says so, and a partition can stall you completely.

**Make the operations commute.** This is the CRDT answer. If `A then B` always equals `B then A`, order stops mattering and you can drop the coordination. Counters, sets, last-writer-wins registers all live here. It is a genuinely beautiful idea. The catch is the precondition: your operations have to commute. The moment a write can violate a business rule (ship an unpaid order, overdraw an account, approve a loan that is not funded) commutativity is not something you get to assume. Merging two "valid" replica states can produce a state that is not valid at all, and a CRDT has no notion of "not valid."

So the working engineer's mental model is: fast-but-limited (CRDTs) or expressive-but-slow (consensus). Pick your pain.

There is a third option, and it is not a compromise between those two. It is a different axis. What if distributed systems don't have to coordinate, because they agree on the rules ahead of time, so ordering and compensations are deterministic?

**Agree on the rules, not the order.** Instead of coordinating on *order*, agree ahead of time on *the rules* and on *how to repair a violation of them*. Then any order of events converges to the same valid state, with no coordination at write time. Concretely you declare three things:

1. **Variables** with finite domains: a status enum, a paid flag, an inventory count.
2. **Invariants**: what "valid" means. "An order ships exactly when it is paid and a shipment was requested." Along with each invariant, a **repair**: if this is violated, here is how to fix it. ("Recompute the status from the facts.")
3. **Events**: the operations, which are allowed to violate invariants.

The README's [order fulfillment example](../README.md#example-order-fulfillment) is this machine in full. Each event records a fact and never touches the status. An event can leave the status stale, an invalid state (paid and requested, but still open). That is fine. The compensation repairs it, and because the repair obeys two algebraic properties, every ordering of these events lands on the same valid normal form. (The tempting first draft, "ship sets the status, and a repair un-ships an unpaid order", does not converge: ship-then-pay ends paid-but-not-shipped while pay-then-ship ends shipped. `Build` rejects it with exactly those two orders as the counterexample.) No consensus. No requirement that the operations commute. That underlying result has a name: **normalization confluence**. It proves that compensation is sufficient for convergence when two algebraic properties hold, without the expressiveness limits of CRDTs or the latency cost of consensus.

**Even the hard case has a graceful answer.** Some systems genuinely cannot converge on their own. The classic shape is a cycle across registries: A mirrors B, and B is forced to disagree with A. gsm tells you so rather than pretend, shows you the oscillation, and computes a small, correct set of connection points to coordinate so the rest can run free ([Federation](federation.md#escape-hatch-3-coordinate-the-obstruction)).

The one-line version: you do not have to choose between fast-and-limited and expressive-and-slow. If you can say what valid means and how to repair a violation, you can have coordination-free convergence over operations that break the rules, and a machine-checked proof that it holds.

---

## What Problem Are We Solving?

**The central challenge**: In distributed systems, events arrive out of order. When events can violate business rules, we need compensation (repair) to restore validity. But compensation itself can behave differently depending on event order, leading to **divergence**: replicas seeing the same events end up in different states.

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

<!-- gocheck: check registry -->
```go
status := r.Enum("status", "pending", "paid", "shipped")  // 3 values
paid := r.Bool("paid")                                     // 2 values
count := r.Int("count", 0, 5)                              // 6 values

// Total state space: 3 × 2 × 6 = 36 states
```

Each state is a unique assignment of values to all variables:
- State 0: `{status=pending, paid=false, count=0}`
- State 1: `{status=pending, paid=false, count=1}`
- ...
- State 35: `{status=shipped, paid=true, count=5}`

**Why finite?** Because `Build` enumerates all states at build time to verify convergence exhaustively. Only variable *domains* need be finite: for a machine too large to enumerate, `Build` (with combinator rules) and `BuildCompositional` verify per footprint component instead of the global product, so the total state space can still be astronomically large.

**Internally**: States are bitpacked into a single `uint64`, enabling O(1) table lookups. See [ARCHITECTURE.md#state-representation](design/ARCHITECTURE.md#state-representation) for details.

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

<!-- gocheck: excerpt an entry of the internal normal-form table -->
```go
nf[4] = 0  // State 4 (shipped, unpaid) → State 0 (pending, unpaid)
```

**Key property**: If `s` is already valid, then `NF(s) = s` (repairs are identity on valid states).

---

### Invariants and Compensation

An **invariant** is a property that must always hold on valid states. Each invariant has three parts:

- **`Watches(vars...)`**: Declares which variables the invariant depends on (its "footprint"). The repair function can only modify these variables.
- **`Holds(func)`**: The boolean condition that must be true. When this returns false, compensation fires.
- **`Repair(func)`**: How to fix states where the invariant is violated. This is the compensation function.

<!-- gocheck: check registry -->
```go
r.Invariant("no_overdraft").
    Watches(balance).             // Footprint: {balance}
    Holds(func(s State) bool {
        return s.GetInt(balance) >= 0
    }).
    Repair(func(s State) State {
        return s.SetInt(balance, 0)  // Clamp to zero
    }).
    Add()
```

**Compensation** is the automatic repair process that fires when events violate invariants:

1. Event modifies state (e.g., "withdraw $100")
2. Invariant check fails (e.g., balance becomes -50)
3. Repair function fires (e.g., set balance to 0)
4. Repeat until all invariants hold (or detect non-termination)

**Priority**: Invariants fire in **declaration order**. If multiple invariants are violated, the first one repairs first, then the next, until all hold.

**Constraints**:

- Repairs must only modify variables in the invariant's footprint
- Repairs must be **identity on valid states** (if `s` is valid, `repair(s) = s`)
- Repairs must **terminate** (reach a valid state in finite steps)

For convergence, compensation must be **well-founded** (WFC: repairs eventually terminate, no infinite loops) and **commutative** (CC: event order doesn't matter after compensation runs). The library **verifies both properties at build time** by exhaustively checking all possible states and event orderings; [The Two Properties](#the-two-properties) explains each.

---

### Events

**Events** are operations that modify state. Each event declares:

- **`Writes(vars...)`**: Which variables this event modifies
- **`Guard(func)`**: Optional precondition - if false, event is a no-op
- **`Apply(func)`**: The effect function that transforms the state

Events can arrive **in any order**. The library verifies that different orderings converge to the same final state.

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

"The same events" means the same multiset, each event delivered once. A redelivered event (an
at-least-once queue, a retry) is a different multiset. See
[Deployment](deployment.md#delivery) for which events tolerate that.

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

<!-- gocheck: check registry -->
```go
r.Invariant("force_even").
    Watches(x).
    Holds(func(s State) bool {
        return s.GetInt(x) % 2 == 0
    }).
    Repair(func(s State) State {
        return s.SetInt(x, s.GetInt(x) + 1)  // Make odd
    }).
    Add()

r.Invariant("force_odd").
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

A tempting fix is to guard `ship` on payment. It does not work:

<!-- gocheck: check registry -->
```go
r.Event("ship").Writes(status).
    Guard(func(s State) bool {
        return s.GetBool(paid) // reads a variable another event writes
    }).
    Apply(func(s State) State { return s.Set(status, "shipped") }).
    Add()
```

```
Order 1: ship → guard false, no-op → {pending, false}
         pay  → {paid, true}
         Final: {paid, true}

Order 2: pay  → {paid, true}
         ship → {shipped, true}
         Final: {shipped, true}
```

Still two finals: a guard that reads another event's write makes the event's effect depend on
what arrived first. The fix that converges is to make each event record a fact that reads nothing
else (`pay` sets `paid`, `request_ship` sets `ship_requested`) and let an invariant derive the
status from the facts (`shipped` exactly when paid and requested). Both orders then end in the
same state, because the status is recomputed from the same facts. The README's
[order fulfillment example](../README.md#example-order-fulfillment) is this machine in full.

#### Disjoint Footprints

Two events that write different variables do **not** automatically commute: the guard above
reads `paid`, which `pay` writes, so `ship` and `pay` touch disjoint write sets and still diverge.
Commutation by disjointness needs every event to *read* only its own footprint as well.

- `Build`'s global check therefore checks every independent pair exhaustively; it never skips a
  pair by footprint.
- The per-component check (`Build` on a registry too large to enumerate, `BuildCompositional`)
  skips pairs whose events lie in different footprint components, with every rule's reads in its
  footprint. Written with combinators, the guarded `ship` above reads `paid`, so `paid` and
  `shipped` land in one component and the check fails the pair, as the global check does; written
  as a closure that declares only `Writes(shipped)`, `BuildCompositional` rejects it with a
  footprint violation naming `paid`.

<!-- gocheck: check registry -->
```go
r.Event("deposit").Writes(balance).    // reads and writes only balance
    Apply(func(s State) State { return s.SetInt(balance, s.GetInt(balance)+1) }).Add()
r.Event("send_email").Writes(notified). // reads and writes only notified
    Apply(func(s State) State { return s.SetBool(notified, true) }).Add()
// Different components, and neither reads the other's variable: the per-component
// check can skip this pair. Build's global check checks it anyway (cheap from the tables).
```

#### In the Report

```
CC (Compensation Commutativity): PASS (10 pairs: 7 disjoint, 3 brute-force)
```

This means:
- 10 event pairs checked
- 7 needed no check because the events lie in different footprint components (the per-component check only; `Build`'s global check always reports 0 disjoint)
- 3 checked exhaustively, state by state

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

**Proof**: See Section 5 of the paper, and the axiom-free mechanization in
[normalization-confluence](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq).
Two refinements are mechanized there too. The termination measure can be any well-founded
potential, not just a step count, so the theorem itself does not need finite domains
([`GovernanceWF.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceWF.v)). And the conditions are exact: on the states a run
can reach, CC holds **iff** every ordering converges
([`GovernanceConverse.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceConverse.v), `cc_exact_from`). gsm checks CC on
every valid state, which is sufficient; a reported failure is a state from which two orders
diverge, which a given deployment may never reach. The formal treatment is [Theory §7](theory.md#7-the-convergence-theorem).

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

To fix: repair the other way, so the rule reads "write implies read": when `write` is granted
without `read`, the repair grants `read` instead of revoking `write`. Both orders then end at
`{read=true, write=true}`. (Guarding `grant_write` on `read=true` does not fix it: the guard reads
a variable another event writes, so `grant_write` first is a no-op and the orders still differ, as
in the guarded `ship` above.)

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

## Why you do not have to take the library's word for it

Anyone can write a library and claim it converges. The claim is worthless without a reason to believe it. Three things back this one up.

**It is verified at build time, not hoped for at runtime.** `Build` does not just wire up your callbacks. It enumerates the state space and checks that the two convergence properties actually hold for *your* rules. If they do not, it refuses to build and hands you a counterexample. Convergence is a compile-time property of your model, not a runtime prayer. (For models too large to enumerate whole, it verifies each independent component separately, so the cost scales with your biggest component, not the product of everything.)

**The convergence theorem is machine-checked, with no axioms.** The underlying math is proved in Coq/Rocq, and the proof is *axiom-free*: `Print Assumptions` reports "Closed under the global context," which is the proof assistant's way of saying nothing was assumed, everything was derived. CI gates on it. This is not "we wrote it on a whiteboard and it looked right." A machine checked every step, and rejects the build if a step goes missing.

**The library is checked against the proof, not just against itself.** A checker extracted directly from the Coq proof re-certifies the tables that the Go library produces, in process, before `Build` returns a machine, and a second extracted checker recomputes the result from the rules themselves when they are written in gsm's combinator vocabulary. So a bug in the Go convergence check cannot smuggle a non-convergent machine past you: the independent, proof-derived oracle would reject it. The trust does not rest on the Go code being correct. It rests on the math, and the Go code is held to the math.

That chain (build-time check, backed by an axiom-free proof, enforced by an extracted oracle) is what provenance of trust means here. Most "verified" claims stop at the first link. This one runs all the way down. What each oracle checks, and what it trusts, is in [Verification](verification.md#what-the-extracted-oracles-check).

The assumptions that sit outside the proof are named, not buried. The guarantee is about orderings of the same events, each delivered once, so the build report lists every event that is not idempotent (deliver it twice and the result changes) and needs deduplication; the proof shows that for those events no amount of ordering cleverness absorbs a duplicate. If you declare only some event pairs independent, the report lists each undeclared pair that does not commute, because those must arrive in causal order. Both are in [Deployment](deployment.md).

---

## Where CRDTs Fit

CRDTs solve convergence by requiring operations to commute. But when your operations can violate business invariants (shipping an unpaid order, overdrawing an account) commutativity alone isn't enough. `gsm` provides convergence through **compensation**: declare what valid means and how to repair violations, and the library proves that all event orderings converge to the same valid state.

CRDTs: `op1; op2 = op2; op1` (operations commute)
gsm: `NF(op1; op2) = NF(op2; op1)` (normal forms converge)

CRDTs are not a competitor to this. They are the **degenerate corner** of it: the special case where your operations already commute, so the required repair is empty. Everything a CRDT does, this framework does with a no-op compensation. Drop that design restriction and you still have convergence (normalization confluence), and the inclusion is **strict**: governed machines exist that no CRDT can express. Under causal delivery the correspondence is exact: a compensation-free governed system converges under causal delivery **iff** it is an op-based CRDT ([`CausalReplay.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CausalReplay.v), `compensation_free_exact`), so op-based CRDTs are precisely the compensation-free fragment. This is machine-checked and axiom-free in [`CRDT.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CRDT.v) (full statement in [SUBSUMPTION.md](https://github.com/blackwell-systems/normalization-confluence/blob/main/SUBSUMPTION.md)). What you get by leaving that corner is the ability to handle operations that violate invariants (the entire world of real business rules) while keeping the coordination-free convergence that made CRDTs attractive in the first place.

`gsm` can even tell you *which* corner you are in: it will certify whether your machine falls in the commute-for-free fragment (use a CRDT, it is simpler) or genuinely needs compensation ([Verification](verification.md#the-rules-oracle-and-its-fragment)).

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
| **Synthesis** | `Registry.Synthesize` | Generate a CC-satisfying compensation from invariants + events (the inverse of verifying one) |
| **Repair target** | `Synthesis.Repairs` | Where each invalid state is repaired to, in a synthesized compensation |
| **Impossibility witness** | `Synthesis.Witness` | A critical pair no compensation can reconcile (why synthesis returned IMPOSSIBLE) |
| **Combinator rule** | `DeclInvariant` / `DeclEvent` | An invariant or event built from a fixed vocabulary as inspectable data, instead of an opaque closure |
| **Sugar layer** | `AtMost`, `Inc`, `r.Rule(...)`, `r.On(...)` | Read-as-English surface that lowers to the primitive combinators |
| **Rules serialization** | `Registry.WriteMachineAST` | Emit the combinator rules as S-expressions for the extracted rules oracle |
| **Compositional build** | `Registry.BuildCompositional` | Verify each footprint component independently, so cost scales with the largest component, not the whole machine |
| **Oracle** | Extracted Coq checker (`checker` / `astchecker`) | Independently re-certifies gsm's convergence result, from tables or from rules |

> **Synthesis vs. verification.** `Build` *checks* the compensation you supplied (WFC + CC).
> `Synthesize` does the inverse: given only the invariants (validity) and events, it *searches*
> for a normal-form map on invalid states that satisfies CC, returning a convergent
> compensation (least-invasive by default; steer it with `Prefer`, or get the provably minimal
> one with `Optimal`), or proving that none converges. Convergent is not the same as desirable:
> it only makes orderings agree, so inspect or steer the repair. When it proves none converges,
> the witness is the **ceiling** of the compensation regime (the dual of the CRDT floor): no repair
> reconciles the orderings, so only coordination can, and gsm has shown compensation is not enough
> here. See [Verification](verification.md#compensation-synthesis).

The federation terms (morphism, resolver, C1, C2, XU, holonomy) are in the
[federation glossary](federation.md#glossary-federation-terms--code).

---

## Common Misconceptions

### "Redelivered events are harmless"

**Only for some events.** The guarantee is about orderings of one multiset of events, each
delivered once. A duplicate is absorbed only when the event is idempotent and commutes with what
arrived between the copies, and `Build` lists the events that need deduplication. A counter
increment is the typical case; setting a flag is not. See [Deployment](deployment.md#delivery).

---

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

<!-- gocheck: check machine -->
```go
machine.Apply(s, "ship")  // O(1) table lookup: step[ship][s]
```

No repair functions execute at runtime. Everything is baked into lookup tables during `Build()`.
(The exception is a lazy machine checked per footprint component, which has no global tables and runs the
rules at `Apply` time; see [Compositional Verification](verification.md#compositional-verification).)

---

### "All event pairs must be independent"

**False**. By default, `gsm` checks all pairs. But you can declare only the pairs that can be
reordered, and every other pair must then be delivered in causal order. See
[Independence declarations](getting-started.md#independence-declarations).

---

### "Finite state spaces are a limitation"

**Narrower than it sounds**. The constraint is finite *per-variable domains* (no arbitrary strings or lists), which is what enables **exhaustive verification**. It is not a cap on the whole state space: `Build` (with combinator rules) and `BuildCompositional` verify each footprint component independently, so a machine whose global state space is astronomically large still certifies when it decomposes into small components. You trade unbounded domains for a mathematical proof of convergence you cannot get with infinite spaces.

The finiteness is a property of gsm's verification, not of the theory: the convergence theorem
holds on infinite domains under any well-founded repair potential (mechanized in
[`GovernanceWF.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceWF.v)). What an infinite domain costs is the exhaustive check.

For business logic state machines (order workflows, authorization states, inventory counts), finite domains are natural.

---

### "This is the same as CRDTs"

**Backwards, if anything**: CRDTs are a *special case* of what gsm does. See [Where CRDTs Fit](#where-crdts-fit).

---

### "The sugar layer hides behavior"

**False**. The ergonomic helpers (`AtMost`, `Inc`, `Rule`/`On`, ...) are pure syntax: each one
lowers to the same primitive combinators, and a test pins that a machine written with the sugar
serializes to *byte-identical* rules as the same machine written with the primitives. The
analyzable core, and the extracted checker that re-certifies it, see exactly what you wrote, with
nothing added or elided by the friendly surface. See [Rule expression layers](getting-started.md#rule-expression-layers).

---

## Further Reading

### Academic Background

- **[Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)** (Blackwell, 2026)
  The paper this library implements

- **[The mechanized proof](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq)**
  Axiom-free, CI-gated

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

- [Federation](federation.md): The multi-registry sequel: when a whole network of connected registries converges
- [ARCHITECTURE.md](design/ARCHITECTURE.md): How `gsm` implements the theory
- [Getting started](getting-started.md) and [Reference](reference.md): API usage and reference
- [nccheck](https://github.com/blackwell-systems/nccheck): YAML-based verifier (reference implementation from paper)
