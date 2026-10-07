# Deployment

`Build`'s guarantee is about a model: which events arrive, how often, in what order, and where
the repair runs. Some of what that model assumes is about your runtime, not your rules, so `Build`
cannot enforce it. It measures what it can and names the rest in the report. This page lists each
obligation, what the report says about it, and what to do.

- [Delivery](#delivery): each event once, and causal order for undeclared pairs.
- [Running a federation as one machine](#running-a-federation-as-one-machine): the `FedMachine`, for example over a shared log.
- [Projection deployments](#projection-deployments): one node per registry, merging projections.
- [Cycles: ghosts and reset epochs](#cycles-ghosts-and-reset-epochs): why projection deployments on a cycle are not certified, and what would certify them.
- [Changing a running system](#changing-a-running-system): check a change of rules or schema before you deploy it: safe online, safe behind a barrier, or unsafe with a witness.

Each report's regime summary (`Report.Regime`, `FedReport.Regime`, `MigrationReport.Regime`) lists
the rules below that apply to what you built, under "You must provide", and what is not covered,
with a pointer back here ([Regime summary](verification.md#regime-summary)).

To run a built machine outside Go, see the [export format](reference.md#multi-language-runtime).

---

## Delivery

### Duplicates and redelivery

The guarantee is about orderings of one multiset of events: each event applied once. If your
transport can redeliver (an at-least-once queue, a client retry), check `Report.NotIdempotent`.
It lists every event whose second application changes the state, and the report prints:

```
  Delivery: exactly once for withdraw (applying one twice differs from once); deduplicate redelivered events
```

Those events need an event id and a dedupe set (or equivalent) in front of `Apply`. The list is
exact for free and causal delivery (normalization-confluence `alo_exact`, `causal_alo_exact`).
If the registry declares `Independent` pairs, deduplicating exactly the listed events is still
enough, provided your transport keeps every undeclared pair in order for retries as well as first
deliveries (normalization-confluence `dalo_unlisted_converge`, `dalo_gsm_build`). A retry that
jumps ahead of an undeclared partner can diverge even when nothing is listed
(`fl_retry_order_needed`); if your transport cannot promise that ordering, deduplicate every event. A non-idempotent
event delivered twice always diverges from delivering it once, while an idempotent one is absorbed
when it commutes with what arrived between the copies (normalization-confluence
[`AtLeastOnce.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/AtLeastOnce.v): `non_idempotent_diverges`, `alo_absorbed`). A counter
increment is the typical case that needs deduplication; setting a flag is not.

Under causal delivery (declared-only mode, [below](#causal-order-for-undeclared-pairs)) there is one more rule: a redelivered copy must not
arrive after an event that causally follows the original. Idempotence does not cover that case
(`late_duplicate_diverges`: `add`, `remove`, then a late duplicate `add` leaves a flag set that
exactly-once delivery clears).

The formal account, including when every at-least-once delivery reaches the exactly-once result,
is [Theory §7.8](theory.md#78-delivery-duplicates-and-redelivery).

### Causal order for undeclared pairs

When you declare some pairs `Independent` ([Getting started](getting-started.md#independence-declarations)),
**every undeclared pair must be delivered in causal order.** Declared-only mode is sound only if each pair you did not declare reaches every replica in one fixed order (normalization-confluence `coq/Trace.v` `run_tequiv`, `coq/CausalReplay.v` `causal_tequiv`). gsm cannot see your delivery order, so `Build` still checks every undeclared pair on its step tables (two lookups per state, no closure calls) and does not fail on them, but lists each one that does not commute in `Report.CausalOrderRequired`, with a witness state. The report then reads:

```
  Undeclared pairs: 5 checked, 3 do not commute: each must be causally ordered (not independent), delivered in the same order at every replica
    (close, deposit) from {open=true, bal=0, notified=false}: close→deposit gives ..., deposit→close gives ...
  Convergence: GUARANTEED under causal delivery of the 3 undeclared pair(s) above
```

If your runtime can reorder a listed pair, it is not causally ordered: declare it `Independent` (and fix the rules until it commutes) instead. An empty list means every pair commutes and the declarations cost nothing.

---

## Running a federation as one machine

A `FedMachine` (from `Federation.Build`) runs the morphism repair after every event, so a target
never acts on a shared value its source has not approved. That is the model C1 and C2 certify
([Federation](federation.md#event-order-across-registries-c1-and-c2)). A deployment that runs the
`FedMachine` itself (for example one `FedMachine` over a shared, totally ordered log) gets that
guarantee directly, on acyclic networks and monotone cycles alike: on a cycle, `normalizeCyclic`
performs the reset to bottom on every normalization, so nothing in
[the cycle section below](#cycles-ghosts-and-reset-epochs) applies.

---

## Projection deployments

Instead of running the `FedMachine`, each node can run only its own component (`FedMachine.Component`): it applies local events to its own `Machine`, its sources send `FedMachine.SharedProjection` messages, and it merges them with `Machine.MergeProjection` (or `MergeProjectionAfter`, which refuses stale or misaddressed projections) whenever they arrive. That is a different model from the one C1 and C2 certify. In the `FedMachine` the morphism repair runs after every event; on a node, a local event can write a shared variable and a second event can run before the next projection repairs it, and the next local event then reads a value no source would ever send. C1 does not cover that state, because C1 only looks at targets whose shared value is something a source can produce.

The formal counterexample (`fed_grs_c1_c2_insufficient` in [`FederationGRS.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationGRS.v)) has a target with a shared `flag` that its source always sets to `false`, and two events that each flip `flag` and fold the old `flag` into a local `acc`. C1 and C2 hold, and every `FedMachine` order converges. But node A merges between the two flips and ends with `acc = false`, while node B flips twice before merging and ends with `acc = true`: two nodes that see the same events but merge at different times end in different states.

**XU.** The projection model is proved convergent, once propagation completes, under the stronger condition **XU**: C1 at *every* valid target state, not only those whose shared component is an image, in an acyclic federation ([`FederationEvents.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/FederationEvents.v): `dist_interleavings_converge`, `propagation_flush`; `xu_implies_c1_c2`). Under XU (and each registry's own CC), every interleaving of local events and merges converges once propagation completes. `Build` does not require XU, since its guarantee is the `FedMachine`. It checks XU on every target (cost per target: events x valid target states x (images + 1) table lookups, no closure calls) and reports it without requiring it:

- `FedReport.ProjectionSafe`;
- a `FedReport.Checks` line (`distributed projection merging ...: certified (XU)` or `not certified: <witness or reason>`), also in `FedReport.ProjectionLine`;
- `FedReport.ProjectionWitnesses`, one `*ProjectionOrderError` per failing target.

**`RequireProjectionSafe` certifies distributed deployment.** `Federation.RequireProjectionSafe()` makes XU a build requirement: `Build` then returns the `*ProjectionOrderError` (target, event, state, and the two diverging results), or an error naming the structural reason, both wrapping `ErrProjectionNotCertified`. Call it if your deployment merges projections and you want `Build` to refuse a federation that is not certified for it. Like `AllowMonotoneCycles`, it belongs to the federation it is called on.

**Propagation order and delivery.** On an acyclic network the propagation steps can fire in any
fair order (every node keeps getting updated, no global schedule): after finitely many events, every
fair schedule settles at the `FedMachine` result, and a node at depth d is final after d + 1 rounds
in which every node updates (normalization-confluence `RobertFair.v`: `rb_events`, `rb_rounds`;
Robert's theorem in this model). No final flush is needed. The propagation half of the guarantee,
XU, holds for every delivery class of events (`xu_xurd`, `DistributedDelivery.v`). For the events
themselves:

- **Causal delivery:** only pairs that happens-before leaves unordered must commute on the
  `FedMachine` (`dist_causal_exact`). `Build`'s C2 checks declared pairs statically, so it can reject
  a causal deployment that would in fact converge (`tr_causal_instance`): the check is safe, not
  exact.
- **At-least-once delivery:** besides commutation, each event's *federated* step (the event, then the
  network's repair) must be idempotent where the event is first delivered (`dist_alo_exact`,
  `dist_causal_alo_exact`). `Report.NotIdempotent` checks each registry on its own, so in a
  projection deployment with redelivery it does not cover this: deduplicate events unless you have
  checked the federated step.

**What is not certified.** `ProjectionSafe` is false on a cyclic network (the theorem is for acyclic ones; [why that is required](#cycles-ghosts-and-reset-epochs)) and on a network with a multi-source target (`SharedProjection` sends one edge's `Map` image, not the resolver's merge the theorem covers). Like C1 and C2, XU is static: a witness is a valid state that a given deployment need not reach. M1 guarantees each merge keeps a valid node valid; it does not make the nodes converge.

**Ordering projections.** A projection carries no ordering by itself. A transport that can reorder
or redeliver projections should stamp `Projection.Version` and merge with `MergeProjectionAfter`,
which refuses a projection that is not newer than the last one applied from that edge (wrapping
`ErrStaleProjection`) or that names another target. On an acyclic federation with single-source
targets this is a proved guarantee (normalization-confluence `ProjectionChannels.v`), given two
deployment rules:

1. **Versions follow send order, stamped on the snapshot sent.** Each source stamps a strictly
   increasing version per edge on the projection it takes at that send. A retry must resend the
   same snapshot with its original version; restamping an old snapshot with a newer version lets a
   stale value win (`version_order_counterexample`).
2. **A send follows every source change.** If the last change to a source is never sent, the
   target stays stale once the channels drain, in either merge mode (`no_final_send_counterexample`).

With both rules, `ProjectionSafe` (XU) plus `Build`'s C2 make every deployment converge once its
channels drain, despite late, reordered and duplicated projections, with no outside flush
(`vsettle_xu_c2`); the drained state is what the current-value model reaches with the same events
followed by a flush (`vsettle_cv`), and the condition is exact (`vsettle_exact_cond`).
The guarantee holds at any depth, not only on two-level networks: on an acyclic federation whose
targets each have one source, the condition over versioned channels from a given start is exactly
the current-value model's (`vchan_single_exact` in `ProjectionChains.v`), so chains of projections
of any length are covered.
Plain `MergeProjection` over a channel that can reorder or redeliver converges only after an
outside flush (`chan_exact`): a stale projection delivered after a fresh one can leave the
deployment wrong at drain even under XU (`plain_stale_counterexample`). Per-edge FIFO channels
without redelivery avoid that. Multi-source targets and cyclic networks are not certified, as
before: on a monotone cycle the ghost survives versioned channels (`vchan_cyc_ghost`; see
[Cycles](#cycles-ghosts-and-reset-epochs)), and what remains open is normalization-confluence
[`REGIME-AUDIT.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md)
gap 21's residue: multi-source shapes, where versioned channels can reach a combination of
projections the current-value model never produces (`vchan_skip_counterexample`, four registries),
channels on cycles, and loss without redelivery.

---

## Cycles: ghosts and reset epochs

The "not certified" on a cyclic network is required, not conservative. On a monotone cycle the repair equations can have several fixed points, and the `FedMachine` picks the least one because `normalizeCyclic` resets every shared value to bottom before its Kleene iteration. Projection nodes have no such reset.

In the ladder picture of [monotone cycles](federation.md#escape-hatch-2-monotone-cycles), "the climb halts at the same top no matter which value you nudge first" is true *when the climb starts from the bottom rung*. The `FedMachine` makes sure it does: every time it normalizes a cycle, it first pushes every shared value back down to the bottom rung and then climbs. Separate nodes have no such referee. Each one just overwrites its shared values with whatever its sources currently say, and a value that is already high can stay high.

### A ghost

Here is the smallest loop where that bites. Two nodes, A and B. Each has a local `alarm`, and each has a shared `flag` that its partner fills in:

```
   A.flag = B.alarm OR B.flag
   B.flag = A.alarm OR A.flag
```

Both arrows are an `OR`, so they only ever push a flag up: the loop is monotone, and gsm accepts it. Now run these four steps on separate nodes:

1. A raises its alarm.
2. B hears from A: `B.flag = true OR false = true`.
3. A hears from B: `A.flag = false OR true = true`.
4. A clears its alarm.

Both flags are now `true`, and both alarms are `false`. Ask each node to re-check: `A.flag = B.alarm OR B.flag = false OR true = true`, and `B.flag = A.alarm OR A.flag = false OR true = true`. Nothing changes. The two flags hold each other up, with no alarm anywhere behind them. That is a **ghost**: a resting point of the loop, just not the lowest one, a quiescent state that no propagation leaves. The `FedMachine` with the same four events gives both flags `false`, because it resets them to the bottom rung before it climbs, and with both alarms clear the climb never leaves the bottom. No amount of further propagation brings the nodes there: from the ghost, every re-check says "still true."

The ghost survives every check gsm runs on the loop: cyclic C1 and C2, monotonicity, the cyclic form of XU (reachable XU), and convergence of the `FedMachine` itself (`dist_cyc_ghost` in [`DistributedCycles.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/DistributedCycles.v), which lands with [normalization-confluence PR #62](https://github.com/blackwell-systems/normalization-confluence/pull/62); see also `dist_schedule_dependence`, `dist_ring_livelock`, `q1_stuck`). That is why gsm reports projection merging on any cyclic network as not certified.

### What would certify a cyclic projection deployment

Two deployments are certified on a cycle, and gsm does not implement either as an API:

- **Reset epochs (the general fix).** At a barrier, every node resets its shared values to bottom; the nodes then propagate until quiescent (at most as many sweeps of every target as the `FedMachine`'s Kleene iteration takes), with no events applied inside the epoch. That is exactly what the `FedMachine` does on every step, done occasionally and collectively. After the epoch every node holds the `FedMachine` state for the events so far, whatever their interleaving. Exactly: agreement after a final epoch iff reachable cyclic XU (`epoch_agree_iff`), convergence iff that plus `FedMachine` convergence (`epoch_conv_iff`); gsm's per-target cyclic C1 and C2 suffice when their image set is widened by each slot's bottom value and the values events write (`lens_epoch`). The reset must be a barrier: if A resets while B still holds the ghost, A's next re-check reads B's `true` and the ghost is back in one propagation step (`dist_cyc_epoch_fix`). States between epochs are not certified.
- **No resets: flush and no ghost.** Given gsm's cyclic C1 and C2 over a value set covering the reachable shared values, a deployment without resets agrees with the `FedMachine` exactly when it **flushes** (propagation reaches a quiescent state) and **has no reachable ghost** (`lens_noreset_iff`, `lens_noreset_fair_iff`; the unconditional forms are `flush_agree_iff` and `fair_agree_iff` in [`DistributedCyclesExact.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/DistributedCyclesExact.v), [normalization-confluence PR #64](https://github.com/blackwell-systems/normalization-confluence/pull/64)). Both conditions are characterized: a fair run flushes iff it reaches a sound state, one where every shared value is at or below what its repair would write (`fair_flush_sound_iff`); there is no reachable ghost iff the start and every state right after an event are ghost-free (`noghost_event_iff`), and when every reachable state is sound that is the same as staying at or below the least fixed point, which is checkable per event (`noghost_soundr_iff`, `lowr_post_iff`). In practice there are two cheap sufficient checks and a fallback:
  - **Inflationary events (per event).** Every event raises locals in an order the morphisms are monotone in and never raises a shared value, for example an alarm that can be raised but never cleared. From a normalized start (a `FedMachine` normal form is sound and low, `nc_sound_low`), that keeps every reachable state sound and low, so the deployment flushes and has no ghost (`infl_evsound`, `evlow_lowr`). Cost: events x valid component states, plus a monotonicity check of the morphisms in their source locals; no global enumeration.
  - **A unique fixed point (global).** The repair has one fixed point for every assignment of the locals, so no ghost can exist (`uniq_agree`; more generally an invariant whose states have one-fixed-point locals or are low, `unique_or_low_noghost`). Flushing still needs its own argument: uniqueness alone does not make propagation quiesce (`flip_noflush`).
  - **Otherwise, a global search** over the reachable global states for a non-flushing run or a ghost, with a cost like any reachability check over the product of the registries' state spaces.

  A clear event on an unpinned feedback loop, the flag loop above (the shape of the `alarms` federation in gsm's cycle tests), cannot be certified without resets at all (`ghost_exact`): "clear the alarm" is a step down with a feedback loop to remember the old value. It needs epochs. If only every run agreeing with every other run matters, and not agreement with the `FedMachine`, that is also exact: runs may all settle on the same ghost, and the condition is that each reachable state flushes to one quiescent state, events and flushes commute up to a common flush, and flush-then-apply is order-independent (normalization-confluence `conv_quiet_exact`, which closed [`REGIME-AUDIT.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md) gap 14). gsm certifies agreement with the `FedMachine`, which is that convergence plus no ghost (`agree_conv_noghost`). The full statements, with the necessity instance for each condition, are in normalization-confluence's [`coq/docs/distributed.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/docs/distributed.md#the-no-reset-model-on-monotone-cycles-exactly-distributedcyclesexactv).

A deployment that runs the `FedMachine` itself (for example one `FedMachine` over a shared, totally ordered log) is unaffected: it performs the reset on every normalization. The formal model and the full statements are in [Theory §11.4](theory.md#114-multi-registry-systems).

---

## Changing a running system

`Build` checks one configuration. A deploy changes it while events are in flight: a limit goes
from 5 to 10, a variable is added or rescaled, an event is renamed. `CheckMigration` takes the old
registry, the new one, a migration of the state and a translation of the events, and classifies
the change before you ship it:

- **Safe online**: switch whenever you like, with events of the old configuration still in flight.
- **Safe behind a barrier**: drain first (apply every event submitted under the old rules), then
  switch. The report carries a witness: two runs, one switching with an event in flight, that end
  in different states.
- **Unsafe**: even a drained switch diverges. The witness is two such runs.
- **Unknown**: only when the check's search stops at its size limit before it is exhaustive
  (below). Otherwise one of the three outcomes above is always decided.

<!-- gocheck: run -->
```go
cart := func(name string, limit int) (*gsm.Registry, gsm.Var) {
    r := gsm.NewRegistry(name)
    items := r.Int("items", 0, limit+1)
    r.Invariant("cap").Watches(items).
        Holds(func(s gsm.State) bool { return s.GetInt(items) <= limit }).
        Repair(func(s gsm.State) gsm.State { return s.SetInt(items, limit) }).Add()
    r.Event("add").Writes(items).
        Apply(func(s gsm.State) gsm.State { return s.SetInt(items, s.GetInt(items)+1) }).Add()
    return r, items
}
v1, items1 := cart("cart v1", 5)
v2, items2 := cart("cart v2", 10)

// The migration builds a state of v2 (blank is its zero state) from a state of v1. Events of
// v1 still in flight become the v2 event of the same name (pass a map to rename them).
migrate := func(old, blank gsm.State) gsm.State { return blank.SetInt(items2, old.GetInt(items1)) }
report, err := gsm.CheckMigration(v1, v2, migrate, nil)
if err != nil {
    panic(err)
}
fmt.Print(report)
if report.Outcome != gsm.MigrationSafeBehindBarrier {
    panic("raising the cap with an add in flight should need a barrier")
}
```

The report:

```
Migration cart v1 -> cart v2: SAFE BEHIND A BARRIER (an event in flight at the switch does not commute with it; drain, then switch)
  ...
  Online (switch at any time, events in flight):
    cart v2 converges from the migrated start [PermB-start]: PASS (0 pairs of cart v2 events at the 11 states cart v2 reaches from the migrated start)
    in-flight events commute with the switch [DS1]: FAIL (at {items=5}, add applied under cart v1 then the switch gives {items=5}; the switch then add under cart v2 gives {items=6})
  Behind a barrier (drain, then switch):
    cart v2 converges from every migrated reachable state [PermB-every]: PASS (...)
    cart v1 converges from the start [PermA]: PASS (...)
    the migration is injective on the reachable states [Faithful]: yes (on the reachable states of cart v1)
  Why not online (DS1), from {items=0}:
    run 1: add, add, add, add, add, add under cart v1, switch, nothing under cart v2: {items=5}
    run 2: add, add, add, add, add under cart v1, switch with add in flight, add under cart v2: {items=6}
  Theorems: det_live_exact, det_barrier_faithful, run_tequiv, perm_tequiv_total (...)
```

Six adds are submitted. A replica that applies all six under v1 clamps the sixth at 5 and keeps 5
after the switch; a replica that switches with the sixth still in flight applies it under v2 and
reaches 6. Drain first and every replica agrees.

**What your deployment must do.** The outcome is about this model of the switch, which is gsm's
runtime with one change of configuration in each replica's life:

1. **Migrate every replica's state with the same migration, then `Normalize` it under the new
   machine** (`newMachine.Normalize(migrate(old, newMachine.NewState()))`). The migration may
   produce a state the new invariants reject; the normalization is part of the switch.
2. **Apply an old event that arrives after the switch as its translation**, the event of the same
   name in the new registry or the one the map names.
3. **Apply each event once, by one configuration.** Events may arrive in any order (free delivery).
   Under another delivery class, deliver as that class says (below).
4. **For a barrier, drain before switching**: every replica applies every event submitted under the
   old configuration, then switches.

**What each outcome rests on.** `Machine.Apply` repairs before it returns, so a switch never
migrates a state between an event and its repair. In that model (normalization-confluence
`coq/Reconfiguration.v`, section `Det`, with each step being `Apply`):

- Safe online is exact, both ways (`det_live_exact`): the new registry converges from the migrated
  start (every pair of its events commutes at every state it reaches from there: `run_tequiv`,
  `perm_tequiv_total`), and every in-flight event commutes with the switch (DS1: at every state the
  old registry reaches, migrating after the event is applying its translation after migrating).
  The old registry's own convergence is not needed: a migration that forgets where the old rules
  diverge is still safe online (`forgetful_migration`).
- Safe behind a barrier is exact: the new registry converges from every migrated reachable state,
  and two orders of the same old events never migrate to different states. When the old registry
  converges from the start, that second part holds outright (`det_barrier_exact`, and
  `det_barrier_faithful` when the migration is injective on the reachable states). When it
  diverges on its own and the migration is not injective, gsm searches the pair closure: the
  pairs of states reached by applying two old events in both orders at a reachable state, then
  the same events to both. The barrier is safe iff the migration sends both states of every pair
  to one state (`det_barrier_closure_exact`, `amodm_closure_exact`, `gsm_closure_exact`), so a
  search exhausted with no witness certifies it (`amodm_witness_exact`): the migration absorbs the
  old rules' divergence.
- Unsafe is certified by its witness, two barrier runs that diverge, replayed before the report is
  returned.
- Unknown means only that the closure search stopped at its size limit (2²⁰ pairs of states)
  before it was exhaustive (`SearchStopped` says so); gsm then claims neither outcome. On finite
  instances the outcome is otherwise always decided (`det_classify_complete`,
  [`REGIME-AUDIT.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md)
  gap 20, residue (c), closed in this model).

**Delivery classes.** By default every event is applied exactly once, in any order. Check the
change under the class your deployment provides:

- **Declared independence.** When either registry declares `Independent` pairs, only declared
  pairs are reordered and every other pair reaches every replica in one fixed order. The check
  then covers the declared pairs only (`live_declared_exact`, `barrier_declared_exact`): the old
  registry's pairs (as in-flight events), the new registry's pairs, and an in-flight old event
  with a new event when the new registry declares the translation with that event.
  `MigrationInFlightIndependent(oldEvent, newEvent)` declares more such cross pairs, for a
  deployment where an in-flight event can arrive after a new one submitted later.
- **At-least-once** (`MigrationDeliveryClass(gsm.MigrationAtLeastOnce)`): an event may be
  delivered more than once, a redelivery possibly arriving after the switch
  (`live_free_alo_exact`, `barrier_free_alo_exact`). The new registry's events must also absorb a
  redelivery (`Idem-start`, `Idem-every`), an old event applied before a barrier switch and
  redelivered after it must change nothing (`AbsorbS`), and runs of the old registry with the same
  set of events must migrate to one state (`AmodA`). Combined with `Independent` pairs it is not
  covered, so refused.

A publishing flow adds an `unpublish` event. The new registry declares no pair, so a replica
applies `publish` and `unpublish` in the order they were submitted, in-flight ones included: safe
online. If an in-flight `publish` can arrive after an `unpublish` submitted later, declare that
cross pair, and the change needs a barrier:

<!-- gocheck: run -->
```go
v1 := gsm.NewRegistry("docs v1")
pub1 := v1.Bool("published")
v1.Event("publish").Writes(pub1).Apply(func(s gsm.State) gsm.State { return s.SetBool(pub1, true) }).Add()
v2 := gsm.NewRegistry("docs v2")
pub2 := v2.Bool("published")
v2.Event("publish").Writes(pub2).Apply(func(s gsm.State) gsm.State { return s.SetBool(pub2, true) }).Add()
v2.Event("unpublish").Writes(pub2).Apply(func(s gsm.State) gsm.State { return s.SetBool(pub2, false) }).Add()
v2.OnlyDeclaredPairs() // every pair delivered in submission order
migrate := func(old, blank gsm.State) gsm.State { return blank.SetBool(pub2, old.GetBool(pub1)) }

ordered, err := gsm.CheckMigration(v1, v2, migrate, nil)
if err != nil {
    panic(err)
}
crossed, err := gsm.CheckMigration(v1, v2, migrate, nil, gsm.MigrationInFlightIndependent("publish", "unpublish"))
if err != nil {
    panic(err)
}
fmt.Println(ordered.Outcome, "/", crossed.Outcome, crossed.Failed)
fmt.Print(crossed.LiveWitness.Run2.InFlight, " in flight, ", crossed.LiveWitness.Run2.After, "\n")
if ordered.Outcome != gsm.MigrationSafeOnline || crossed.Outcome != gsm.MigrationSafeBehindBarrier {
    panic("want online in submission order, a barrier with the cross pair declared")
}
```

```
SAFE ONLINE / SAFE BEHIND A BARRIER PermB-start
[publish] in flight, [unpublish publish]
```

A migration that starts a new epoch of read receipts clears every `read` flag. Exactly once, an
in-flight `read` is applied after the reset, so the change needs a barrier. At least once, even a
drained switch diverges: a `read` applied before the switch and redelivered after it sets the
cleared flag again, and the report names that straddling duplicate (`AbsorbS`):

<!-- gocheck: run -->
```go
receipts := func(name string) (*gsm.Registry, gsm.Var) {
    r := gsm.NewRegistry(name)
    read := r.Bool("read")
    r.Event("read").Writes(read).Apply(func(s gsm.State) gsm.State { return s.SetBool(read, true) }).Add()
    return r, read
}
v1, _ := receipts("receipts v1")
v2, _ := receipts("receipts v2")
newEpoch := func(old, blank gsm.State) gsm.State { return blank }

once, err := gsm.CheckMigration(v1, v2, newEpoch, nil)
if err != nil {
    panic(err)
}
atLeastOnce, err := gsm.CheckMigration(v1, v2, newEpoch, nil, gsm.MigrationDeliveryClass(gsm.MigrationAtLeastOnce))
if err != nil {
    panic(err)
}
fmt.Println(once.Outcome, "/", atLeastOnce.Outcome, atLeastOnce.Failed)
w := atLeastOnce.BarrierWitness
fmt.Println(w.Run2.Before, "then", w.Run2.InFlight, "redelivered:", w.Result1, "versus", w.Result2)
if once.Outcome != gsm.MigrationSafeBehindBarrier || atLeastOnce.Failed != gsm.MigrationAbsorbS {
    panic("want a barrier exactly once, unsafe by AbsorbS at least once")
}
```

```
SAFE BEHIND A BARRIER / UNSAFE AbsorbS
[read] then [read] redelivered: {read=false} versus {read=true}
```

Causal delivery across the switch is not supported: gsm has no happens-before declaration
(normalization-confluence `live_causal_exact` and `barrier_causal_exact` would back it).

The registries need not pass `Build`: `CheckMigration` checks the states runs reach from the start
(the old registry's zero state, or the states you pass with `MigrationFrom`, such as a running
system's current state), not every state. Its cost is those states, at most 2²⁰ on each side; a
larger instance is an error.

**Not covered.** `CheckMigration` refuses a registry declared with `Abstract`, and `Independent`
pairs under at-least-once delivery (declared pairs combined with at-least-once delivery across the
switch are not covered by the theorems; causal delivery is not supported, above: the rest of gap
20, residue (b)). It takes single registries: a topology change of a federation is covered
by the theory under `FedMachine` semantics (`fed_live_exact`, `fed_barrier_exact`; adding an edge
an in-flight event reads is unsafe online, `late_edge`) but not implemented, and a collection
migrated through its template and a projection deployment, where propagation is itself in flight
(gap 20, residue (a)), are not covered. A runtime that can switch between an event and its repair
is not gsm's, and is the theory's rewriting model (`live_exact`), where the change above fails
condition (S2) at the raw 6 instead (`cap_raise`). The formal account is
[Theory §11.11](theory.md#1111-changing-a-running-system-migration).
