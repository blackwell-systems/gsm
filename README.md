# gsm - Governed State Machines

[![Blackwell Systems™](https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg)](https://github.com/blackwell-systems)
[![Go Reference](https://pkg.go.dev/badge/github.com/blackwell-systems/gsm.svg)](https://pkg.go.dev/github.com/blackwell-systems/gsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/blackwell-systems/gsm)](https://goreportcard.com/report/github.com/blackwell-systems/gsm)
[![CI](https://github.com/blackwell-systems/gsm/actions/workflows/test.yml/badge.svg)](https://github.com/blackwell-systems/gsm/actions/workflows/test.yml)
[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.18677400.svg)](https://doi.org/10.5281/zenodo.18677400)
[![proof: machine-checked](https://github.com/blackwell-systems/normalization-confluence/actions/workflows/verify.yml/badge.svg)](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq)

**Send in any order. Converge on the rules.**

What if distributed systems don't have to coordinate - because they agree on the rules ahead of time, so ordering and compensations are deterministic? The underlying theory - normalization confluence - proves that compensation is sufficient for convergence when two algebraic properties hold, without the expressiveness limits of CRDTs or the latency cost of consensus.

`gsm` is a Go library for constructing state machines where events may arrive out of order and violate business rules, but automatic compensation ensures all replicas converge to the same valid state. Convergence is **verified at build time**: `Build` enumerates the state space (O(1) table-lookup runtime), and for machines too large to enumerate, `BuildCompositional` verifies each **footprint component** independently, so certification cost scales with the largest component rather than the whole machine.

The convergence theorem itself is **machine-checked**: an axiom-free Coq/Rocq proof (`Print Assumptions` reports "Closed under the global context") with CI that gates on it. See the [mechanized proof](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq) and the [regime field guide](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIMES.md) for when a governed network converges.

And gsm's own verification can be **differentially checked** against that proof: `Machine.WriteConvergenceTables` emits a built machine's tables, and a checker extracted from the Coq development re-certifies, independently of this Go code, that they converge. A bug in gsm's Go verification cannot make a non-convergent machine pass the extracted oracle, but the oracle protects only the machines it is run on. Today that means machines someone exports and checks by hand, and the oracle tests in gsm's own test suite when they are pointed at built checker binaries. `Build` does not run the oracle, so a user's machine is protected by gsm's Go verifier alone; a runtime gate that runs an extracted checker on every success path is planned. See [`coq/extraction`](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq/extraction) and [What the extracted oracles check](#what-the-extracted-oracles-check).

CRDTs solve convergence by requiring operations to commute. But when your operations can violate business invariants - shipping an unpaid order, overdrawing an account - commutativity alone isn't enough. `gsm` provides convergence through **compensation**: declare what valid means and how to repair violations, and the library proves that all event orderings converge to the same valid state.

Registries also **federate**: connect independently-governed machines with directed morphisms that encode cross-organizational constraints - a manufacturer's status constrains a supplier's listing, a regulator's rules constrain a bank - and `gsm` proves the *whole network* converges. Same build-time guarantee, now across organizational boundaries. See [Federated Registries](#federated-registries).

## Example: Order Fulfillment

Payment and shipment requests arrive at different replicas in different orders. Every replica must end in the same state, and no order may ship unpaid:

<!-- gocheck: run -->
```go
r := gsm.NewRegistry("order_fulfillment")

// Facts: each event records one fact and reads nothing else.
paid := r.Bool("paid")
shipRequested := r.Bool("ship_requested")
cancelled := r.Bool("cancelled")

// Outcome: derived from the facts by compensation, never written by an event.
status := r.Enum("status", "open", "shipped", "cancelled")

// The business rule: what the status must be, given the facts.
outcome := func(s gsm.State) string {
    switch {
    case s.GetBool(cancelled):
        return "cancelled" // cancellation wins, in every order
    case s.GetBool(paid) && s.GetBool(shipRequested):
        return "shipped" // ship only once paid
    default:
        return "open"
    }
}

r.Invariant("status_matches_facts").
    Watches(paid, shipRequested, cancelled, status).
    Holds(func(s gsm.State) bool { return s.Get(status) == outcome(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(status, outcome(s)) }).
    Add()

r.Event("process_payment").Writes(paid).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(paid, true) }).Add()
r.Event("request_shipment").Writes(shipRequested).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(shipRequested, true) }).Add()
r.Event("cancel_order").Writes(cancelled).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(cancelled, true) }).Add()

machine, report, err := r.Build() // verifies that every ordering converges
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}

// Replica A sees the shipment request first; the order stays open until payment.
a := machine.NewState()
a = machine.Apply(a, "request_shipment") // status=open
a = machine.Apply(a, "process_payment")  // status=shipped: compensation derives it

// Replica B sees the same events in the other order.
b := machine.NewState()
b = machine.Apply(b, "process_payment")
b = machine.Apply(b, "request_shipment")

fmt.Println(a.Get(status), b.Get(status), a.ID() == b.ID()) // shipped shipped true
```

The shape to copy: **events record facts; invariants derive outcomes.** Each event writes one variable and reads nothing else, so nothing an event does depends on what arrived before it. The order's status is never set by an event: the invariant recomputes it from the facts, and compensation applies it after every event.

**Why not guard the shipment on payment?** The natural first draft ships only when the order is already paid:

<!-- gocheck: check registry -->
```go
r.Event("ship").Writes(status).
    Guard(func(s gsm.State) bool { return s.GetBool(paid) }). // reads another event's write
    Apply(func(s gsm.State) gsm.State { return s.Set(status, "shipped") }).
    Add()
```

A replica that sees `ship` before `pay` drops the shipment (the guard is false), and one that sees `pay` first ships, so the two never agree. `Build` rejects that machine with the two orderings as a counterexample. A guard that reads a variable another event writes is the most common way to lose convergence; record the request as a fact instead and let an invariant decide.

> **New to convergent systems?** See [CONCEPTS.md](CONCEPTS.md) for foundational definitions, theory explanations, and a glossary mapping paper terms to code.
>
> **Want rigorous mathematical foundations?** See [THEORY.md](THEORY.md) for formal definitions, proofs, and connections to rewriting theory.
>
> **Research paper:** [*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18677400) (Blackwell, 2026)

## When to Use gsm

**Use gsm when:**
- Building event-sourced systems with out-of-order events
- Operations can violate invariants (need compensation/repair)
- Each variable's domain is finite (the *global* state space may be astronomically large: `BuildCompositional` scales with the largest footprint component, not the product of all domains)
- You want mathematical convergence guarantees
- Multiple registries share constraints across organizational boundaries (federated registry networks)
- You want gsm to **synthesize** the compensation from your invariants + events (or prove none converges)
- You're using Go

**Don't use gsm when:**
- Operations already commute: a CRDT is simpler (and is provably the compensation-free special case of what gsm does; gsm can certify whether your machine falls in that fragment)
- Operations preserve invariants in all orderings (use invariant confluence)
- A variable needs a truly unbounded domain (arbitrary strings, lists), or the machine is one large tightly-coupled footprint that neither `Build` nor `BuildCompositional` can enumerate
- Real-time latency requirements conflict with build-time verification cost

## How It Works

### Build Time (Verification)

When you call `registry.Build()`:

1. **Enumerate state space** - All combinations of variable values (finite per variable; `BuildCompositional` enumerates per footprint component instead of the global product)
2. **Compute normal forms** - For every state, apply compensation until valid
3. **Verify WFC** - Compensation terminates and reaches valid states
4. **Build step table** - For every (event, state) pair, precompute the normal form after applying the event
5. **Verify CC** - For every independent event pair and every valid state, both orderings reach the same normal form (two step-table lookups per state; no pair is skipped)

If verification passes, you get an immutable `Machine` with precomputed lookup tables.

If verification fails, you get a detailed report showing:
- Which events violate CC
- At which state the violation occurs
- The divergent traces (order 1 vs order 2)

### Runtime (O(1) Execution)

<!-- gocheck: check machine -->
```go
machine.Apply(state, "request_shipment")
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

<!-- gocheck: check registry -->
```go
// Calling Independent() automatically switches to declared-only mode
r.Independent("deposit", "send_notification")
r.Independent("withdraw", "send_notification")
```

**Independent events** can arrive in either order (they're not causally related). Only declared pairs will be checked for commutativity.

`Build` checks every declared pair exactly, whatever the pair's footprints. Two events that write different variables can still fail to commute: a guard or effect may read a variable the other event writes (see [Why not guard the shipment on payment?](#example-order-fulfillment)). Only `BuildCompositional` skips pairs by footprint, and only after it has checked what each event reads (see [Compositional Verification](#compositional-verification)).

### Declarative rules (combinators)

Rules can also be written from a fixed, gsm-owned vocabulary instead of Go closures. It still reads as Go, but produces an expression tree gsm can both evaluate and analyze:

<!-- gocheck: check registry -->
```go
a := r.Int("a", 0, 5)
r.DeclInvariant("a_cap", Le(V(a), Lit(3)), Do(Set(a, Lit(3)))) // holds when a<=3; repair sets a=3
r.DeclEvent("inc_a", Do(Set(a, Add(V(a), Lit(1)))))            // a := a + 1
```

The footprint is **derived** from the tree: an invariant's footprint is every variable its predicate and repair mention, and an event's write set is the variables it assigns. Nothing is declared by hand, so nothing can be mis-declared, and `BuildCompositional` checks an event's reads (its guard and the expressions it assigns) against its write set exactly, from the tree. Because the rules are data (not opaque closures), they are inspectable and serializable, the precondition for a verified verifier and portable policies. The closure API (`Holds`/`Repair`/`Apply`) is unchanged; use whichever fits.

These combinators are the **analyzable core**: primitives we serialize (`Registry.WriteMachineAST`) and hand to the machine-checked oracle. On top of them sits an ergonomic layer that lowers to the exact same AST, so it adds nothing the verifier must learn:

<!-- gocheck: check registry -->
```go
r.Rule("a_cap").Require(AtMost(a, 3)).RepairWith(SetTo(a, 3)).Add()
r.On("inc_a").Does(Inc(a)).Add()
```

`AtMost`/`Inc`/`SetTo` and the `Rule`/`On` builders desugar to `Le(V(a),Lit(3))` / `Do(Set(a,Add(V(a),Lit(1))))` etc.; a test pins that the friendly and primitive spellings serialize to byte-identical rules. Write for humans at the top; test, serialize, and prove at the primitive bottom.

Either spelling can be cross-checked against the verified **rules oracle**: `WriteMachineAST` emits the machine as S-expressions and the OCaml `astchecker` (extracted from the axiom-free Coq proof in `normalization-confluence`) recomputes convergence straight from those rules. It also certifies the **CRDT-fragment classification** (a machine-checked `compensation_free` verdict: whether repair is ever needed), so a consumer can confirm from the rules whether a machine is a plain CRDT or a compensation-bearing governed machine. See `astoracle_test.go` (`GSM_AST_CHECKER`).

**Serializable fragment.** Only rules built from the combinator vocabulary can be exported to the oracle: variables over `min .. min+domain-1` (any `min`, negative included); the comparison predicates `Le`/`Lt`/`Eq`/`Ge`/`Gt`/`Ne` and boolean `And`/`Or`/`Not`; the transforms `Set`/`Add`/`Sub`; and events with an optional guard. Closure-based invariants and events cannot be serialized, so `WriteMachineAST` returns an error rather than emit something the checker would misread. gsm's own `Build` verification has no such restriction; the fragment is only the boundary of what the extracted oracle can independently re-certify.

**Policy as a portable artifact.** `Registry.PolicyBytes` returns those serialized bytes and `Registry.PolicyDigest` a stable, domain-separated SHA-256 over them, so a policy can be named and anchored independently of who built it (a rule written with the sugar surface digests identically to the equivalent primitive combinators). The digested bytes are exactly the oracle's input, so the anchored digest and the re-checked artifact cannot diverge. This is the interchange contract an external audit layer pins to: it commits the digest in a log and hands the same bytes to `astchecker`. `State.Digest` is the companion primitive for the resulting state: a stable, domain-separated hash over a state's packed value (meaningful because the policy pins the layout), so a layer that anchors a policy can also attest which state an action produced and reproduce it by replaying the same events over a reference build.

## API Overview

### Using Machines

<!-- gocheck: check machine -->
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

<!-- gocheck: check machine -->
```go
// Enum variables
status := s.Get(statusVar)           // returns string

// Bool variables
enabled := s.GetBool(enabledVar)     // returns bool

// Int variables
count := s.GetInt(countVar)          // returns int (adjusted for min offset)
```

### Writing State

<!-- gocheck: check machine -->
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

<!-- gocheck: check federation -->
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

**Multi-source targets.** When a target has *two* sources, the authority argument doesn't pick a winner — so the target declares a `Resolver` that deterministically merges its sources (priority, AND/OR, most-restrictive, etc.), relaxing the tree requirement to any acyclic DAG:

<!-- gocheck: check federation -->
```go
fed.Morphism(hr, door).Shared(access).Map(hrToDoor).Add().
    Morphism(security, door).Shared(access).Map(securityToDoor).Add().
    Resolve(door, func(dst gsm.State, src map[string]gsm.State) gsm.State {
        if src["hr"].GetBool(employed) && src["security"].GetBool(cleared) {
            return dst.Set(access, "granted") // AND: both sources must agree
        }
        return dst.Set(access, "denied")
    })
```

Multi-source convergence is the paper's **Federated Convergence with Resolution** theorem (§8), which holds whenever the resolver is a function of its sources alone (**R1**) and preserves target validity (**R2**, the multi-source generalization of M1). gsm certifies exactly those hypotheses: it **exhaustively verifies at build time** that, for *this* federation, the resolver writes only shared variables, satisfies R1, and satisfies R2 — the same verify-the-preconditions contract it applies to single-registry WFC/CC. A resolver that could diverge (violates R2), reads local state (violates R1), or writes non-shared variables is rejected; a multi-source target *without* a resolver is rejected.

> Single-source authority is the special case of a resolver with one source. Both are backed by the paper's proofs (Federated Convergence, and its multi-source generalization); gsm's build-time checks establish the theorems' preconditions.

**Monotone cycles.** Acyclicity is only needed to tame *non-monotone* repair (the divergence counterexample is negation, which is antitone). With `Federation.AllowMonotoneCycles()`, cyclic networks are allowed when every morphism/resolver is **monotone** (verified by enumeration); `Build` then computes the normal form by Kleene iteration to the least fixed point, which converges order-independently even on arbitrary cyclic graphs (the paper's *Monotone Convergence Despite Cycles*, via Knaster–Tarski + chaotic iteration). State-based CRDTs are the compensation-free special case of this monotone regime; that CRDTs (op- and state-based) are a *strict* sub-fragment of normalization confluence is machine-checked in [`CRDT.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CRDT.v) (see [SUBSUMPTION.md](https://github.com/blackwell-systems/normalization-confluence/blob/main/SUBSUMPTION.md)). Non-monotone cycles are still rejected. When a cycle is rejected, `Build`'s error names the offending loop, and `Federation.DiagnoseCycle` iterates the loop's repair to report whether it settles or oscillates (the loop-composite fixed-point witness), so you can see exactly which constraint cycle cannot converge. To *accept* such a network rather than reject it, `Federation.CoordinationPlan` returns a set of morphism edges (shared variables) to place under an external single writer or consensus, and `Federation.BuildCoordinated(plan)` builds the federation given that coordination: the coordinated edges become external inputs and the acyclic residual converges coordination-free. This is a localized mixed-consistency partition (consensus only on the obstructing edges); the plan is a correct feedback edge set of size at most the number of independent cycles, not necessarily the minimum, which is the group feedback edge set problem (NP-hard in general).

**Compositional construction.** A verified sub-federation embeds into a larger one with `Federation.Embed`: define and verify a subsystem on its own, then reuse it as a unit and connect it with more morphisms. The composed federation runs as the flat convergent machine (a `FedState` holds one `State` per component, so no product state space is materialized). This realizes the paper's compositional-collapse result — a convergent sub-federation collapses to an effective registry — enabling modular, hierarchical verification and black-box reuse of subsystems.

<!-- gocheck: check federation -->
```go
sub := gsm.NewFederation("pricing").Morphism(pricing, catalog)./* ... */Add()
sub.Build() // verify the subsystem on its own

m, _, _ := gsm.NewFederation("storefront").
    Embed(sub).                       // reuse the verified subsystem as a unit
    Morphism(catalog, order)./* ... */Add().
    Build()
```

**Certificate-based reuse.** A verified sub-federation can be packaged as a `Certificate` and reused without re-verifying its internals. `sub.Certify()` builds and verifies the subsystem and returns a certificate carrying the verdict, each morphism and resolver in extensional table form (reified from the finite, source-determined maps), and a tamper-complete digest over both the component policies and those tables. `EmbedCertified(sub, cert)` embeds it on that certificate: `Build` re-checks the internal morphisms from the certificate's tables instead of re-verifying their closures, checks the seam (the boundary morphisms) and the whole-graph acyclicity, and rebuilds each certified component with `Build`, which re-checks its convergence. A consumer that receives a certificate re-checks it independently with `cert.Verify(components)`, which re-derives validity preservation (M1/R2) from the tables rather than the producer's morphism closures and re-checks every component's convergence, so a composition is confirmed without trusting the producer's code or its recorded verdict. An outer morphism may read a certified subsystem, or write one of its declared **input ports** (shared variables the sub leaves free): declare them at `Certify(gsm.Port{Registry: r, Var: v})`, and `Build` verifies each inbound boundary morphism at the seam (M1/R2). A write to any other (sealed) variable is rejected. See [CERTIFICATE-DESIGN.md](CERTIFICATE-DESIGN.md).

<!-- gocheck: check federation -->
```go
cert, _ := sub.Certify() // verify once; package the verdict + morphism tables + digest

m, _, _ := gsm.NewFederation("storefront").
    EmbedCertified(sub, cert). // reuse without re-verifying internals; only the seam is checked
    Morphism(catalog, order)./* ... */Add().
    Build()

// A consumer re-checks the certificate independently, from the tables (not the producer's closures):
err := cert.Verify(map[string]*gsm.Registry{"pricing": pricing, "catalog": catalog})
```

## Compositional Verification

`Build` enumerates the whole state space, which caps it at the 2²⁰ (≈1M) state ceiling.
`BuildCompositional` lifts that cap for machines that are *wide but loosely coupled*: many
variables, but each invariant and event touches only a few of them.

**When to use it:**
- Your global state space exceeds the ~1M enumeration ceiling, but
- The machine decomposes into independent groups of variables (invariants and events with
  disjoint footprints), each group small on its own.

<!-- gocheck: check registry -->
```go
m, rep, err := r.BuildCompositional()
// rep.Components          -> number of independent footprint components
// rep.MaxComponentStates  -> size of the largest component's subspace (the real cost)
// rep.FootprintChecked    -> footprint conformance held
```

**How it works.** gsm partitions the variables into footprint-connected components (union-find
over invariant footprints and event write sets), checks that every rule reads and writes only its
declared footprint, verifies WFC and CC over each component's own subspace, and skips
cross-component event pairs, which commute because they read and write disjoint variables.
Certification cost is exponential in the *largest component*, not the whole machine, so a registry
of many independent small invariants certifies even when its global state space is astronomically
large.

**What the footprint check proves.** An event's guard and effect may read only the variables the
event writes; an invariant's check and repair only its `Watches` set. A rule that reads anything
else (like the guarded `ship` above) is rejected with a footprint violation naming the variable.
For combinator rules the check is syntactic and exact. A closure is opaque, so gsm tests it: from
every state of its component, it changes each outside variable, and each pair of outside
variables, to every other value and confirms the closure's result does not change. That catches
dependence on one or two outside variables (such as `paid && inStock`), but not a closure that
depends only on three or more outside variables jointly. If your rules are closures with
wide guards, use `Build` (exact, no footprint assumption) or the combinator vocabulary.

**Trade-offs.** The returned `Machine` is *lazy*: it computes `Apply`/`Normalize` at runtime from
the rules instead of via a precomputed table lookup, and `Export` is unavailable (there are no
global tables to serialize). Preconditions: every invariant declares its footprint and every
event its write set (both automatic with the combinator vocabulary), every event reads only what
it writes, the zero state is valid, and the machine fits in 64 bits of state.

## Verification Report

The `Report` returned by `Build()` shows:

```
Machine: order_fulfillment
  Variables: 4
  States: 24
  Events: 3

  WFC: PASS (max repair depth: 1)
  CC (Compensation Commutativity): PASS (3 pairs: 0 disjoint, 3 brute-force)

  Convergence: GUARANTEED
```

**WFC (Well-Founded Compensation)**: Compensation terminates from every state. The report shows the maximum number of repair steps needed.

**CC (Compensation Commutativity)**: For every independent pair of events, both orderings reach the same normal form from every valid state (and from the zero state `NewState` returns). The report shows:
- **Disjoint pairs** - Skipped because the two events lie in different footprint components, after the footprint check (`BuildCompositional` only; always 0 for `Build`)
- **Brute-force pairs** - Checked exhaustively, state by state

If verification fails, you get a counterexample. For the guarded-shipment draft above, reduced to two flags:

```
CC (Compensation Commutativity): FAIL
  Events: (pay, ship)
  State:  {paid=false, shipped=false}
  pay→ship: {paid=true, shipped=true}
  ship→pay: {paid=true, shipped=false}
```

This shows the exact state and event pair where CC fails, plus the divergent traces.

A `BuildCompositional` report adds a footprint line. When it rejects a rule for reading outside its footprint, the report names the violation as the cause and shows WFC and CC as not evaluated:

```
Machine: pay_ship
  Variables: 2
  Components: 2
  Events: 2

  Footprint conformance: FAIL
    gsm: event "ship" reads variable "paid" outside its declared footprint
  WFC: not evaluated (footprint violation)
  CC (Compensation Commutativity): not evaluated (footprint violation)
```

### What the extracted oracles check

Two checkers extracted from the axiom-free Coq proof can re-certify a machine independently of
gsm's Go code. Neither runs as part of `Build` or any other gsm entry point: each runs only when
someone invokes it on an exported machine. gsm's CI builds both from a pinned, hash-checked
proof commit (`.github/oracle/`) and cross-checks every machine the test suite passes to `Build`
(the documented examples, the property-tested machines, and 600 random combinator machines)
against them; there the oracle tests are required, not skipped. Locally they run when
`GSM_CONVERGENCE_CHECKER` / `GSM_AST_CHECKER` point at built binaries. A runtime gate that runs
an extracted checker on every success path is planned (it needs the checker specs aligned with
`Build`'s declared pairs and valid-state domain, and a way to run extracted code in-process).

- **Table oracle** (`checker`, input from `Machine.WriteConvergenceTables`): checks that every
  ordered pair of events commutes on every state in the emitted tables, by enumeration, with no
  footprint shortcut. It trusts that the tables are what the rules compute (gsm produces them from
  your closures), checks all pairs whether or not you declared them `Independent`, and checks
  every encodable state, including invalid ones a run never reaches. So it can reject a machine
  `Build` accepts (a stricter check), but it never accepts tables whose events fail to commute.
  Only `Build` machines have tables.
- **Rules oracle** (`astchecker`, input from `Registry.WriteMachineAST`): recomputes every event
  step from the combinator rules and checks that every ordered pair commutes on every valid
  state, with no footprint shortcut. Closure rules cannot be exported. Its arithmetic is gsm's
  (signed integers, signed comparisons, clamped writes). It refuses to certify two things it does
  not model: an expression that could exceed 2^31-1 in magnitude (Go's `int` wraps there on
  32-bit platforms), and a write that could store a negative value into a two-valued variable
  with minimum 0 (a `Bool` stores `value != 0`; the format does not say which variables are
  `Bool`s, so the rule covers every such variable).

Both reject the guarded-shipment machine above. Neither shares the footprint assumption that
`Build`'s former shortcut relied on.

**What gsm recomputes itself.** Independently of the oracles, gsm does not trust a stored
verdict. `Certificate.Verify` recomputes the certificate digest from the consumer's registries,
re-derives validity preservation (M1/R2), input-port freeness, and acyclicity from the
certificate's morphism tables, and rebuilds every component with `Build` (WFC and CC). When
`Build` runs on a federation containing `EmbedCertified` subsystems, it recomputes the digest,
re-derives M1/R2, port freeness, and acyclicity from the tables for the internal morphisms, and
rebuilds every certified component with `Build`; it skips only re-verifying the internal
morphisms from their closures. The certificate's recorded `Report` is never used for a decision.
All of this is gsm's Go code: it removes trust in stored results, not in the Go verifier.

## Compensation Synthesis

You don't have to *design* the compensation. Declare the invariants (what "valid" means) and
the events, **omit `Repair`**, and gsm will **generate** a convergent compensation — or prove
none exists.

<!-- gocheck: check -->
```go
r := gsm.NewRegistry("order")
// ... variables, invariants (Holds only — no Repair), events ...

syn, _ := r.Synthesize()
if !syn.Convergent {
    fmt.Println(syn)   // if impossible, includes a witness: the critical pair no repair fixes
    return
}
m := syn.Machine()      // a ready-to-use, verified-convergent Machine
```

Synthesis reframes convergence as a search for a **normal-form map** on invalid states that
satisfies CC. It uses backtracking with forward-checking (pure Go, no solver dependency), so it
scales well past naive enumeration, and it is precise about the boundary:

- **Convergent** — a repair was found; `Machine()` is ready, `Repairs()` shows it.
- **Impossible** (`Exhaustive`, not convergent) — no compensation converges. `Witness()` gives
  a concrete reason (e.g. two events reaching *distinct already-valid states* — no repair can
  reconcile them), so you know to redesign the **events**.
- **Undetermined** (search budget hit) — none found within budget; one may exist. (A SAT/SMT
  backend would settle these.)

Impossibility is the **ceiling** of the compensation regime, the dual of the CRDT floor. A
`compensation_free` machine sits at the bottom (it is a CRDT: repair is never needed); an
`Impossible` witness marks the top, where no repair converges and only coordination (consensus,
locking) can. gsm certifies both edges of what compensation reaches: the floor by embedding (every
CRDT is a gsm machine), the ceiling by counterexample (the witnessed critical pair).

**Convergent ≠ desirable.** A synthesized repair only makes orderings agree. By default
synthesis returns the **least-invasive** repair (fewest variables changed). Steer it with a
policy, or get the provably minimum-cost repair:

<!-- gocheck: check registry -->
```go
// bias toward a policy (ordering):
syn, _ := r.SynthesizeWith(gsm.Prefer(func(from, to gsm.State) int {
    if to.Get(status) == "cancelled" { return 5 } // avoid cancelling
    return 1
}))

// or the provably minimum-cost convergent repair (branch-and-bound):
syn, _ := r.SynthesizeWith(gsm.Prefer(cost), gsm.Optimal())
```

Synthesis is the inverse of `Build`: `Build` **verifies** a compensation you wrote;
`Synthesize` **generates** one (or proves impossibility). It's a natural fit for tooling or
LLM-authored policy — describe the rules, get a convergent machine with a proof.

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

Hard limit: 2²⁰ ≈ 1M states. `Build` returns an error above this rather than attempt an intractable enumeration; use `BuildCompositional` (or federation) to go beyond it.

### Runtime

Event application: **O(1)** - single array lookup, no computation.

Memory: One `uint64` per state for normal form table, plus one `uint64` per (event, state) pair for step table. For 1M states × 10 events = ~80MB.

## Relationship to the Paper

This library implements both the **single-registry governance model** (Section 3) and the **federated convergence model** (Section 8) of the paper:

- **Registry** = the machine definition (variables, invariants, compensation, events)
- **WFC** (§3) = well-founded measure on compensation depth
- **CC** (§3) = compensation commutativity (CC1 + CC2)
- **Convergence theorem** (§5) = WFC + CC ⟹ unique normal forms (via Newman's Lemma)
- **Verification calculus** (§10) = footprint components + decomposable repair (`BuildCompositional`, in `compositional.go` and `footprint.go`)
- **Federation** (§8) = registry morphisms + the authority argument + the constructive normalizer ρ_Fed (`federation.go`: `Federation` / `FedMachine`)
- **Multi-source resolution** (§8) = resolution operators (`Resolve`)
- **Monotone cycles** (§8) = convergence on cyclic networks under monotone repair (`AllowMonotoneCycles`)
- **Compositionality** (§8) = sub-federations collapse to effective registries (`Embed`)

The paper proves: **if WFC and CC hold, all processors consuming the same events converge to the same valid state regardless of application order** - that this extends to a tree-shaped network of registries connected by validity-preserving morphisms, and further to any acyclic network whose multi-source targets carry a source-determined, validity-preserving resolution operator; that even *cyclic* networks converge when repair is monotone; and that verified sub-federations compose.

This library verifies: **does your machine satisfy WFC and CC?**

## Limitations

- **Finite variable domains** - Each variable's domain must be finite (no arbitrary strings or lists). This is a per-variable constraint, not a global ceiling: `BuildCompositional` certifies machines whose product state space is astronomically large, as long as each footprint component is small
- **Build-time cost** - Global `Build` enumerates the state space and hard-errors above 2²⁰ (~1M) states (a fixed cap, not configurable), so it does not degrade gracefully past that wall; use `BuildCompositional` for machines that decompose into small footprint components (see [Compositional Verification](#compositional-verification)), where cost scales with the largest component rather than the whole machine
- **Verification requires Go** - Runtime portable via JSON export, but verification engine is Go-only
- **Federation** - Tree networks, multi-source acyclic DAGs (resolution operators), and monotone *cyclic* networks are all covered by the paper's proofs (Section 8). gsm establishes the theorems' preconditions (morphism M1, resolver R1/R2, monotonicity) by exhaustive build-time verification. Only *non-monotone* cycles and multi-source targets without a resolver are rejected at build
- **No runtime monitoring** - Once built, machine is immutable (cannot add events/invariants dynamically)
- **Synthesis is worst-case exponential** - CC synthesis is NP-hard; the backtracking search prunes hard but can return UNDETERMINED beyond its budget (a SAT/SMT encoding would extend the reach). `Optimal` (branch-and-bound) is more expensive than the default first-found repair

## Multi-Language Support

While verification requires Go, **runtime is portable** to any language. Use `Machine.Export()` to serialize the verified machine to JSON:

<!-- gocheck: check machine -->
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
s = m.apply(s, 'request_shipment')
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
- CC verification (exhaustive pairs, compositional footprint components, failures), including a property test against brute-force enumeration of orderings
- Every Go block in the top-level docs (`TestDocSnippets`: blocks marked `run`, like the order-fulfillment example, are executed, so an example whose machine does not Build fails CI)
- Event order independence
- Compensation behavior
- State encoding/decoding

## License

Apache License 2.0 - see [LICENSE](LICENSE) and [NOTICE](NOTICE). The accompanying papers are licensed separately under CC-BY-4.0.

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

- [CONCEPTS.md](CONCEPTS.md) - Single-registry convergence, intuitively
- [FEDERATION-CONCEPTS.md](FEDERATION-CONCEPTS.md) - Federated (multi-registry) convergence, intuitively
- [Paper: Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)
- [Newman's Lemma](https://en.wikipedia.org/wiki/Newman%27s_lemma) - Foundation for confluence proofs
- [CRDTs](https://crdt.tech/) - Alternative approach via operation commutativity
- [Coordination Avoidance in Database Systems](https://dl.acm.org/doi/10.14778/2735508.2735509) - Invariant confluence (Bailis et al., VLDB 2014)
