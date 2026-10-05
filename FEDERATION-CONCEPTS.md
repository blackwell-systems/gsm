# Federated Convergence, Intuitively

This document is the multi-registry sequel to [CONCEPTS.md](CONCEPTS.md). That doc explained why a **single** registry converges: no matter what order events arrive, every replica lands on the same valid state. This one asks the same question of a whole **network** of registries wired together, and explains, with pictures before formulas, when the answer is still "yes."

If you're looking for:
- **Single-registry intuition**: See [CONCEPTS.md](CONCEPTS.md) (read this first)
- **API reference**: See [README.md#federated-registries](README.md#federated-registries)
- **Mathematical foundations**: See [THEORY.md](THEORY.md)
- **Academic paper**: See [Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)

## Table of Contents

- [Recap in One Breath](#recap-in-one-breath)
- [Many Registries, Connected](#many-registries-connected)
- [Morphisms Are Authority Arrows](#morphisms-are-authority-arrows)
- [The Local-to-Global Question](#the-local-to-global-question)
- [When It Just Works: No Loops](#when-it-just-works-no-loops)
- [When It Gets Hard: Cycles](#when-it-gets-hard-cycles)
- [Holonomy: Walk the Loop and See](#holonomy-walk-the-loop-and-see)
- [The Escape Hatches](#the-escape-hatches)
- [What gsm Does About It](#what-gsm-does-about-it)
- [The Real Names (Bridge to Rigor)](#the-real-names-bridge-to-rigor)
- [Glossary: Federation Terms → Code](#glossary-federation-terms--code)
- [Further Reading](#further-reading)

---

## Recap in One Breath

A single registry **converges**: [CONCEPTS.md](CONCEPTS.md) showed that if compensation always terminates (WFC) and any two events can be joined to the same result after repair (CC), then every event ordering reaches the same valid state. That is the whole promise, at one registry.

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
// The manufacturer's status fixes the supplier's listing.
image := map[string]string{"draft": "idle", "active": "listed", "suspended": "stale"}
fed.Morphism(mfr, sup).
    Shared(sstate).                                   // the target variable this arrow controls
    Map(func(srcNF, dst gsm.State) gsm.State {        // source normal form -> target's shared value
        return dst.Set(sstate, image[srcNF.Get(mstate)])
    }).
    Add()
```

Two things to hold onto:

- **`Shared` marks the controlled part.** The supplier's `sstate` is the *shared component*: the manufacturer owns it. Any other supplier variable is *local* and converges by the supplier's own compensation, untouched by this arrow.
- **The source wins ties.** If the supplier fires its own event and the manufacturer fires one too, the arrow re-derives the supplier's listing from the manufacturer's normal form. The supplier's local move is overwritten. No lock, no vote, no consensus round: authority is decided by the arrow's direction. That is what "coordination-free" means here.

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

The sweep settles the *state*; event order across registries is a separate question. A source event changes a target's shared component, and a target event that reads that shared component (in its guard, its effect, or the target's repair) can then land differently depending on which event arrived first: `recall` then `sell` leaves nothing sold, `sell` then `recall` leaves one sale. Each registry's own CC check cannot see this, because the two events sit on different registries. So `Build` also checks **cross-registry CC**: for every target, every image its morphism (or resolver) can produce from a valid source state, every target event, and every valid target state whose shared component is such an image, delivering the source change first and the target event first must end at the same target state. A failure is reported as a `CrossOrderError` naming the morphism, the source normal forms, the target event and state, and the two diverging results. Target events that touch only local variables, or that write a shared variable without reading one, always pass.

Two events on the *same* target raise a second question. The target's own CC check says they commute on the target alone, but in the federation the morphism repair runs after each one, and it can erase what the first event wrote into a shared variable before the second event reads it. The formal counterexample (`c2_counterexample` in the proof repository's `FederationEvents.v`) has a target with a shared `slot_a`, a local `slot_b` and an `audit` bit: `swap` exchanges the slots and `audit` records `slot_a xor slot_b`. They commute locally, the shared slot never changes (so cross-registry CC holds trivially), and yet `audit` then `swap` sets the audit bit while `swap` then `audit` clears it, because the repair after `swap` resets `slot_a`. So `Build` also checks **repaired CC**: for every pair of target events the target's own CC covers (every pair, or the declared `Independent` pairs), every image its sources can produce, and every valid target state consistent with that image, "first event, repair, second event, repair" must equal the other order. A failure is reported as a `SameTargetOrderError` naming both events, the target, the source normal form(s), the target state and the two diverging results.

In the proof these are the conditions C1 (cross-registry CC) and C2 (repaired CC). Together they are sufficient: every interleaving of independent events converges in an acyclic federation (`fed_events_commute`) and on a monotone cycle (`cyc_check_gc_lfp`, see `AllowMonotoneCycles` below). Both are checked statically, over every valid source state rather than only the states a run reaches, so a rejection is sound but can be conservative: the error says the federation *may* diverge from the reported state. The exact condition is the same two equations restricted to the witnesses a run can produce: from a given start, every interleaving converges **iff** C1 and C2 hold there ([`FederationEventsConverse.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationEventsConverse.v), `fed_exact`). The naive converse is false (`naive_converse_fails`: a federation can fail static C1 while every run converges), which is why gsm's message says "may".

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

gsm has this walk built in. `Federation.DiagnoseCycle` finds a directed cycle in your network, then iterates the loop's repair from the zero seed on a finite space (so it must either settle or repeat), and reports which. When the zero seed orbits, it also checks every other seed (every combination of the cycle components' valid states) for a consistent setting:

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

**Escape hatch 1: be acyclic.** No loops, so the local-to-global question never even arises. This is the [tree/DAG case above](#when-it-just-works-no-loops): resolve in topological order, once each, done. It is the default, and it is the one you should reach for unless you truly need a cycle.

**Escape hatch 2: make the repair monotone.** Sometimes you genuinely need a loop, and you can still be safe if every arrow's repair only ever pushes shared values in *one direction* along a ladder, never back down.

Picture the shared values sitting on a finite ladder of rungs, lowest at the bottom. **Monotone** means each arrow's repair can only move a value up a rung or leave it, never down. Now walk the loop: every round, values only climb or hold. But the ladder has finitely many rungs, so the climb cannot go on forever. It must stop, and where it stops is a rung nothing can push higher: a fixed point. That is the whole intuition. A monotone climb on a finite ladder has to halt, and it halts at the same top no matter which rung of which value you nudge first, so the result is order-independent.

That "a monotone map on a finite ordered set must reach a fixed point" is the plain-English form of a classical result (Knaster-Tarski). You do not need the machinery: the finite-ladder picture is the whole of it.

The negation loop from Example 2 fails exactly here: flipping `0` to `1` and `1` to `0` is not "only ever up." It goes up, then down, then up. That is why it orbits. A `max` rule, by contrast, only ever raises a value, so it is monotone. gsm's monotone mesh test wires a 3-cycle `A → B → C → A` where each node takes the `max` of its predecessor's request and shared value; the highest request (3) climbs all the way around the loop and everything settles at 3, order-independently.

**Escape hatch 3: coordinate the obstruction.** Suppose your loop is neither acyclic nor monotone (the negation loop is exactly this): it genuinely cannot converge on its own. You do not have to throw it away. You can pick a few shared variables and put them under a single writer, or a consensus round, so they are agreed externally rather than fought over around the loop. Fixing a shared value breaks the cycle through it, and once every loop is broken the rest of the network converges on its own, coordination-free.

The intuition: cutting a shared variable out of the fight is like pinning one link of a chain. Pin the right links and the chain can no longer pull against itself. The question is *which* links, and *how few*. gsm answers the first exactly and the second well: it walks the loops, finds a set of arrows whose coordination breaks every cycle, and hands it back as a plan. That set is a correct choice and never larger than the number of independent loops. (The truly smallest such set is a hard problem in general, so gsm gives you a correct, small one rather than promising the minimum; see the theory note for why.) A sharper plan that coordinates only the loops that actually twist, and keeps consistent loops such as lossless round-trips coordination-free, is designed in [HOLONOMY-COORDINATION-DESIGN.md](HOLONOMY-COORDINATION-DESIGN.md); its soundness is now machine-checked, with one requirement it makes explicit: the result is unique only once an authority root is chosen, so such a plan has to name its root.

The payoff is a *mixed-consistency* system: strong consistency (consensus) only on the handful of coordinated variables, eventual consistency (coordination-free) everywhere else. You pay the availability cost of coordination on a minimal-ish core, not the whole network.

---

## What gsm Does About It

Each of the following sentences is a real API behavior you can call.

- **`Build` rejects cycles by default.** With no opt-in, the network must be acyclic. If it finds a loop, `Build` refuses and its error *names the offending loop* (`A -> B -> A`), pointing you at `DiagnoseCycle`.
- **`AllowMonotoneCycles` opts into loops.** Call `Federation.AllowMonotoneCycles()` and `Build` stops requiring acyclicity. Instead it runs the monotonicity check (escape hatch 2): it verifies, by enumeration, that every morphism and resolver only pushes shared values one direction on the ladder. The check covers every state the climb can pass through, not just valid ones: the climb starts with every shared value on the bottom rung, which can be a state your invariants forbid, so gsm checks each valid local part with every shared value, and also requires each image there to depend only on its sources and to leave the target valid. A network that passes converges by Kleene iteration to the least fixed point. A non-monotone cyclic network (the negation counterexample) is still rejected even with the opt-in. Event order on the loop is certified by the same C1 and C2 checks. The climb starts by resetting every shared value to the bottom rung, so whatever an event writes into a shared variable is wiped, and only what it does to its own local variables survives. C1 and C2 compare exactly that, the locals after the final overwrite, with each target's image set taken over every valid source state, and that is enough for every interleaving of independent events to converge ([`FederationEventsCyclesCheck.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationEventsCyclesCheck.v), `cyc_check_gc_lfp`). A target fed by several sources is checked against the joint images of all of them at once (its resolver's output over every combination of valid source states, called R in the proof), so nothing relies on stitching per-edge checks together (`multi_edge_c1`, `multi_edge_gc` in `FederationEventsCyclesMulti.v`). Checking each edge's own images instead would not be enough for a resolver: with `r = plus` and edge images `{0, 1}`, the resolver can write `2` (`resolver_edge_insufficient`). Both checks earn their place: an event that copies a shared flag into a local alarm fails C1 (`check_rejects_latch`), and two events that commute on their own registry but not across the reset fail C2 (`c1_localcc_insufficient`).
- **`DiagnoseCycle` explains a rejected cycle.** When you hit a cycle rejection, `Federation.DiagnoseCycle` walks the loop from the zero seed and hands back a `CycleDiagnostic`: the `Cycle` (loop order), whether it `Converges`, and if not, the `Orbit` witness, plus whether any other seed is consistent (`SectionExists`) and whether the loop is a proven contradiction (`Obstructed()`), as in the [holonomy section](#holonomy-walk-the-loop-and-see).
- **`CoordinationPlan` names what to coordinate, and `BuildCoordinated` accepts it (escape hatch 3).** `Federation.CoordinationPlan()` returns the arrows (shared variables) to place under an external single writer or consensus so the network converges; `Federation.BuildCoordinated(plan)` then builds it, treating those variables as external inputs and converging on everything else. This turns a rejection into a deployable mixed-consistency plan. Each point names its `Authority` (the registry whose coordinated variables the cycle is now driven from), because the plan's choice of edge is a choice of root: cutting `B→A` in a copy-back loop makes A the authority and cutting `A→B` makes B the authority, and from the same state the two reach different normal forms (`CoordinatedCycles.v`, `root_choice_matters`). Given the plan, the normal form is unique.

So the flow is: build acyclic and it just works; if you need a loop, opt in with `AllowMonotoneCycles` and gsm proves the repair climbs one direction; if a build is rejected for a cycle, `DiagnoseCycle` shows you whether that loop settles or orbits; and if it orbits and you still need it, `CoordinationPlan` + `BuildCoordinated` accept it by coordinating the few obstructing variables. The full loop, start with something that cannot converge, get the exact adjustment, apply it, and watch it converge, is a worked example in the test suite (`Example_acceptWithCoordination`).

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
| **Cycle diagnosis** | `Federation.DiagnoseCycle()` | Walk a loop from the zero seed; report settle vs orbit; if it orbits, check every seed |
| **Settle-or-orbit result** | `CycleDiagnostic.Converges` / `.Orbit` | `true` = the zero seed settled (a consistent state exists); `false` = the zero seed orbits (the witness), not by itself an obstruction |
| **Obstruction** | `CycleDiagnostic.Obstructed()` / `.SectionExists` | `Obstructed()` = every seed checked and none consistent: the loop cannot converge |
| **Coordination plan** | `Federation.CoordinationPlan()` | The arrows (shared variables) to coordinate so the network converges (a correct feedback edge set); each point names its `Authority`, the root the cycle's normal form is driven from |
| **Accept with coordination** | `Federation.BuildCoordinated(plan)` | Build the network given that coordination: coordinated variables become external inputs; the rest converges |
| **Coordination point** | `CoordinationPoint` | One arrow to coordinate: its `Src`, `Dst`, and the `Shared` variables it controls |
| **Acyclic sweep** | topological order in `Build` | Resolve sources first, each arrow once, no iteration |
| **Monotone least fixed point** | Kleene iteration under `AllowMonotoneCycles` | Climb the ladder to the top rung; order-independent |

---

## Further Reading

- [CONCEPTS.md](CONCEPTS.md): single-registry convergence (WFC + CC); the prerequisite for this doc
- [THEORY.md](THEORY.md): the mathematical foundations, including the federation / multi-registry theorems (M1, R1/R2), the *Monotone Convergence Despite Cycles* result, and the Knaster-Tarski least-fixed-point argument
- [README.md#federated-registries](README.md#federated-registries): the federation API: `NewFederation`, `Morphism`/`Shared`/`Map`, `Resolve`, `AllowMonotoneCycles`, `Build`, `DiagnoseCycle`
- **[CATEGORICAL-STRUCTURE.md](https://github.com/blackwell-systems/normalization-confluence)** (in the sibling theory repository): the rigorous version of [The Real Names](#the-real-names-bridge-to-rigor), the cohomology of the morphism network (`H^0` / `H^1`) and the sheaf gluing condition
- **[Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)** (Blackwell, 2026): the paper this library implements (federation is §8)
