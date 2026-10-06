# Deployment

`Build`'s guarantee is about a model: which events arrive, how often, in what order, and where
the repair runs. Some of what that model assumes is about your runtime, not your rules, so `Build`
cannot enforce it. It measures what it can and names the rest in the report. This page lists each
obligation, what the report says about it, and what to do.

- [Delivery](#delivery): each event once, and causal order for undeclared pairs.
- [Running a federation as one machine](#running-a-federation-as-one-machine): the `FedMachine`, for example over a shared log.
- [Projection deployments](#projection-deployments): one node per registry, merging projections.
- [Cycles: ghosts and reset epochs](#cycles-ghosts-and-reset-epochs): why projection deployments on a cycle are not certified, and what would certify them.

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
Plain `MergeProjection` over a channel that can reorder or redeliver converges only after an
outside flush (`chan_exact`): a stale projection delivered after a fresh one can leave the
deployment wrong at drain even under XU (`plain_stale_counterexample`). Per-edge FIFO channels
without redelivery avoid that. Multi-source targets and cyclic networks are not certified, as
before: on a monotone cycle the ghost survives versioned channels (`vchan_cyc_ghost`; see
[Cycles](#cycles-ghosts-and-reset-epochs)), and what remains open is normalization-confluence
[`REGIME-AUDIT.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md)
gap 21's residue (longer chains, channels on cycles, loss without redelivery).

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
