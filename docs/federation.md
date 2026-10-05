# Federation

A single registry governs one machine. Real systems span **multiple** registries with constraints across boundaries. `gsm` composes them into a **federation** connected by directed **morphisms**, and proves the whole network converges - the same build-time guarantee, one level up.

Morphisms encode cross-organizational constraints: a manufacturer's status constrains a supplier's listing, a regulator's rules constrain a bank. Repair composes freely: the federated normal form is unique on acyclic networks and monotone cycles. Event order across registries costs two local checks per edge, C1 and C2, which `Build` runs and which are proved sufficient (and exact at reachable states). Running each registry on its own node and merging projections needs one more, XU, which `Build` checks and reports ([Deployment](deployment.md#projection-deployments)).

This page explains, with pictures before formulas, when a whole network converges and which API does what. It is the multi-registry sequel to [Concepts](concepts.md), which explains why a **single** registry converges; read that first. The formal statements are in [Theory §11.4](theory.md#114-multi-registry-systems).

## Table of Contents

- [Recap in One Breath](#recap-in-one-breath)
- [Many Registries, Connected](#many-registries-connected)
- [Morphisms Are Authority Arrows](#morphisms-are-authority-arrows)
- [The Local-to-Global Question](#the-local-to-global-question)
- [When It Just Works: No Loops](#when-it-just-works-no-loops)
- [Event Order Across Registries: C1 and C2](#event-order-across-registries-c1-and-c2)
- [Multi-Source Targets](#multi-source-targets)
- [When It Gets Hard: Cycles](#when-it-gets-hard-cycles)
- [Holonomy: Walk the Loop and See](#holonomy-walk-the-loop-and-see)
- [The Escape Hatches](#the-escape-hatches)
- [Composing Federations: Embed and Certificates](#composing-federations-embed-and-certificates)
- [The Real Names (Bridge to Rigor)](#the-real-names-bridge-to-rigor)
- [Glossary: Federation Terms → Code](#glossary-federation-terms--code)
- [Further Reading](#further-reading)

---

## Recap in One Breath

A single registry **converges**: [Concepts](concepts.md) showed that if compensation always terminates (WFC) and any two events can be joined to the same result after repair (CC), then every event ordering reaches the same valid state. That is the whole promise, at one registry.

Now we connect **many** registries into a network and ask the very same question, one level up: when each registry is fine on its own, is the **entire network** consistent all at once? Sometimes yes automatically, sometimes only under a condition, and sometimes never. This doc is about telling those cases apart.

---

## Many Registries, Connected

Real systems are not one machine. An order flows through several independently-governed services, each with its own rules. Picture three registries in a fulfillment pipeline:

```
   ┌───────────────┐        ┌───────────────┐        ┌───────────────┐
   │   inventory   │        │  fulfillment  │        │   shipping    │
   │               │        │               │        │               │
   │ stock: 0..100 │───────>│ status:       │───────>│ label:        │
   │               │        │  idle|packing │        │  none|printed │
   └───────────────┘        └───────────────┘        └───────────────┘
       (source)                (in the middle)            (target)
```

Each box is a registry: its own variables, its own events, its own invariants, its own single-registry convergence proof. The **arrows** are new. An arrow says one registry's state constrains another's. Here: whether inventory has stock decides whether fulfillment may be packing, and whether fulfillment is packing decides whether shipping has printed a label.

In gsm you build this with `NewFederation`, then add the registries and the arrows:

<!-- gocheck: check federation -->
```go
fed := gsm.NewFederation("pipeline")
fed.Morphism(inventory, fulfillment)./* ... */Add()
fed.Morphism(fulfillment, shipping)./* ... */Add()
m, report, err := fed.Build() // proves the WHOLE network converges, or refuses
```

`Build` either hands back a machine for the whole network, proven convergent, or it refuses and tells you why. Everything below is about what it is checking.

---

## Morphisms Are Authority Arrows

An arrow is called a **morphism**. Read `src → dst` as: *the source is the authority over part of the target.* The source's own normal form (its settled, valid state) deterministically fixes some of the target's variables. You name which variables with `Shared`, and you give the rule that computes them with `Map`.

A concrete instance, straight from gsm's test suite (`federation_test.go`): a manufacturer registry is authoritative over a supplier's listing status.

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

Two things to hold onto:

- **`Shared` marks the controlled part.** The supplier's `sstate` is the *shared component*: the manufacturer owns it. Any other supplier variable is *local* and converges by the supplier's own compensation, untouched by this arrow.
- **The source wins ties.** In a morphism `A → B`, source `A` is **authoritative** over `B`'s shared component: `A`'s normal form deterministically fixes it. If the supplier fires its own event and the manufacturer fires one too, the arrow re-derives the supplier's listing from the manufacturer's normal form. The supplier's local move is overwritten. No lock, no vote, no consensus round: authority is decided by the arrow's direction. That is what **coordination-free conflict resolution** means here.

---

## The Local-to-Global Question

Every registry is convergent on its own. Every arrow, taken by itself, is a clean rule for copying authority. So why is there anything left to prove?

Because "each part is consistent" is not the same as "the whole is consistent."

An analogy. You have a room of wristwatches. Each watch is *self-consistent*: its second hand, minute hand, and hour hand all agree with each other, every watch a perfectly good watch. Now wire them together: watch B must read whatever watch A reads, watch C must read whatever B reads, and so on. Each pairwise rule is simple. The real question is whether, once every wire is satisfied at once, **all the watches show the same time**, or whether the constraints fight each other so that no single global setting satisfies them all.

That is the **local-to-global question**. Local: each registry, each arrow, fine. Global: is there a single assignment of every registry's state that satisfies *every* arrow simultaneously, and does the network *reach* it regardless of event order? For a federation, "consistent" means exactly that.

---

## When It Just Works: No Loops

If the arrows form no loops, the answer is always yes, and the reason is almost boring.

A network with no loops is a **tree** (or a **DAG**, a directed acyclic graph, if a registry may have more than one incoming arrow). "Acyclic" just means you can never follow arrows forward and end up back where you started.

When there are no loops you can lay the registries out in **dependency order**: sources first, then whatever they feed, then whatever *those* feed. Then you resolve arrows once each, left to right:

```
   root ──> a
        ──> b ──> d
        │    └──> e
        └──> c

   sweep:  root → a → b → c → d → e
           (each node is finalized before any arrow leaving it fires)
```

Normalize the root. Its normal form fixes the shared components of `a`, `b`, `c`. Now `b` is finalized, so fire `b`'s arrows to fix `d` and `e`. Done. Every arrow fired exactly once, and by the time an arrow fires, its source was already final.

Why can no conflict arise? Because nothing you decide later loops back to disturb something you decided earlier. Authority only ever flows *forward*. When you set `d`'s shared value from `b`, there is no arrow from `d` back to `b` to un-settle `b`. The topological sweep visits each node after all its authorities are fixed, so the order of the sweep is forced and the result is unique. (This is the branching-tree case in `federation_test.go`, and a ten-deep chain that propagates a flag through all ten registries in one pass.)

That is why gsm's constructive normal form is: normalize each source independently, then propagate shared components along arrows in topological order. No product state space, no fixed-point loop, no iteration. It just falls out.

**The build-time contract, extended.** `Build()` refuses any federation that cannot converge: the network must be a tree/forest (no cycles; at most one source per target; a [resolver](#multi-source-targets) relaxes this), component names must be distinct, and every morphism must preserve target validity when it overwrites the shared component (the *M1* condition). Event order is checked too ([below](#event-order-across-registries-c1-and-c2)). A federated machine exists only if the whole network is proven convergent. The federated normal form is *constructive* - each source normalizes independently, then shared components propagate along morphisms in topological order - so federation never materializes the product state space.

---

## Event Order Across Registries: C1 and C2

The sweep settles the *state*; event order across registries is a separate question. A source event changes a target's shared component, and a target event that reads that shared component (in its guard, its effect, or the target's repair) can then land differently depending on which event arrived first: `recall` then `sell` leaves nothing sold, `sell` then `recall` leaves one sale. Each registry's own CC check cannot see this, because the two events sit on different registries. So `Build` also checks **cross-registry CC**: for every target, every image its morphism (or resolver) can produce from a valid source state, every target event, and every valid target state whose shared component is such an image, delivering the source change first and the target event first must end at the same target state. A failure is reported as a `CrossOrderError` naming the morphism, the source normal forms, the target event and state, and the two diverging results. Target events that touch only local variables, or that write a shared variable without reading one, always pass.

Two events on the *same* target raise a second question. The target's own CC check says they commute on the target alone, but in the federation the morphism repair runs after each one, and it can erase what the first event wrote into a shared variable before the second event reads it. The formal counterexample (`c2_counterexample` in the proof repository's `FederationEvents.v`) has a target with a shared `slot_a`, a local `slot_b` and an `audit` bit: `swap` exchanges the slots and `audit` records `slot_a xor slot_b`. They commute locally, the shared slot never changes (so cross-registry CC holds trivially), and yet `audit` then `swap` sets the audit bit while `swap` then `audit` clears it, because the repair after `swap` resets `slot_a`. So `Build` also checks **repaired CC**: for every pair of target events the target's own CC covers (every pair, or the declared `Independent` pairs), every image its sources can produce, and every valid target state consistent with that image, "first event, repair, second event, repair" must equal the other order. A failure is reported as a `SameTargetOrderError` naming both events, the target, the source normal form(s), the target state and the two diverging results.

In the proof these are the conditions C1 (cross-registry CC) and C2 (repaired CC). Together they are sufficient: every interleaving of independent events converges in an acyclic federation (`fed_events_commute`) and on a monotone cycle (`cyc_check_gc_lfp`, see [monotone cycles](#escape-hatch-2-monotone-cycles)). Both are checked statically, over every valid source state rather than only the states a run reaches, so a rejection is sound but can be conservative: the error says the federation *may* diverge from the reported state. The exact condition is the same two equations restricted to the witnesses a run can produce: from a given start, every interleaving converges **iff** C1 and C2 hold there ([`FederationEventsConverse.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationEventsConverse.v), `fed_exact`). The naive converse is false (`naive_converse_fails`: a federation can fail static C1 while every run converges), which is why gsm's message says "may".

C1 and C2 certify the `FedMachine`, where the morphism repair runs after every event. A deployment in which each node runs only its own registry and merges its sources' projections when they arrive is a different model, which needs the stronger condition XU: see [Deployment](deployment.md#projection-deployments).

---

## Multi-Source Targets

When a target has *two* sources, the authority argument doesn't pick a winner, so the target declares a `Resolver` that deterministically merges its sources (priority, AND/OR, most-restrictive, etc.), relaxing the tree requirement to any acyclic DAG:

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

Multi-source convergence is the paper's **Federated Convergence with Resolution** theorem (§8), which holds whenever the resolver is a function of its sources alone (**R1**) and preserves target validity (**R2**, the multi-source generalization of M1). gsm certifies exactly those hypotheses: it **exhaustively verifies at build time** that, for *this* federation, the resolver writes only shared variables, satisfies R1, and satisfies R2: the same verify-the-preconditions contract it applies to single-registry WFC/CC. A resolver that could diverge (violates R2), reads local state (violates R1), or writes non-shared variables is rejected; a multi-source target *without* a resolver is rejected. A target has exactly one resolver: a second `Resolve` for the same target, or an embedded sub-federation that brings a resolver for a target that already has one, panics rather than replacing the first.

> Single-source authority is the special case of a resolver with one source. Both are backed by the paper's proofs (Federated Convergence, and its multi-source generalization); gsm's build-time checks establish the theorems' preconditions.

---

## When It Gets Hard: Cycles

The trouble starts when the arrows form a **loop**: a directed cycle. Follow arrows forward and you come back to where you began.

```
        ┌─────────────────┐
        │                 │
        v                 │
   ┌────────┐        ┌────────┐
   │   A    │───────>│   B    │
   └────────┘        └────────┘
        ^                 │
        │                 v
        │            ┌────────┐
        └────────────│   C    │
                     └────────┘

   A → B → C → A     (a 3-cycle)
```

Now the "resolve once in order" trick has no order to use. A's shared component depends on C. C's depends on B. B's depends on A. There is no first node to finalize: each is waiting on the one behind it, all the way around.

This is a genuine **circular dependency**, not a bookkeeping annoyance. It might still be fine (all three might happily agree on one setting), or it might be a contradiction dressed up as a loop (each arrow demanding the next disagree with the last). You cannot tell by looking at any single arrow. You have to walk the whole loop.

**`Build` rejects cycles by default.** With no opt-in, the network must be acyclic. If it finds a loop, `Build` refuses and its error *names the offending loop* (`A -> B -> A`), pointing you at `DiagnoseCycle`.

---

## Holonomy: Walk the Loop and See

Here is the one idea that decides a cycle. Give it a plain name: **holonomy**. It means: *start somewhere, walk all the way around the loop applying each arrow's repair in turn, and see whether you come back to where you started.*

If walking the loop leaves the shared values unchanged, those values are a **consistent** setting: one every arrow is happy with. If walking the loop keeps *changing* the values and they never stop changing, the shared state drifts forever in a repeating pattern, called an **orbit**. From any one start, the values either settle to a fixed point or orbit. The loop is a **contradiction** only when *every* start orbits: one orbiting start does not rule out a consistent setting somewhere else (Example 3). Three worked examples make the split concrete.

### Example 1: a loop that settles

Two registries, each with one variable holding 0 or 1. A copies its value to B; B copies its value straight back to A. (This is `TestDiagnoseCycle_IdentitySettles`.)

<!-- gocheck: check federation -->
```go
fed.Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
    Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, s.GetInt(fb)) }).Add()
```

Start from the zero seed, `fa=0, fb=0`, and walk the loop:

```
  round 0:   fa=0, fb=0
  A→B copies fa into fb:   fb = 0     (no change)
  B→A copies fb into fa:   fa = 0     (no change)
  round 1:   fa=0, fb=0    ← identical to round 0: settled
```

Nothing moved. `fa=0, fb=0` is a fixed point. Walking the loop returns you exactly where you started, so the loop is consistent. (Every other starting value, `fa=fb=1`, is a fixed point too.) The "copy around a loop" composite is the identity, and the identity has fixed points everywhere.

### Example 2: a loop that orbits

Same two registries, but now B copies back the **negation** of A's value. (This is `TestDiagnoseCycle_NegationOrbit`.)

<!-- gocheck: check federation -->
```go
fed.Morphism(a, b).Shared(fb).Map(func(s, d State) State { return d.SetInt(fb, s.GetInt(fa)) }).Add().
    Morphism(b, a).Shared(fa).Map(func(s, d State) State { return d.SetInt(fa, 1-s.GetInt(fb)) }).Add()
```

Start again from `fa=0, fb=0` and walk:

```
  round 0:   fa=0, fb=0
  A→B copies fa into fb:      fb = 0
  B→A sets fa = 1 - fb:       fa = 1     ← changed
  round 1:   fa=1, fb=0
  A→B copies fa into fb:      fb = 1
  B→A sets fa = 1 - fb:       fa = 0     ← changed back
  round 2:   fa=0, fb=1
  ... the shared values never stop flipping: an orbit
```

Walking the loop never returns you to a stable point. The composite around the loop is a **flip**, and a flip on `{0,1}` has no fixed point: whatever you feed in comes out the other value, from either start. There is no global setting every arrow accepts, so the network cannot converge. The endless sequence is the **orbit witness** for the zero seed; what makes the loop a contradiction is that no start settles.

### Example 3: a seed that orbits, a loop that does not contradict

Widen both variables to `{0,1,2}`. A copies to B; B copies back through a **swap** that exchanges 0 and 1 and leaves 2 alone. (This is `TestDiagnoseCycle_SeedOrbitsButSectionExists`.) From the zero seed the walk is Example 2 again: 0 and 1 trade places forever. But start from `fa=2, fb=2` and nothing moves: 2 is a consistent setting. So an orbit from one seed is not proof of a contradiction. The exact statement (machine-checked in normalization-confluence, `CohomologyGeneral.v`: `c15_definitive_claim_false`, `c15_exact_refuter`) is that a loop has no consistent setting iff **no** seed reaches a fixed point.

### This is exactly what DiagnoseCycle computes

gsm has this walk built in. When a cycle is rejected, `Federation.DiagnoseCycle` finds a directed cycle in your network, then iterates the loop's repair from the zero seed on a finite space (so it must either settle or repeat), and reports which (the loop-composite fixed-point witness). When the zero seed orbits, it also checks every other seed (every combination of the cycle components' valid states) for a consistent setting, and reports the cycle as `Obstructed()` only when none is, since one oscillating seed does not rule out a consistent state elsewhere ([`CohomologyGeneral.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CohomologyGeneral.v)):

<!-- gocheck: check federation -->
```go
d, err := fed.DiagnoseCycle()
if d != nil {
    fmt.Println(d.Cycle)         // the loop, in order: e.g. ["A", "B"]
    fmt.Println(d.Converges)     // true if the zero seed settled, false if it orbits
    fmt.Println(d.Orbit)         // if !Converges, the zero seed's repeating configurations
    fmt.Println(d.SectionExists) // whether some seed is a consistent setting
    fmt.Println(d.Obstructed())  // true only if every seed was checked and none settles
}
```

For the negation loop above, `Converges` is `false`, `Orbit` holds the repeating sequence of shared configurations, and `Obstructed()` is `true`: all four seeds were checked and none is consistent. For the swap loop of Example 3, `Converges` is `false` but `SectionExists` is `true` (`Section` shows `fa=fb=2`), so it is not an obstruction. For the copy-around loop, `Converges` is `true` (the loop is still named for reference). Two cautions: `Converges == true` means *this* seed settles, which proves a consistent setting exists but not that every start reaches one; and when the cycle's seeds are too many to check (`AllSeeds == false`), a `false` `Converges` says only that the zero seed orbits.

---

## The Escape Hatches

If cycles are where convergence can fail, there are three ways to handle it: avoid the loop, tame it
with monotone repair, or (when neither applies) accept it by coordinating the few variables that
actually obstruct.

### Escape hatch 1: be acyclic

No loops, so the local-to-global question never even arises. This is the [tree/DAG case above](#when-it-just-works-no-loops): resolve in topological order, once each, done. It is the default, and it is the one you should reach for unless you truly need a cycle.

### Escape hatch 2: monotone cycles

Sometimes you genuinely need a loop, and you can still be safe if every arrow's repair only ever pushes shared values in *one direction* along a ladder, never back down.

Picture the shared values sitting on a finite ladder of rungs, lowest at the bottom. **Monotone** means each arrow's repair can only move a value up a rung or leave it, never down. Now walk the loop: every round, values only climb or hold. But the ladder has finitely many rungs, so the climb cannot go on forever. It must stop, and where it stops is a rung nothing can push higher: a fixed point. That is the whole intuition. A monotone climb on a finite ladder has to halt, and it halts at the same top no matter which rung of which value you nudge first, so the result is order-independent.

That "a monotone map on a finite ordered set must reach a fixed point" is the plain-English form of a classical result (Knaster-Tarski). You do not need the machinery: the finite-ladder picture is the whole of it.

The negation loop from Example 2 fails exactly here: flipping `0` to `1` and `1` to `0` is not "only ever up." It goes up, then down, then up. That is why it orbits. A `max` rule, by contrast, only ever raises a value, so it is monotone. gsm's monotone mesh test wires a 3-cycle `A → B → C → A` where each node takes the `max` of its predecessor's request and shared value; the highest request (3) climbs all the way around the loop and everything settles at 3, order-independently.

**In gsm.** Acyclicity is only needed to tame *non-monotone* repair (the divergence counterexample is negation, which is antitone). With `Federation.AllowMonotoneCycles()`, cyclic networks are allowed when every morphism/resolver is **monotone**, verified by enumeration over every state the iteration can visit. The climb starts with every shared value on the bottom rung, which can be a state your invariants forbid, so gsm checks each valid local part with any shared values, not only valid states, together with source-determinacy and validity of every image there. `Build` then computes the normal form by Kleene iteration to the least fixed point, which converges order-independently even on arbitrary cyclic graphs (the result cannot depend on the order morphisms were declared; if a closure misbehaves at run time so the iteration does not settle within its bound, `Apply` panics rather than return a non-fixed point) (the paper's *Monotone Convergence Despite Cycles*, via Knaster–Tarski + chaotic iteration). State-based CRDTs are the compensation-free special case of this monotone regime; that CRDTs (op- and state-based) are a *strict* sub-fragment of normalization confluence is machine-checked in [`CRDT.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CRDT.v) (see [SUBSUMPTION.md](https://github.com/blackwell-systems/normalization-confluence/blob/main/SUBSUMPTION.md)). Non-monotone cycles are still rejected, even with the opt-in.

Event order on a monotone cycle is certified by the same per-target C1 and C2 checks. Kleene iteration resets every shared variable to bottom, so an event's shared writes are erased and only its local outcome survives, and C1 and C2 (locals compared after the final overwrite, each target's image set taken over every valid source state) imply that every interleaving of independent events converges ([`FederationEventsCyclesCheck.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationEventsCyclesCheck.v), `cyc_check_gc_lfp`; the exact condition, global commutation after re-normalization, is `gc_iff` in [`FederationEventsCycles.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationEventsCycles.v)). A multi-source target on a cycle is checked against the joint image set of all its incoming edges (its resolver's image R over every combination of valid source states, not each edge's own image, which would not suffice: with `r = plus` and edge images `{0, 1}`, the resolver can write `2`, `resolver_edge_insufficient`), so the per-edge combination route (`multi_edge_c1`, `multi_edge_gc` in `FederationEventsCyclesMulti.v`, which needs M1) is not relied on. Both conditions are needed: an event that copies a shared value into a local fails C1 (`check_rejects_latch`), and a pair that commutes on its registry but not across the reset fails C2 (`c1_localcc_insufficient`).

The opt-in belongs to the federation it is called on: embedding a sub-federation that opted in (with `Embed` or `EmbedCertified`) does not opt the parent in, so a parent with a cycle must call `AllowMonotoneCycles` itself (`Build`'s cycle error names the opted-in sub).

A monotone cycle deployed as separate nodes that merge projections, instead of one `FedMachine`, can settle on the wrong fixed point (a "ghost"), so `Build` reports such a deployment as not certified. Barrier reset epochs are the general fix. Without resets, given cyclic C1 and C2, the deployment agrees with the `FedMachine` exactly when it flushes and has no reachable ghost (`lens_noreset_iff`, `lens_noreset_fair_iff` in normalization-confluence [`coq/docs/distributed.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/docs/distributed.md#the-no-reset-model-on-monotone-cycles-exactly-distributedcyclesexactv)); the cheap sufficient checks are inflationary events (per event) or a unique fixed point (global), and otherwise it takes a global search. gsm implements neither route. See [Deployment](deployment.md#cycles-ghosts-and-reset-epochs).

### Escape hatch 3: coordinate the obstruction

Suppose your loop is neither acyclic nor monotone (the negation loop is exactly this): it genuinely cannot converge on its own. You do not have to throw it away. You can pick a few shared variables and put them under a single writer, or a consensus round, so they are agreed externally rather than fought over around the loop. Fixing a shared value breaks the cycle through it, and once every loop is broken the rest of the network converges on its own, coordination-free.

The intuition: cutting a shared variable out of the fight is like pinning one link of a chain. Pin the right links and the chain can no longer pull against itself. The question is *which* links, and *how few*. gsm answers the first exactly and the second well:

<!-- gocheck: check federation -->
```go
if _, _, err := fed.Build(); err != nil { /* rejected: non-monotone cycle */ }
d, _ := fed.DiagnoseCycle()            // d.Obstructed() == true: no starting value settles, and the orbit shows why
plan := fed.CoordinationPlan()         // a small, correct set of points to coordinate
m, _, _ := fed.BuildCoordinated(plan)  // accept the network given that coordination
// now drive it and watch the rest converge with no further coordination
```

`Federation.CoordinationPlan` returns a set of morphism edges (shared variables) to place under an external single writer or consensus, and `Federation.BuildCoordinated(plan)` builds the federation given that coordination: the coordinated edges become external inputs and the acyclic residual converges coordination-free. gsm does not check the coordination mechanism itself (`Report.Coordinated` lists the inputs it must serialize). Each point names its `Authority`, the registry whose coordinated variables the cycle is then driven from: the normal form is unique given the plan, but cutting a different edge of the same cycle picks a different root and, from the same state, a different normal form ([`CoordinatedCycles.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CoordinatedCycles.v): `root_choice_matters`, `copyback_without_authority`). Cutting `B→A` in a copy-back loop makes A the authority and cutting `A→B` makes B the authority.

This is a localized mixed-consistency partition (consensus only on the obstructing edges): strong consistency only on the handful of coordinated variables, eventual consistency (coordination-free) everywhere else. You pay the availability cost of coordination on a minimal-ish core, not the whole network. The plan is a correct feedback edge set of size at most the number of independent cycles, not necessarily the minimum, which is the group feedback edge set problem (NP-hard in general). A sharper plan, which coordinates only the cycles whose holonomy is non-trivial and accepts consistent cycles (such as lossless round-trips) without coordination, is designed in [HOLONOMY-COORDINATION-DESIGN.md](design/HOLONOMY-COORDINATION-DESIGN.md); its soundness is now machine-checked, with one requirement it makes explicit: the result is unique only once an authority root is chosen, so such a plan has to name its root.

So the flow is: build acyclic and it just works; if you need a loop, opt in with `AllowMonotoneCycles` and gsm proves the repair climbs one direction; if a build is rejected for a cycle, `DiagnoseCycle` shows you whether that loop settles or orbits; and if it orbits and you still need it, `CoordinationPlan` + `BuildCoordinated` accept it by coordinating the few obstructing variables. The full loop, start with something that cannot converge, get the exact adjustment, apply it, and watch it converge, is a worked example in the test suite (`Example_acceptWithCoordination`). That is the real shape of the guarantee: converge for free where you can, and get told precisely what it costs where you cannot.

---

## Composing Federations: Embed and Certificates

**Compositional construction.** A verified sub-federation embeds into a larger one with `Federation.Embed`: define and verify a subsystem on its own, then reuse it as a unit and connect it with more morphisms. The composed federation runs as the flat convergent machine (a `FedState` holds one `State` per component, so no product state space is materialized). This realizes the paper's compositional-collapse result (a convergent sub-federation collapses to an effective registry), enabling modular, hierarchical verification and black-box reuse of subsystems.

<!-- gocheck: check federation -->
```go
sub := gsm.NewFederation("pricing").Morphism(pricing, catalog)./* ... */Add()
sub.Build() // verify the subsystem on its own

m, _, _ := gsm.NewFederation("storefront").
    Embed(sub).                       // reuse the verified subsystem as a unit
    Morphism(catalog, order)./* ... */Add().
    Build()
```

**Certificate-based reuse.** A verified sub-federation can be packaged as a `Certificate` and reused, pinned to what was certified. `sub.Certify()` builds and verifies the subsystem and returns a certificate carrying its build report, each morphism and resolver in extensional table form (reified from the finite, source-determined maps), and a digest over the component rules, the names and declared pairs they are addressed by, and those tables (it binds each morphism closure only through its table, which records the images at one representative target; the digest is an integrity binding, not authentication). `EmbedCertified(sub, cert)` embeds it on that certificate: `Build` re-checks the internal morphisms from the certificate's tables, re-runs the live internal closures over every valid source and target state so they cannot differ from the tables there (a closure that writes a non-shared variable is refused, as plain `Build` refuses it), checks the seam (the boundary morphisms) and the whole-graph acyclicity, and rebuilds each certified component with `Build`, which re-checks its convergence.

At run time the machine **executes the certificate's verified tables** for the sub's internal morphisms and resolvers: a repair is a table lookup (copied from the certificate at `Build`), and the sub's `Map` and `Resolver` closures are not called, so the guarantee is about exactly what runs. On every state the certificate covers the result is the same as the closures' (`Build` binds them), but a side effect in a closure does not happen; a `FedState` outside the certified domain makes `Normalize` and `Apply` panic, naming it. A cyclic network, a certified target that also has an outer writer, and `SharedProjection` along a morphism into a resolved target still run the (verified) closures; `FedReport.Runtime` says which.

A consumer that receives a certificate re-checks it independently with `cert.Verify(components)`, which re-derives validity preservation (M1/R2), table completeness (one row per valid source state, valid source ids) and, for a cyclic `Monotone` certificate, monotonicity from the tables rather than the producer's morphism closures and re-checks every component's convergence, so a composition is confirmed without trusting the producer's code or its recorded report. An outer morphism may read a certified subsystem, or write one of its declared **input ports** (shared variables the sub leaves free): declare them at `Certify(gsm.Port{Registry: r, Var: v})`, and `Build` verifies each inbound boundary morphism at the seam (M1/R2). A write to any other (sealed) variable is rejected.

<!-- gocheck: check federation -->
```go
cert, _ := sub.Certify() // verify once; package the result + morphism tables + digest

m, _, _ := gsm.NewFederation("storefront").
    EmbedCertified(sub, cert). // reuse without re-verifying internals; only the seam is checked
    Morphism(catalog, order)./* ... */Add().
    Build()

// A consumer re-checks the certificate independently, from the tables (not the producer's closures).
// Each key is that registry's name.
err := cert.Verify(map[string]*gsm.Registry{"pricing": pricing, "catalog": catalog})
```

What these re-checks trust, and what they do not, is in [Verification](verification.md#certificates-what-gsm-recomputes-itself). The design, including the digest's coverage and the trust policy, is [CERTIFICATE-DESIGN.md](design/CERTIFICATE-DESIGN.md).

---

## The Real Names (Bridge to Rigor)

Everything above has a formal name, offered here only as optional deeper reading. You do not need any of it to use gsm.

The **local-to-global question** ("each part is fine; is the whole consistent?") is what mathematicians call a **cohomology** question. The globally consistent states (the settings every arrow accepts at once) are the *degree-zero* cohomology, written `H^0`. The obstruction, the twist that stops a loop from agreeing with itself, is the *degree-one* cohomology, `H^1`. `H^1` is precisely the [holonomy](#holonomy-walk-the-loop-and-see) you saw: zero twist means the loop settles, nonzero twist means it orbits.

The **settle-or-drift condition** ("the local pieces agree on their overlaps well enough to glue into one global object") is the **sheaf gluing condition**. A federation converges globally exactly when its local shared components glue.

These names are the rigorous version of the pictures in this doc, nothing more. For the full treatment (categorical structure of federated convergence, the cohomology of the morphism network, the gluing argument), see the `CATEGORICAL-STRUCTURE.md` note in the sibling theory repository, [normalization-confluence](https://github.com/blackwell-systems/normalization-confluence). This doc deliberately does not reproduce that math; it just marks the bridge to it.

---

## Glossary: Federation Terms → Code

| Federation Term | Code Equivalent | Meaning |
|-----------------|-----------------|---------|
| **Federation** | `NewFederation(name)` / `Federation` | A network of registries connected by morphisms |
| **Morphism (authority arrow)** | `Federation.Morphism(src, dst)` | A directed arrow: the source fixes part of the target |
| **Shared component** | `MorphismBuilder.Shared(vars...)` | The target variables the arrow controls (the rest are local) |
| **Morphism image** | `MorphismBuilder.Map(fn)` | The rule computing the shared values from the source's normal form |
| **Register a component** | `Federation.Add(r)` | Add an isolated registry (morphisms auto-register their endpoints) |
| **Resolver** | `Federation.Resolve(target, fn)` | How a multi-source target merges its incoming arrows (AND/OR/priority) |
| **Monotone-cycle opt-in** | `Federation.AllowMonotoneCycles()` | Permit loops when every repair is monotone (climbs one direction) |
| **Build the network** | `Federation.Build()` | Prove the whole network converges, or refuse and say why |
| **Shared projection** | `FedMachine.SharedProjection` / `Machine.MergeProjection` | The message a source node sends its target, and the target node merging it, for a deployment without a `FedMachine` |
| **Projection-safe (XU)** | `FedReport.ProjectionSafe` / `Federation.RequireProjectionSafe()` | Whether nodes that merge projections whenever they arrive still converge (C1 at every valid target state); reported by default, required with the opt-in |
| **Cycle diagnosis** | `Federation.DiagnoseCycle()` | Walk a loop from the zero seed; report settle vs orbit; if it orbits, check every seed |
| **Settle-or-orbit result** | `CycleDiagnostic.Converges` / `.Orbit` | `true` = the zero seed settled (a consistent state exists); `false` = the zero seed orbits (the witness), not by itself an obstruction |
| **Obstruction** | `CycleDiagnostic.Obstructed()` / `.SectionExists` | `Obstructed()` = every seed checked and none consistent: the loop cannot converge |
| **Coordination plan** | `Federation.CoordinationPlan()` | The arrows (shared variables) to coordinate so the network converges (a correct feedback edge set); each point names its `Authority`, the root the cycle's normal form is driven from |
| **Accept with coordination** | `Federation.BuildCoordinated(plan)` | Build the network given that coordination: coordinated variables become external inputs; the rest converges |
| **Coordination point** | `CoordinationPoint` | One arrow to coordinate: its `Src`, `Dst`, and the `Shared` variables it controls |
| **Acyclic sweep** | topological order in `Build` | Resolve sources first, each arrow once, no iteration |
| **Monotone least fixed point** | Kleene iteration under `AllowMonotoneCycles` | Climb the ladder to the top rung; order-independent |
| **Embed** | `Federation.Embed(sub)` | Reuse a verified sub-federation as a unit (re-verified in the whole) |
| **Certificate** | `Federation.Certify` / `EmbedCertified` / `Certificate.Verify` | Reuse a sub-federation pinned to a re-checkable certificate; the machine runs its tables |

---

## Further Reading

- [Concepts](concepts.md): single-registry convergence (WFC + CC); the prerequisite for this doc
- [Deployment](deployment.md): running a federation (a shared log, or one node per registry with projections) and what each needs
- [Theory](theory.md): the mathematical foundations, including the federation / multi-registry theorems (M1, R1/R2), the *Monotone Convergence Despite Cycles* result, and the Knaster-Tarski least-fixed-point argument
- [Reference](reference.md#fedreport): `FedReport` fields and the federation error types
- **[CATEGORICAL-STRUCTURE.md](https://github.com/blackwell-systems/normalization-confluence)** (in the sibling theory repository): the rigorous version of [The Real Names](#the-real-names-bridge-to-rigor), the cohomology of the morphism network (`H^0` / `H^1`) and the sheaf gluing condition
- **[Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)** (Blackwell, 2026): the paper this library implements (federation is §8)
