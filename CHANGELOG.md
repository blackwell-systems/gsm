# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **XU check for distributed projection merging.** `Federation.Build` now checks XU
  (normalization-confluence `coq/FederationEvents.v`) on every target of an acyclic federation:
  `ow(ρ_B(e(ow(b, v))), v) = ow(ρ_B(e(b)), v)` for every target event `e`, every image `v` of a
  valid source state (or source combination), and every valid target state `b`, not only those
  whose shared component is an image (that restriction is C1). XU, with each component's CC, is
  the hypothesis of `dist_interleavings_converge`: nodes that each run their own component
  `Machine`, apply local events, and merge projections (`SharedProjection`, `MergeProjection`,
  `MergeProjectionAfter`) whenever they arrive converge, once propagation completes, to the
  `FedMachine` run of the same events. C1 and C2 alone do not give that
  (`fed_grs_c1_c2_insufficient`). Cost per target: events x valid target states x (images + 1)
  table lookups, reusing the images C1 computes (no extra closure calls).
- **`FedReport.ProjectionSafe`, `ProjectionLine`, `ProjectionWitnesses`.** The result is reported,
  not required: `Build`'s guarantee remains the `FedMachine` model, where C1 and C2 are exact.
  `FedReport.Checks` ends with `distributed projection merging (...): certified (XU)` or `not
  certified: <witnesses and reasons>`. Each witness is a `*ProjectionOrderError` naming the
  target, event, valid state, source, and the merge-first and event-first results. A cyclic
  network, and a network with a multi-source target (its `SharedProjection` is one edge's `Map`
  image, not the resolver's merge), are reported as not certified.
- **`Federation.RequireProjectionSafe()`.** Opt-in that makes an uncertified federation a build
  error: the first `*ProjectionOrderError`, or an error naming the structural reason; both wrap
  the new `ErrProjectionNotCertified`. Like `AllowMonotoneCycles`, it belongs to the federation it
  is called on.

### Changed

- **Cyclic projection deployments without resets: the exact condition.** Docs only; no behavior
  change, and the cyclic "not certified" reason string is unchanged (gsm still implements neither
  route). The doc comments of `Machine.MergeProjection`, `MergeProjectionAfter`,
  `FedMachine.SharedProjection` and `Federation.RequireProjectionSafe`, `docs/deployment.md`, the
  ghost paragraph of `docs/federation.md` and `docs/theory.md` 11.4 now state the no-reset route as
  exact, per normalization-confluence `coq/DistributedCyclesExact.v` (PR #64): given cyclic C1 and
  C2 over a covering value set, a deployment without resets agrees with the `FedMachine` exactly
  when it flushes and has no reachable ghost (`lens_noreset_iff`, `lens_noreset_fair_iff`;
  `flush_agree_iff`, `fair_agree_iff`). Flushing is reaching a sound state
  (`fair_flush_sound_iff`); no reachable ghost is `noghost_event_iff`, and with every reachable
  state sound it is staying at or below the least fixed point, checkable per event
  (`noghost_soundr_iff`, `lowr_post_iff`). Inflationary events (per event: `infl_evsound`,
  `evlow_lowr`) and a unique fixed point (global: `uniq_agree`, `unique_or_low_noghost`) are the
  cheap sufficient checks, and otherwise it is a global search; barrier reset epochs remain the
  general fix. A clear on an unpinned feedback loop cannot be certified without resets
  (`ghost_exact`). Convergence among runs alone, without agreement with the `FedMachine`, is open
  (normalization-confluence gap 14) and does not affect gsm, which would certify agreement.
- **Projection docs.** `Machine.MergeProjection`, `MergeProjectionAfter`,
  `FedMachine.SharedProjection` and `FedMachine.Component` now state that projection-based
  deployment is proved convergent under XU (`dist_interleavings_converge`) and that `Build`'s C1
  and C2 certify the `FedMachine` model only, pointing to `FedReport.ProjectionSafe` and
  `RequireProjectionSafe`. `MergeProjection`'s claim that merging "preserves local validity by the
  M1 guarantee, so no re-normalization is required" is narrowed to what M1 gives: a valid state
  stays valid when the projection is the image of a valid source state along a single-source
  morphism (`Build` does not check an edge into a multi-source target against M1), and validity is
  not convergence. README (Federated Registries), FEDERATION-CONCEPTS.md and THEORY.md (11.4,
  replacing the stale "partial synchronization: future work" note) describe the two models.
- **README framing (#32, #33).** The README opens with the theory's claim as an exact regime map
  (in every regime, a machine-checked exact condition, a hardness result showing no efficient one
  exists, or a gap stated in the open, linked to normalization-confluence's `REGIME-AUDIT.md`) and
  names the idea, convergence by compensation. The federation paragraph states the split between
  repair (a unique federated normal form) and event order (the C1 and C2 checks `Build` runs).
- **Cyclic projection deployments: why "not certified", and what would certify them.** No
  behavior change; the cyclic reason in `FedReport.ProjectionLine` (and the
  `RequireProjectionSafe` build error) now explains that, on a monotone cycle, nodes that merge
  projections without a shared reset can settle on a larger fixed point than the `FedMachine`'s
  least one (`dist_cyc_ghost` in normalization-confluence `coq/DistributedCycles.v`, PR #62), and
  points to `RequireProjectionSafe`. Its doc comment, and those of `MergeProjection`,
  `MergeProjectionAfter` and `SharedProjection`, give the two certified options: barrier reset
  epochs (`epoch_agree_iff`, `epoch_conv_iff`, `lens_epoch`; a staggered reset is not enough,
  `dist_cyc_epoch_fix`), or, without resets, inflationary events (`evlow_lowr`, `infl_evlow`) or a
  unique fixed point for every locals assignment (`uniq_agree`). gsm does not implement epochs. A
  deployment that runs the `FedMachine` is unaffected (`normalizeCyclic` resets the shared values
  to bottom on every normalization). README (Federated Registries), FEDERATION-CONCEPTS.md (a new
  "Ghosts" section with the flag-cycle example) and THEORY.md 11.4 cover the same ground.
- **Documentation restructured.** No behavior change. The README (921 lines) is now a short
  front page: what gsm is, the order example and its report, a table of what `Build` guarantees in
  each setting, when to use it, install, a documentation map, citation and license. Its content
  moved, with every caveat and theorem citation, into `docs/`: `getting-started.md` (tutorial, API
  basics, combinators, independence declarations), `concepts.md` (CONCEPTS.md and EXPLAINER.md
  merged), `federation.md` (the README's federation section and FEDERATION-CONCEPTS.md merged),
  `deployment.md` (delivery obligations, shared logs, projection deployments, ghosts and reset
  epochs), `verification.md` (the report, the extracted oracles, assurance levels, certificates,
  compositional verification, synthesis, performance, limitations), `reference.md` (`Report` and
  `FedReport` fields, errors, build options, the export format) and `theory.md` (THEORY.md, plus
  the paper-to-code map). ARCHITECTURE.md, CERTIFICATE-DESIGN.md and HOLONOMY-COORDINATION-DESIGN.md
  moved unchanged to `docs/design/`. The old top-level files are stubs that link to their new
  location. The README's sample report now shows the `Rules oracle:` line `Build` prints for a
  closure machine. `TestDocSnippets` checks the new pages (EXPLAINER's blocks, previously
  unchecked, are merged into checked pages or replaced by links to the README example).

## [0.13.0] - 2026-10-04

A trust-point release. Every condition a developer could get wrong in the v0.12.0 trust audit is
now a check gsm runs, a default it picks, or an obligation the report names and explains: event
order across federated registries (C1) and between two events of one target under the morphism
repair (C2), declared-only pairs, duplicate delivery, invalid start states, closure footprints in
`BuildCompositional`, monotone cycles over the states the Kleene iteration visits, certificate
tables, projection staleness, resolver conflicts and the cycle opt-in scope. And a certified
sub-federation now executes the verified tables it was certified with, rather than the closures
they were checked against. Event order on monotone cycles is now certified by the same C1 and C2
checks, backed by a mechanized theorem. Merged from #19, #20, #21, #22, #23, #24, #26, #27, #28
and #29.

### Breaking / behavior changes (upgrading from v0.12.0)

No exported identifier is removed. One signature gains variadic options (`BuildCompositional`),
which keeps existing calls compiling. What can start failing, or behave differently, and what to do:

- **BREAKING: `BuildCompositional` rejects closure rules by default (#22).** A registry with any
  closure rule (`Holds`, `Repair`, `Apply` or `Guard` written as a Go function) now fails with an
  error naming the first one. A closure's footprint can only be tested by perturbation, which
  misses a joint dependence on three or more outside variables. Write the rules with combinators
  (no option needed, assurance unchanged), use `Build` (exact for any rule), or pass
  `TrustClosureFootprints()`: `r.BuildCompositional(gsm.TrustClosureFootprints())`. With the option
  the report's assurance is the new `AssuranceOracleComponentsTested` instead of
  `AssuranceOracleComponents`; update code that compares it.
- **Federations whose events race a morphism are rejected (#19, #26).** `Federation.Build`,
  `BuildCoordinated` and `Certify` now check event order across the network and return an error
  for a federation that may diverge:
  - C1, cross-registry CC (#19): a target event that does not commute with a change of its
    shared component a source can cause (typically its guard, effect or the target's repair reads
    a `Shared()` variable) returns a `*CrossOrderError` naming the morphism, the source normal
    forms, the target event and state, and both results.
  - C2, repaired CC (#26): two target events that commute on the target alone but not with the
    morphism repair between them (one writes a shared variable the repair overwrites before the
    other reads it) return a `*SameTargetOrderError` naming both events, the target, the source
    and target states, and both results. Only pairs the target's own CC covers are checked (every
    pair, or the declared `Independent` pairs).
  Both are static checks over every valid source state, so the witness may be a state a given
  run never reaches; the message says the federation *may* diverge. To fix one, record the fact
  locally and let an invariant derive the outcome, or move the dependency into the morphism.
- **The cycle opt-in no longer carries over (#23).** `Embed` and `EmbedCertified` do not copy a
  sub-federation's `AllowMonotoneCycles` to the parent. A parent with a cycle must call
  `AllowMonotoneCycles` itself; `Build`'s cycle error names the opted-in sub.
- **A second resolver for a target panics (#23)** in `Resolve`, `Embed` and `EmbedCertified`,
  instead of silently replacing the first. Declare one resolver per target.
- **`AllowMonotoneCycles` checks more states (#20).** Monotonicity, source-determinacy, the
  write mask and image validity are checked over every state the iteration can visit (each valid
  local part with every in-domain shared value), not only valid states, so some cyclic federations
  that built under v0.12.0 are now rejected. They could depend on morphism declaration order.
- **Certificates are checked harder (#21).** `Certificate.Verify` and `EmbedCertified` reject
  incomplete or forged tables (a missing or duplicate row, an invalid source id, a source that is
  not a provided component, two tables for one target, non-monotone tables on a cyclic `Monotone`
  certificate), and `EmbedCertified` rejects an internal closure that does not match its table on
  every valid source and target state. Re-issue with `Certify` if one is refused.
- **`EmbedCertified` refuses wiring its certificate does not describe (#27):** an outer morphism
  between two components of the same certified sub, and an outer `Resolve` for a target whose
  sources are all inside the sub. Both used to build and were left unverified. Put the wiring in
  the sub and re-certify, or use `Embed`.
- **A certified sub's closures no longer run at run time (#27).** `Apply`, `Normalize`, `IsValid`
  and `SharedProjection` repair a certified sub's internal targets by a lookup in the certificate's
  verified table, so a side effect in its `Map` or `Resolver` (logging, a metric, a counter) does
  not happen. Results are unchanged on every certified state. Closures still run on a cyclic
  network, on a certified target that also has an outer writer, and for `SharedProjection` along a
  morphism into a resolved target; `FedReport.Runtime` says which.
- **A foreign `FedState` panics on a certified sub (#27).** A component state outside the certified
  domain (valid source and target states) makes `Normalize` and `Apply` panic, naming the
  component, the state and the sub; `IsValid` returns false and `SharedProjection` an error. Before,
  a closure ran on it. Only states from this machine's `NewState`, `Apply` or `Normalize` are in
  the domain.
- **`Int(min, max)` panics at declaration (#24)** when the range has more values than an int can
  count (for example `Int(0, math.MaxInt)`), naming the variable. Narrow the range.
- **`Machine.Apply` normalizes an invalid input first (#24).** `Apply(s, e)` now equals
  `Apply(Normalize(s), e)` for a state that violates an invariant (restored from storage, or built
  with `Set`); the zero state is applied as it is. Code that relied on applying an event to an
  invalid state as is sees the normalized result.
- **Runtime panics replace silent misbehavior (#20, #21, #22).** A lazy `BuildCompositional`
  machine panics when a repair chain exceeds the bound `Build` measured; `FedMachine` panics when a
  `Map` or `Resolver` writes a non-shared variable, or when the monotone-cycle iteration reaches its
  cap or an invalid fixed point. Each means a closure behaved differently at run time than when it
  was checked (impure or nondeterministic).
- **`BuildCompositional` fails fast on huge domains (#22):** a variable whose domain does not fit
  its bit field, or a closure perturbation test above 2^28 calls, is rejected before any check
  (it used to hang, for example on `Int(0, math.MaxInt)`).
- **A foreign `Var` in `Writes` or `Watches` is an error (#24)** naming the rule and the variable,
  on every build path, instead of an index panic (or silent acceptance when the index was in
  range).
- **`Export` writes format version 2 (#23).** The JSON `version` field is now `2`, and
  `verification` gains `pairs` (the event pairs CC was checked for, by name) and `all_pairs`.
  Version 2 only adds fields; readers that reject an unknown version must accept 2.
- **`DiagnoseCycle` no longer reports an obstruction from one seed (#29).** When the zero seed
  does not settle, it now tests every seed of the cycle's finite fiber. `Converges` keeps its
  meaning (the zero seed settles); use the new `Obstructed()` for "no consistent state exists".
  Code that read `Converges == false` as a proven contradiction should switch to `Obstructed()`.
- **`BuildCoordinated` rejects a `CoordinationPoint.Authority` other than `Dst` (#29).** An empty
  `Authority` is still accepted.
- **Report text changed.** New `Report` lines: undeclared pairs and "GUARANTEED under causal
  delivery", "Delivery: exactly once", "Saturation:", "Coordinated input"; `Synthesis` opens with
  `REPAIR SYNTHESIZED` when it substituted the repairs; `FedReport` prints its `Assurance`, the
  federation checks (now including C1 and C2) and the certified-runtime lines. Code that parses
  report text should switch to the fields.

### Added
- **Repaired CC (C2) for federations (#26).** `Federation.Build` checks that every pair of target
  events the target's own CC covers still commutes when the morphism or resolver repair runs after
  each of them, over every image its sources produce and every consistent target state. New
  `SameTargetOrderError` (`Federation`, `Morphism`, `Target`, `First`, `Second`, `State`,
  `Source`, `FirstThenSecond`, `SecondThenFirst`). `FedReport.Checks` lists `cross-registry CC
  (C1)` and `repaired CC (C2)`. Static C1 + C2 are sufficient for every interleaving of independent
  events to converge in an acyclic federation (normalization-confluence `FederationEvents.v`,
  `fed_events_commute`; `FederationEventsConverse.v`, `static_c1_c2_gc`).
- **Cross-registry CC (C1) (#19).** `Federation.Build` checks that every target event commutes
  with every change of the target's shared component that its sources can cause, along chains and
  for resolved targets. New `CrossOrderError`.
- **Certified execution (#27).** A `FedMachine` with `EmbedCertified` subs runs the certificate's
  verified tables for their internal morphisms and resolvers. `Build` deep-copies the tables,
  checks their digest against the certificate and against the live closures, re-checks the copy and
  compiles that copy, so a `Certificate` changed during or after `Build` cannot reach the machine.
  New `FedReport.Runtime`, printed by `String()`, states per certified sub whether its internal
  targets execute tables, and if not all of them, how many and why.
- **Undeclared pairs are checked and reported (#24).** In declared-only mode (`Independent`),
  `Build` checks every undeclared pair on the step tables without failing on it, and lists each
  pair that does not commute in `Report.CausalOrderRequired` with a witness. New
  `Report.PairsUndeclared`. The convergence line reads "GUARANTEED under causal delivery of the N
  undeclared pair(s) above".
- **`Report.NotIdempotent` (#24):** events whose second application changes the state, printed as
  "Delivery: exactly once for ...". These need deduplication under at-least-once delivery. The
  multiset assumption is documented at `Machine.Apply`.
- **`Report.Saturations` (#24):** rules whose write was clamped into a variable's range, printed
  as a "Saturation:" line.
- **`Report.Coordinated` (#24):** `BuildCoordinated` records each removed edge on its target
  component's report, printed as a "Coordinated input" obligation.
- **`TrustClosureFootprints()` and `AssuranceOracleComponentsTested` (#22).**
  `BuildCompositional` takes variadic `CompositionalOption`s; existing calls compile unchanged.
- **Versioned projections (#21).** `Projection.Version`, `Machine.MergeProjectionAfter(s, p, last)`
  and `ErrStaleProjection`: a stale, unversioned or misaddressed projection is refused.
  `MergeProjection` is unchanged.
- **`FedReport.Assurance` and `FedReport.Checks` (#23)** state which federation-level checks ran
  and that they are checked by gsm's Go code, not by the verified oracle.
- **`Synthesis.Substituted` and `Synthesis.BuildError` (#23).** `String()` opens with
  `REPAIR SYNTHESIZED` when `BuildOrSynthesize` returned a machine that does not use the declared
  repairs.
- **Export format version 2 (#23):** `verification.pairs` and `verification.all_pairs`.

- **`CycleDiagnostic.AllSeeds`, `Seeds`, `SectionExists`, `Section` and `Obstructed()` (#29).**
  A section exists iff some seed reaches a fixed point (`CohomologyGeneral.v`,
  `thm_obstruction_reachable`, `c15_exact_refuter`). Above 2^20 seed combinations the search is
  skipped and the diagnostic says only that the zero seed does not settle.
- **`CoordinationPoint.Authority` (#29):** the registry each coordinated cycle is driven from. The
  normal form is unique given the plan, but a different cut picks a different root and a different
  normal form (`CoordinatedCycles.v`, `root_choice_matters`); the report's "Coordinated input"
  line names it.
- **Event order on monotone cycles is certified (#28).** On `AllowMonotoneCycles` networks, the
  per-target C1 and C2 checks already compared locals after the final overwrite over each target's
  full image set, which is exactly the hypothesis of `cyc_check_gc_lfp`
  (`FederationEventsCyclesCheck.v`); `FedReport.Checks` now says event order on the cycle is
  certified. A multi-source target is checked against its resolver's joint image, so the result
  does not depend on M1 or on combining per-edge checks (`FederationEventsCyclesMulti.v`). New
  regression tests: the latch (fails C1), the shared-write instance (builds), the Swap/Audit cycle
  (fails C2), and a resolver `x + y` whose per-edge images miss the value 2.

### Changed
- **BREAKING: `BuildCompositional` rejects closure rules unless `TrustClosureFootprints()` is
  passed (#22).** Combinator-only machines need no option.
- **`Export` format version 1 -> 2 (#23)**, additive.
- **`Machine.Apply` normalizes an input that violates an invariant (#24)** on table and lazy
  machines; the zero state keeps its table entry.
- **`Embed` and `EmbedCertified` do not propagate `AllowMonotoneCycles` (#23)**, and panic on a
  second resolver for a target, as `Resolve` does. `EmbedCertified` routes through `Embed`'s
  helpers.
- **`AllowMonotoneCycles` verification (#20)** runs over the visited states (each valid local part
  with every in-domain shared value), in the new `verifyMonotoneVisited`, after the existing
  valid-state check.
- **`EmbedCertified` binds internal closures (#21):** each internal Map or Resolver is run over
  every valid source (combination) and valid target state, as `Embed` does. Since #27 the runtime
  does not depend on this binding; it is kept as an early mismatch diagnostic and so that the
  build-time checks that evaluate closures (C1, C2, the monotone checks) speak for the tables.
- **`Certificate.Verify` table checks (#21):** every table source is a provided component, one
  valid source id per source per row, no duplicate rows, one row per valid source state
  (combination), at most one table per target, and monotone tables for a cyclic `Monotone`
  certificate. Docs state the digest binds integrity, not authorship.
- **`BuildCompositional` fails fast on huge domains (#22)** (see above).
- **Docs.** The README, THEORY, CONCEPTS, FEDERATION-CONCEPTS, ARCHITECTURE, EXPLAINER,
  CERTIFICATE-DESIGN and HOLONOMY-COORDINATION-DESIGN documents match this release: C1 and C2 are
  both checked and statically sufficient (the exact condition quantifies over reachable states);
  the delivery obligations (which events need deduplication, and that a redelivered copy must not
  overtake a causal successor of its event); op-based CRDTs as exactly the compensation-free
  fragment under causal delivery; convergence for any well-founded repair potential; the
  mechanized soundness of the holonomy-minimal coordination plan, and that such a plan must name
  its authority root. Two examples that did not converge as written were corrected (EXPLAINER's
  order machine and a CONCEPTS fix suggestion), and stale claims were removed (EXPLAINER called
  `CoordinationPlan` minimal; CERTIFICATE-DESIGN described a fallback to full re-verification that
  `Build` does not do). `BuildCompositional`'s cross-component claim now cites
  `calc_components_cc1_valid` (sound at valid states, which is all `Apply` reaches) and notes that
  the refuted invariant-footprint theorem is not used (#29). Cycle docs cite `cyc_check_gc_lfp`
  instead of saying C1 and C2 are unproved on cycles (#28).

### Fixed
- **`DiagnoseCycle` called a cycle obstructed when only the zero seed failed to settle (#29).** On
  the loop "copy, then swap 0 and 1" over {0,1,2}, the zero seed orbits but 2 is a consistent
  value; v0.12 reported no reachable fixed point (`c15_definitive_claim_false`).
- **The `Export` runtime contract applied events to unnormalized states (#29).** The documented
  foreign-runtime recipe and the Python example looked up `step[e][state]` directly; on a registry
  where an invalid state is reachable by hand they diverged from `Machine.Apply`. Both now look up
  `nf[state]` first, as `Machine.Apply` does.
- **Federation docs stated the refuted claim that M1 alone gives federated convergence (#29).**
  M1 gives a terminating, valid repair; event order also needs C1 and C2
  (`fed_thm_fed_convergence_refuted`, `fed_thm_fed_convergence_guarded`).
- **Cross-registry event order was never checked (#19).** A target event reading a shared
  variable could diverge against a source event, and `Federation.Build` accepted it.
- **Two target events could diverge through the morphism repair (#26)** although each registry's
  CC and the cross-registry check passed (normalization-confluence `c2_counterexample`).
- **An outer morphism or resolver inside a certified sub was accepted unverified (#27).**
- **Declared-only mode hid non-commuting pairs (#24)** behind "Convergence: GUARANTEED".
- **A hand-built invalid start state voided order independence (#24).**
- **Monotone cycles could depend on declaration order (#20):** a Map non-monotone only on an
  invalid visited state built, and the result changed with the order morphisms were declared.
  Reaching `kleeneCap` returned a non-fixed point silently; it now panics.
- **`BuildCompositional` accepted a closure that broke its footprint (#22)**, and the lazy
  runtime could hang on a repair cycle; repairs are now bounded and the loop panics with the
  input, the state reached and the last invariant repaired.
- **`BuildCompositional` hung on `Int(0, math.MaxInt)` (#22, #24).**
- **`EmbedCertified` checked internal closures at one target state only (#21)**, and the runtime
  image check did not catch a write to a non-shared variable (#21).
- **`Certificate.Verify` accepted truncated or forged tables (#21).**
- **A foreign `Var` in `Writes` or `Watches` panicked with an index error (#24).**
- **A second resolver silently replaced the first (#23).**

### Performance
- **Certified repair is a table lookup (#27).** Apple M1 Pro, tables vs closures: a single-source
  repair 6.8 ns vs 39.7 ns, a two-source resolver 8.1 ns vs 184 ns (0 allocations vs 2), and a
  whole `FedMachine.Apply` 96 ns vs 276 ns (resolver sub) and 102 ns vs 168 ns (three-component
  chain). The compiled tables take one `uint64` per row plus one `int32` per encoding of each
  source component.
- **C2 costs** 4 table lookups per checked pair per consistent target state, within a constant of
  the target's own CC check (#26); C1 stays within the existing M1 enumeration bound (#19).
- **`Build` is about 10% slower** on `BenchmarkBuild_WideCounters10` (the idempotence pass and
  saturation recording), and `Machine.Apply` about 0.5 ns slower (the normal-form lookup for an
  invalid input) (#24). `EmbedCertified` costs what `Embed` spends on internal morphisms (#21).

## [0.12.0] - 2026-10-03

A soundness release. `Build` in v0.11.0 and earlier certified some machines that do not converge
(it skipped the CC check for pairs whose invariant footprints were disjoint, and never checked what
a guard or effect reads). It now checks every pair exactly, and every machine `Build`,
`SynthesizeWith` and `BuildCompositional` return has had its tables certified, in-process, by the
table oracle generated as Go from the Rocq proof (normalization-confluence); `Build` also runs the
proof's rules oracle when the machine is within its work cap. If an oracle does not certify, the
call returns an error and no machine. Around this, registries, closures, federations and
certificates now reject inputs that let a check and the runtime see different machines (duplicate
names, out-of-domain results, foreign registries, declarations made while a check runs).

Machines built with v0.11.0 or earlier should be rebuilt. Certificate digests changed, so
certificates must be re-issued. See "Upgrading from v0.11.0".

### Upgrading from v0.11.0

No exported identifier is removed and no signature changes (the API changes are additions). What
can start failing at run time, and what to do:

- **Rebuild every machine.** `Build` now checks every event pair exactly. A machine that built
  under v0.11.0 and now fails was never convergent; the report gives the counterexample. Fix the
  rules, or let `BuildOrSynthesize` synthesize a convergent repair.
- **Re-issue every certificate with `Certify`.** Certificate digests now bind names (events,
  variables and their kinds, enum labels, input ports) and the declared `Independent` pairs, so
  every digest changed and a certificate issued by v0.11.0 or earlier fails `Certificate.Verify`
  and `EmbedCertified` with `digest does not match`. The certificate version tag
  (`gsm-fedcert-v3`) is unchanged: gsm keeps one development certificate version until 1.0 and
  relies on re-checking on load, not on version bumps. `Certificate.Verify` also requires each
  component map key to be its registry's name.
- **Names must be unique.** A registry that declares two events, or two variables, with the same
  name is rejected (`duplicate event name`, `duplicate variable name`), and so is an enum that
  repeats a label (`duplicate label`). Rename them.
- **Closure results must be states of the machine.** Effects, repairs, morphism `Map`s and
  `Resolver`s that return a state built from another registry's variables, a value outside a
  variable's range, or bits outside the encoding are rejected with an error naming the rule and
  the state; an effect's out-of-range value is no longer clamped. Build results from the input
  state with `Set`, `SetBool`, `SetInt` or the combinators. Lazy `BuildCompositional` machines and
  `FedMachine` panic on such a result at `Apply` time, and a lazy machine's `Apply` and
  `Normalize` panic on an input that is not one of its states.
- **`Machine.MergeProjection` errors on out-of-domain input.** Its signature is unchanged (it
  already returned an error), but it now returns one, and leaves the state unchanged, for a value
  outside its variable's range or bit field (it used to truncate) and for a state that is not a
  state of the machine. Check the error.
- **`BuildOrSynthesize` falls back to synthesis only when the compensation failed** (an invariant
  has no `Repair`, or WFC or CC fails). Other `Build` errors are returned as they are.
- **Federations.** A morphism's `Shared()` variables must be its target's; `BuildCoordinated`
  points must match a morphism by `Src`, `Dst` and shared set; `Certify` input ports must be this
  registry's variables (by value); `FedMachine.Of` and `Apply` panic for a registry outside the
  federation; and a closure that declares on a component while `Federation.Build` or `Certify`
  runs gets the federation rejected. `Build`, `BuildCompositional` and `Synthesize` likewise
  reject a registry a rule closure changed while it was being verified.
- **`BuildCompositional` rejects machines wider than 64 bits** instead of freezing the extra
  variables at 0.
- **Report text changed.** Code that pins or parses `Report.String` output sees:
  - an `Assurance:` line at the end of every report (`Assurance: not certified` when not
    certified);
  - `Verified oracle: did not certify` and `Convergence: NOT CERTIFIED (no machine)` when an
    oracle does not certify;
  - a `Rules oracle: not run: <reason>` line on a certified report that skipped the rules oracle;
  - `Rule results: FAIL` (new `Report.DomainViolation`) and `Footprint conformance: FAIL` (new
    `Report.FootprintViolation`, with WFC and CC "not evaluated") instead of a WFC failure;
  - `Components: N` instead of `States: 0` for compositional reports;
  - `Report.PairsDisjoint` always 0 for `Build`.
- **`WriteConvergenceTables` writes format version 2** (`gsm-tables 2` header, normal-form table,
  declared pairs). External readers of the format 1 file must be updated.
- **`Build` costs more.** The table oracle re-checks every machine; the rules oracle runs when its
  work is at most `RulesOracleMaxWork` (2^29). `BuildOrSynthesize`, synthesis and
  `BuildCompositional` run the table oracle. Measured on an Apple M1 Pro with Go 1.26 (the last
  column is `main` before the gate, from #15):

  | `Build` of | Oracles run | v0.12.0 | Without the gate |
  |---|---|---|---|
  | 24 states, 3 pairs (closure rules) | table | 0.34 ms | 0.32 ms |
  | 1,024 states, 10 events, 45 pairs (closure rules) | table | 3.0 ms | 2.2 ms |
  | 2^19 states, 1 event (combinators) | table and rules | 0.67 s | |
  | 2^12 states, 20 events, every pair (combinators) | table and rules | 1.5 s | |
  | 2^20 states, 20 events, every pair (closure rules) | table | 5.6 s; peak RSS about 330 MB over three builds | 0.76 s; 240 MB |

  Within its cap the rules oracle adds at most about 2.2 s and under about 170 MB. Declaring only
  the pairs that must commute (`Independent`) keeps the gate fast. Running out of memory or
  goroutine stack kills the process rather than returning an error: plan memory for the largest
  machines you build. Test suites that build many combinator machines run slower, notably under
  `-race`.
- **Audit layers.** `PolicyDigest` is unchanged (it covers `PolicyBytes` only, by position). Use
  the new `Registry.PolicyIdentityDigest` to bind names and declared pairs too.

### Behaviour changes

Every change below can make code that worked with v0.11.0 behave differently. Details are in the
sections that follow.

- **Verification.** `Build` checks every event pair exactly (no footprint shortcut), over the
  valid states plus the zero state. `BuildCompositional`'s footprint check tries every value of
  each outside variable and of each pair of them, and refuses machines wider than 64 bits.
- **The oracle gate.** `Build`, `SynthesizeWith` (so `BuildOrSynthesize` and `Synthesis.Machine`)
  and `BuildCompositional` return a machine only after the table oracle certifies its tables;
  `Build` also runs the rules oracle within `RulesOracleMaxWork`. Otherwise they fail closed.
  `Synthesis.Machine` decides from what `SynthesizeWith` recorded, not from the exported fields.
- **Names.** Duplicate event names, duplicate variable names and repeated enum labels are
  rejected.
- **Closure results.** Results that are not states of the machine are rejected (no clamping);
  lazy machines and `FedMachine` panic on them, and lazy `Apply` and `Normalize` panic on such an
  input.
- **Mid-run changes.** A registry changed by a rule closure while it is verified is rejected;
  `Federation.Build`, `Certify` and `DiagnoseCycle` work on the federation as called and reject a
  component changed while they run. Declared rules, `And`/`Or` and `Enum` copy the caller's
  slices. `Synthesis.Machine` uses the registry as synthesized.
- **API.** `Machine.MergeProjection` returns an error for out-of-domain values.
  `BuildOrSynthesize` falls back to synthesis only on compensation failures.
- **Federations and certificates.** Certificate digests bind names and declared pairs (every
  digest changed). `Certificate.Verify` and `EmbedCertified` rebuild every component with `Build`;
  `Verify` requires each key to be its registry's name. A morphism's `Shared()` variables must be
  its target's; `BuildCoordinated` rejects a point that matches no morphism by `Src`, `Dst` and
  shared set; `Certify` checks input ports by value; `FedMachine.Of` and `Apply` panic for a
  registry outside the federation.
- **Output.** `Report.String` lines change (see above); `WriteConvergenceTables` writes format 2.

### Security
- **Duplicate event names let `Build` certify a machine that diverges.** A registry could declare
  two events with the same name. `Independent` resolved the name to the first, while the built
  machine's `Apply` ran the last, so CC was checked for one event and the runtime applied another;
  an order-dependent machine was reported convergent. Every path that produces a machine, a
  certificate, or an export for the checkers now rejects the registry with
  `gsm: registry "r": duplicate event name "e"`: `Build`, `BuildCompositional`, `Synthesize`,
  `SynthesizeWith`, `BuildOrSynthesize` (reported as itself, not as a failed synthesis),
  `Federation.Build` (every component, before certificate validation), `Certify`,
  `BuildCoordinated`, `EmbedCertified` builds, `Certificate.Verify`, `WriteMachineAST`,
  `PolicyBytes`, `PolicyDigest`, and `WriteDeclaredPairs`. `Synthesis.Machine` is built from a
  snapshot of the registry taken when `Synthesize` returns, so a duplicate declared afterwards does
  not reach it. `Build`, `BuildCompositional` and `Synthesize` also reject a registry that a rule
  closure changed while it was being verified (`gsm: registry "r" was changed while it was being
  verified ...`), so a declaration made from inside a rule cannot get past the check.
  **Behaviour change:** a registry that reused an event name, which used to build, is now
  rejected; rename one of the events.
- **Duplicate variable names misrouted certificate re-checks and projections.** Certificate tables,
  input ports, and shared projections name variables, so with two variables of the same name
  `Certificate.Verify` re-checked a table against the last one while `MergeProjection` wrote the
  first, and a projection sharing both carried one value for the two. The same paths now reject
  `gsm: registry "r": duplicate variable name "v"`. **Behaviour change:** a registry that reused a
  variable name is now rejected.
- **`Certificate.Verify` re-checked tables against whichever registry a key named.** The digest names
  components by registry name but the re-check looked a table's target up by map key, so a map
  whose keys did not match the registries' names (for example two keys swapped) still matched the
  digest and re-checked a table against the wrong registry. **Behaviour change:** `Verify` now
  requires each key to be its registry's name.
- **`Synthesis.Machine` read the registry when called, not when synthesized.** An event declared
  after `Synthesize` (a duplicate name made `Apply` and `Events` panic), or a pair declared
  `Independent` afterwards, reached the machine; the second was recorded as CC-checked and written
  by `WriteConvergenceTables`, although synthesis never checked it and the pair need not commute.
  `Synthesis` now keeps the variables, event names, and pairs it ran on, and `Machine` and
  `Repairs` use them.
- **Closures could return a state outside the machine, and `Build` certified it.** Effects,
  repairs, morphism `Map`s and `Resolver`s are Go closures, and their results were recorded
  without checking that they were states of the machine: a state built from another machine's
  variables, a value outside a variable's range, or bits outside the encoding. Depending on the
  case `Build` certified the machine (and `Apply` then returned a state outside the certified
  space) or panicked with an index out of range. These paths now check every result they
  compute and reject the machine with an error naming the rule, the input state and the result:
  `Build`, `BuildCompositional` (including its footprint check; reported in the new
  `Report.DomainViolation`, not as a WFC or footprint failure), `Synthesize`, `Federation.Build`
  (components, and every `Map` and `Resolver` image computed while verifying M1, R1/R2 and
  monotonicity), `DiagnoseCycle`, and `Certify` and `EmbedCertified`. Their reach is limited to
  the results they compute: `BuildCompositional` runs closures only on each component's states
  (other variables at zero) and their footprint perturbations; and an embedded sub's internal
  `Map`s and `Resolver`s are run only at one representative target (when the tables are
  extracted to match the certificate), so one edited after certification to go wrong at other
  targets is not caught by `Federation.Build`. A state of the machine is defined by value: the same variable schema (each variable
  equal in name, kind, layout, range and labels, so a structurally identical machine's state is
  accepted), every variable within its range, and no bit outside the encoding. A lazy
  `BuildCompositional` machine and a `FedMachine` run closures at `Apply` time and now panic on
  any such result, as `Apply` does for an unknown event, which also covers what the build-time
  checks above cannot reach. A lazy machine's `Apply` and `Normalize` also check their input
  and panic, naming it, on one that is not a state of the machine (before, a correct rule was
  blamed, or the input was returned unchanged). On a table machine the input is a documented
  precondition and not checked, since the check costs 30% to 150% of the lookup. `Certificate.Verify` refuses table values
  outside a variable's range and rows of the wrong length. The differential test now reports a
  certified machine whose tables cannot be exported instead of skipping it.
- **`Build` certified machines that do not converge.** It skipped the Compensation Commutativity
  check for any event pair whose triggered invariant footprints were disjoint, and never checked
  what an event's guard or effect reads. The shortcut's theorem (`disjoint_events_commute` in
  normalization-confluence) assumes each event reads only its own footprint. Any event whose guard
  reads a variable another event writes could be certified convergent when it is not; the naive
  pay/ship machine (ship guarded on paid) was. The same shortcut also ignored write sets no
  invariant watches, so two events overwriting the same variable were certified. `Build` now checks
  every pair exactly, with no shortcut. Machines built with v0.11.0 or earlier should be rebuilt;
  a machine that now fails was never convergent, and the report gives the counterexample.
  Affected through `Build`: `Federation.Build` (every component), `Certify`, `BuildCoordinated`,
  and `BuildOrSynthesize` (a wrongly accepted machine was returned instead of being repaired).
  `Synthesize` and both extracted oracles never used the shortcut.
- **`BuildCompositional` footprint check missed ordinary reads.** It changed one outside variable
  at a time, to one value, from a zero background, so it never saw a dependence on any value but 1
  or a guard over two outside variables (`paid && inStock`). It now tries every value of each
  outside variable and of each pair of outside variables, and checks combinator rules exactly from
  the tree. Closures that depend only on three or more outside variables jointly remain undetected;
  this is documented. Pairs are now skipped only across footprint components (which also fixes the
  write-set and invariant-chain gaps above for `BuildCompositional`).
- **Certificates were trusted for their recorded verdict.** `Certificate.Verify` and
  `EmbedCertified` did not re-check component convergence, so a certificate issued by v0.11.0 for a
  non-convergent component would still be accepted after the fix above. Both now rebuild every
  component with `Build`, and `EmbedCertified` re-checks internal morphisms from the certificate's
  tables. A certificate whose digest matches is re-verified on load by the fixed checker; the
  certificate version is unchanged (see "Versioning and trust policy" in CERTIFICATE-DESIGN.md).
  Certificates issued by v0.11.0 no longer match their digest (see "certificate digests bind
  names" under Changed) and must be re-issued.
- **`BuildCompositional` accepted machines wider than 64 bits.** `State` is one `uint64`; variables
  past bit 64 were frozen at 0 yet the machine was certified. It now returns an error.

### Added
- **The rules oracle in the gate.** After the table oracle certifies a machine, `Build` also runs the
  rules oracle (`checkBuild` from the normalization-confluence proof, generated as Go and vendored
  in `internal/oracle` with the table oracle; `oracle.CheckRules`) on the machine's combinator rules,
  exactly what `WriteMachineAST` and `WriteDeclaredPairs` write. It re-derives every step from the
  expression trees, so it does not trust gsm's tables.
  - It runs on machines whose rules are all combinators, whose work is at most
    `RulesOracleMaxWork` (2^29), and that are inside its fragment. The work (`oracle.RulesCost`)
    is an upper bound on the steps `checkBuild` takes in the generated Go, counted from the
    expression trees, the checked pairs (every pair when none is declared) and the repair depth:
    per state, normalizing it, and for each checked pair both events' guards, effects and
    normalizations, twice. Otherwise the table oracle alone certifies the machine and the new
    `Report.RulesOracleSkipped` says why, with the work.
  - `Report.Assurance` is the new `AssuranceOracleTablesAndRules` when both ran;
    `AssuranceOracleTables` now means the table oracle only.
  - A rejection (repair does not terminate, or a declared pair does not commute), or no verdict,
    fails `Build` closed, with the error in `Report.OracleDisagreement`.
  - `SynthesizeWith` and `BuildCompositional` run the table oracle only.
  - Cost (generated Go, Apple M-series, `TestRulesOracleCostPerStep`): at most about 4 ns per
    step (4.04 ns on a wide Int domain; 0.7 to 3.2 ns elsewhere). Memory is mostly the box, about
    states x (variables + 1) list cells: from about 100 bytes per state at 2 variables to about
    220 at 20, plus a few MB. Within the cap that is at most about 2.2 s and under about 170 MB
    (the most measured within it: 160 MB, at 2^20 states of 10 four-valued Ints).
  - Output change: the report's oracle-failure header is now `Verified oracle: did not certify`,
    and a certified report that skipped the rules oracle ends with a `Rules oracle: not run:` line.
- **The oracle gate: the proof re-checks every machine, in-process.** `Build`, `SynthesizeWith`
  (and so `BuildOrSynthesize` and `Synthesis.Machine`) and `BuildCompositional` (per footprint
  component) now give the tables gsm's verification produced to the table oracle.
  - **The oracle.** It is `check_fn` from the normalization-confluence proof (the table oracle
    over accessor functions, proven equal to `check_tables` and to the list oracle `check_fast`;
    its guarantee, `check_fn_converges`, is stated on the accessors), generated as Go
    from the Rocq extraction and vendored in `internal/oracle` (no OCaml, no subprocess, no
    dependencies). `internal/oracle/PROVENANCE` pins the proof commit, the prover image and the
    hashes. CI regenerates the file from them and requires the same bytes.
  - **Failing closed.** If the oracle does not certify the tables, the call returns an error and
    no machine, and `Report.OracleDisagreement` holds the oracle's error. With deterministic
    rules, a disagreement points at a bug in gsm's verification or in the oracle's generator. An
    impure rule, which gives different results when `BuildCompositional` runs it again, can cause
    one too.
  - **`Federation.Build` and `Certificate.Verify`.** They rebuild components with `Build`, so
    every component's tables are gated. The morphism checks (M1, acyclicity, monotone-cycle
    iteration) and certificate re-derivation are gsm's Go code and are not oracle-gated.
  - **In place.** The oracle reads the machine's tables through accessors (`oracle.Lookup`,
    `oracle.CheckLookup`) and copies nothing: the gate adds about 12 MB (the renumbering of the
    in-domain states) at 2^20 states. With 20 events and every pair declared, `Build` with the
    gate takes about 5.6 s (0.76 s without) and three builds peak at about 330 MB RSS (240 MB
    without).
  - **Limits.** Running out of memory or goroutine stack kills the process rather than returning
    an error.
  - **Output change: `Report.String` ends with an `Assurance:` line.**
    - A certified report prints it after `Convergence: GUARANTEED`.
    - Every report that is not certified now ends with `Assurance: not certified`. This covers a
      failed WFC or CC as well as an oracle rejection (which prints `Convergence: NOT CERTIFIED
      (no machine)`).
    - Code that pins or parses report text sees the new line.
  - **`Synthesis.Machine` decides from what `SynthesizeWith` recorded.** Changing the exported
    `Convergent` or `Assurance` fields does not make it return a machine.
  - **`Report.Assurance`** (and `Synthesis.Assurance`) record what certified a machine:
    `AssuranceOracleTables`, or `AssuranceOracleComponents` for `BuildCompositional`, where
    cross-component independence still rests on gsm's footprint check.
- **Example-machine gate in CI.** Every machine gsm's examples make (each `Example` function and
  the README run block) is checked by both extracted checkers in the oracle job, against a catalog
  of every machine and its expected verdict (`.github/oracle/machines.txt`). The job fails if a
  checker rejects a listed machine, disagrees with `Build`, or an example makes a machine the
  catalog does not list; a test fails if an example is missing from the catalog. A program built
  with `-tags gsmgate` and run with `GSM_GATE_DIR` set records every machine it makes (each `Build`
  result, synthesized and compositional machine) for `internal/cmd/gsmgate`, which projects using
  gsm can run on their own machines. Without the tag nothing changes. The checker binaries are
  named in `pins.env`, so a faster checker replaces one by pin.
- **`Registry.WriteDeclaredPairs`** writes the pairs CC is checked for, in the format the rules
  oracle takes as an optional second file. It is not part of `WriteMachineAST`'s output, so
  `PolicyBytes` and `PolicyDigest` are unchanged.
- **The oracle cross-check is required in CI.** A new `oracles` job builds both extracted checkers
  from a pinned proof commit in a digest-pinned Rocq image, checks them against pinned SHA-256
  hashes (`.github/oracle/`), and runs the whole test suite with `GSM_REQUIRE_ORACLES=1`, so the
  oracle tests fail instead of skipping when a checker is missing.
- **Differential test of every `Build`.** `oracle_differential_test.go` cross-checks every machine
  the test suite builds, plus 600 random combinator machines (`GSM_DIFF_RANDOM` sets the count),
  against both checkers, and fails on any disagreement with `Build`.
- `TestDocSnippets`: every Go block in the top-level docs type-checks, and README examples marked
  `run` execute (so an example whose machine does not `Build` fails CI).
- A property test and fuzz target comparing `Build` and `BuildCompositional` against brute-force
  enumeration of event orderings on random machines whose guards read other events' writes.
- Benchmarks for `Build` and `BuildCompositional`.

### Changed
- **The table oracle is faster with many events and declared pairs.** Pinned to
  normalization-confluence#12: `check_fast` computes the same boolean (`check_fast_eq` is
  unchanged), reading each step target's column once per state instead of once per declared
  pair. Verdicts are unchanged; the extracted binaries' hashes change.
- **The rules oracle now checks gsm's largest machines.** The pinned `astchecker` died with
  `Stack_overflow` under OCaml 4.14 on 2^20-state machines: it multiplied domain sizes and
  converted every variable read to Z through unary arithmetic, and enumerated the valuations
  with non-tail recursion. normalization-confluence#11 replaces each with a proven-equal form
  (or, for the enumeration, one proven to have the same members), with no new extraction
  directives; verdicts are unchanged. Pinned to its merge commit.
- **Behaviour change: certificate digests bind names and declared pairs.** Certificate digests
  covered each component's rules in the oracle's positional format only, so two subsystems whose
  components differed only in an event name, a variable name or kind, an enum label, or the
  declared `Independent` pairs had the same digest, although replay, projections, certificate
  tables, input ports, `Set` and the CC check address those. The new `Registry.PolicyNames`
  serializes them, and certificate digests now cover it. Every certificate digest changes:
  certificates issued earlier no longer match and must be re-issued with `Certify`. Per the
  development versioning policy the tag (`gsm-fedcert-v3`) is unchanged. The test certificate
  `testdata/payship-cert-v0.11.0.json` has its `Digest` recomputed (its other fields are as
  v0.11.0 issued them). The digest also quotes every name it frames (component, table target,
  sources and shared variables, input ports), which unquoted could collide (a port `a.b`/`c`
  digested like `a`/`b.c`), and digests the declared pairs as a set, so the same pairs declared
  in another order, direction or more than once digest alike. `CERTIFICATE-DESIGN.md` states what the digest covers: a morphism or
  resolver closure is bound only through its table, at one representative target.
- **`PolicyDigest` is unchanged and does not cover names.** It stays SHA-256(`gsm-policy-v1` "\n"
  `PolicyBytes`), the oracle's input, which external audit layers recompute. That input addresses
  variables and events by position, so registries that differ only in names, kinds, labels or
  declared pairs share a `PolicyDigest`. The new `Registry.PolicyIdentityDigest` (tag
  `gsm-policy-identity-v1`) covers `PolicyBytes` and `PolicyNames` together. Moving the names into
  the serialized format, and so into `PolicyDigest`, is planned before 1.0.
- **The table oracle now certifies gsm's largest machines.** The pinned `checker` (built with
  OCaml 4.14.2 in the pinned image) died with `Stack_overflow` on 2^20-state tables, gsm's maximum
  size, so those machines failed the oracle cross-check with an input error. The oracle is now
  pinned to `check_fast` (normalization-confluence `TableFast.v`), proven equal to `check_tables`
  and axiom-free. Its stack depth does not grow with the number of states, events or declared
  pairs. On 2^20 states with 10 declared pairs it takes about 5 s (OCaml 4.14.2); the previous
  checker took about 60 s there when built with OCaml 5, and never completed with the pinned 4.14.2
  build. `TestConvergenceTables_LargestMachine` (20 flags, 2 events) now requires the oracle to
  certify a 2^20-state machine.
- **The extracted checkers decide exactly what `Build` checks.** Both oracles previously checked a
  different property, so the differential test had to excuse whole classes of disagreement. Now
  they check `Build`'s: repair terminates from every state (the rules oracle used to require it
  only where an event reaches), only the pairs declared `Independent` are checked (both used to
  check every pair), and commutation is checked on the valid states plus the zero state (the table
  oracle used to check invariant-invalid encodings, and the rules oracle skipped an invalid zero
  state). The differential test now fails on any disagreement except a rules-oracle refusal
  outside its arithmetic fragment, and the table oracle also checks, and must reject, the tables
  of every machine `Build` rejects for CC. Proofs: normalization-confluence `TableCheck.v`,
  `Trace.v` and `checkBuild` in `AstChecker.v`.
- **`WriteConvergenceTables` writes format version 2**: a `gsm-tables 2` header, the normal-form
  table, and the declared pairs, so the table oracle can check `Build`'s property. It also no
  longer panics on a synthesized machine, and fails rather than writing a wrong id if a table
  entry is not an in-domain encoding.
- **`WriteMachineAST` refuses a variable from another registry.** The rules format names a variable
  by index only, so a `Var` from another registry with the same name and index (which gsm reads
  with that registry's minimum and domain) was exported as this registry's variable, a different
  machine.
- **`Do()` with no assignments is a combinator rule.** It returned a nil `Transform`, so an event
  declared with an empty effect could not be exported.
- **Behaviour change:** `Machine.MergeProjection` returns an error, and leaves the state
  unchanged, for a projection value outside its variable's domain (out of range, or wider than
  the variable's bit field, which it used to truncate silently) and for a state that is not a
  state of the machine. It used to write such values, producing a state outside the machine.
- **Behaviour change:** `BuildOrSynthesize` falls back to synthesis only when the compensation
  failed (an invariant has no `Repair`, or WFC or CC fails). Any other `Build` error, such as a
  rule result outside the machine or a registry changed while it was verified, is returned as
  is; it used to be followed by synthesis, which could return a machine for it.
- An event effect that returns a variable outside its range is rejected instead of clamped.
  `Build`, `BuildCompositional`, `Synthesize` and the lazy runtime used to clamp each variable of
  an effect's result into its range. `Set`, `SetBool`, `SetInt` and the combinators never produce
  such a value, so only a result built some other way (another machine's state, a raw field
  write) was affected, and clamping hid the bug.
- The CC check runs over the valid states plus the zero state `NewState` returns (CC1 as THEORY.md
  §6.3 states it), instead of every encodable state for the pairs it did check. Checking invalid
  states that no run reaches rejected convergent machines (the order-fulfillment test machine fails
  only at the invalid state {shipped, unpaid}); runs started from a hand-built invalid state are
  outside the guarantee, as documented.
- `Report.PairsDisjoint` is always 0 for `Build`. New `Report.FootprintViolation` names the cause
  when `BuildCompositional` rejects a rule for reading or writing outside its footprint; the report
  then shows WFC and CC as "not evaluated" instead of "WFC: FAIL". Compositional reports show
  `Components` instead of a `States: 0` line.
- `EmbedCertified` re-checks certified components' CC (it still skips re-verifying internal
  morphisms from their closures). Cost: two table lookups per state per pair, on tables it builds
  anyway.

### Fixed
- **The example-machine gate recorded synthesis candidates.** `SynthesizeWith` builds a candidate
  machine to have the table oracle certify it, and the `gsmgate` hook sat where that candidate is
  built, so a gate run recorded it as well as the machine `Synthesis.Machine` hands out (twice for
  `BuildOrSynthesize`), and recorded a candidate the oracle refused. The hook is now in
  `Synthesis.Machine`, after its certified check: the gate records only machines a program receives.
- **Behaviour change: `FedMachine.Of` and `FedMachine.Apply` panic for a registry outside the
  federation.** They used the lookup's zero value and acted on component 0. They now panic naming
  the registry, as `Apply` does for an unknown event (`ApplyNamed` already returned an error).
- **Behaviour change: `BuildCoordinated` rejects a coordination point that names no morphism.** It
  ignored such a point and built the federation without the coordination the caller asked for.
- **Behaviour change: an enum may not repeat a label.** `Set`, `TrySet` and the label sugar resolve
  a label to its first index, so a repeated label was unreachable. `Build` and every other path
  that checks names return `gsm: registry "r": enum "e" has duplicate label "l"`.
- **Behaviour change: a morphism's `Shared()` variables must be its target's.** They were never
  checked, so a `Var` of another registry at the same index made `Federation.Build` panic, or one
  with the same name and another range was taken for the target's. `Federation.Build` and
  `DiagnoseCycle` now return `morphism a→b: Shared() variable "v" is not a variable of registry
  "b"`.
- **Behaviour change: `BuildCoordinated` matches a point's `Shared`.** A point removes the
  morphisms matching its `Src`, `Dst` and shared variable set (in any order), as
  `CoordinationPlan` returns them; one that matches no morphism on all three is an error.
- **Behaviour change: `Certify` checks an input port's variable by value.** It used the variable's
  index only, so a `Var` of another registry declared the variable at that index of this one as a
  port (or an index past the end passed). A port's variable must now be this registry's variable
  (same name, kind, layout, range and labels).
- **`DiagnoseCycle` analyzes the federation as called.** Like `Federation.Build`, it works on a
  frozen copy and rejects a component that a morphism closure changed while it ran (it returned
  a diagnostic about a different registry, with no error).
- **A declared combinator rule could be rewritten through the caller's slice.** `Do(as...)`
  returned the caller's slice and `DeclEvent`, `DeclEventGuarded` and `DeclInvariant` kept it, and
  `And(ps...)`/`Or(ps...)` kept theirs, while write sets and footprints were computed once. Changing
  an element afterwards changed the rule without changing anything the build checks, so a
  `BuildCompositional` machine's `Apply` could run a rewritten, unverified rule (an event writing
  outside its declared write set, order-dependent), and the policy digest changed after the build.
  The declarations and `And`/`Or` now copy what they are given. `Enum` also copies its labels.
- **`Federation.Build` and `Certify` could act on declarations no check saw.** Both build the
  components, then run morphism closures (verification, and for `Certify` table extraction). A
  closure that declared on a component there left the registry and the `FedMachine`'s machine for
  it differing silently, and `Certify` digested the declaration. A closure that added a morphism
  to the federation reached the later passes (the monotonicity check) but not the machine, and
  `Certify` put its unverified table into the certificate. Both now work on a copy of the
  federation's wiring taken when they are called, so a morphism, component, or resolver added to
  the federation while they run is ignored (not verified, and not in the machine or certificate),
  and they reject a component registry changed while they run (`gsm: registry "r" was changed
  while it was being verified ...`). **Behaviour change:** a federation whose closures declare on
  a component while it is built or certified is now rejected.
- **The rules oracle used the wrong arithmetic.** `astchecker` (normalization-confluence) evaluated
  rules over the natural numbers, so `Sub` truncated at 0 and a guard such as
  `Lt(Sub(V(a), V(b)), Lit(0))` was never true. It certified machines that `Build` correctly
  rejects. It now evaluates with gsm's signed arithmetic, and refuses to certify expressions that
  could exceed 2^31-1 in magnitude or writes that could store a negative value into a two-valued,
  minimum-0 variable (a `Bool` stores `value != 0`). `WriteMachineAST` now exports negative
  minimums and literals; its output is otherwise unchanged, so `PolicyDigest` is unchanged for
  every policy that exported before.
- The README's flagship example did not build (`Build` correctly rejects it), so pasting it
  panicked. It is replaced by a convergent order-fulfillment machine that shows the pattern (events
  record facts; an invariant derives the outcome) and explains why the guarded draft fails.
  CONCEPTS.md taught the same guard as a fix for a CC failure; it now shows why it diverges.
- Docs that described `Build`'s footprint shortcut, CC2 as holding "structurally", or combinator
  rules as needing no read check are corrected (README, THEORY, ARCHITECTURE, CONCEPTS).

## [0.11.0] - 2026-09-26

### Added
- **Verify-or-repair for registries** (`Registry.BuildOrSynthesize`): ties gsm's two mechanisms
  together. It verifies with `Build` and, when the rules as written do not converge, falls back to
  synthesizing a convergent compensation and building that. Returns the machine plus a `*Synthesis`
  that is nil when `Build` succeeded as written and non-nil (with `Repairs()` and `String()`
  describing what changed) when a synthesized compensation was substituted; it errors only when
  neither works, carrying the impossibility witness when the search is exhaustive. So a caller can say
  "build this, and if my repair does not converge, give me one that does."

## [0.10.0] - 2026-09-25

### Added
- **Accept-with-coordination for cyclic federations** (`Federation.CoordinationPlan`,
  `Federation.BuildCoordinated`, `CoordinationPoint`): when a cyclic morphism network is not monotone,
  `Build` rejects it; `CoordinationPlan` now says WHERE to put coordination, returning a set of
  morphism edges (shared variables) to place under an external single writer or consensus so the rest
  of the network converges coordination-free. `BuildCoordinated(plan)` accepts the federation given
  that coordination: the coordinated edges become external inputs and the acyclic residual is built as
  usual. The plan is a feedback edge set (correct, polynomial, at most the number of independent
  cycles); it is not necessarily the minimum, which is the group feedback edge set problem, NP-hard in
  general (see the categorical structure note in the papers repo). This turns a blunt cyclic rejection
  into a localized mixed-consistency partition: consensus only on the obstructing edges, everything
  else coordination-free.

## [0.9.2] - 2026-09-25

### Fixed
- **Unsatisfiable targets no longer pass vacuously**: `Build` now rejects a morphism or resolver whose
  target registry has no valid state. M1/R2 were trivially satisfied because the verification loop had
  no target states to range over; such a target can never converge to a valid state, so it is a
  definite error rather than a silent pass.
- **Morphisms are evaluated at a valid representative target**: table extraction (`Certify`), the
  monotonicity check, and `SharedProjection` previously applied a source-determined morphism to the
  raw zero-encoded target, which may itself be an invalid state (outside the morphism's contract).
  They now use a valid representative target. Source-determinacy (verified at `Build`) guarantees the
  shared image is independent of which valid target is chosen, so results are unchanged for
  well-formed morphisms and correct for ones that read the target.
- **`DiagnoseCycle` orbit detection is exact**: orbit repetition is keyed on the full packed state IDs
  of the cycle components rather than a human-readable shared-carrier string, which could collide (the
  shared projection omits local state, and the "=,|" delimiters could clash) and report a false orbit.
- **Single-value `Int` ranges are rejected**: `Int(name, n, n)` compiled to a zero-bit variable with
  no states to range over. It now panics at declaration, matching `Enum`'s at-least-two-values rule,
  so the declaration mistake surfaces immediately.

### Changed
- Internal refactors with no behavior change: the resolver source/shared collection and the
  mixed-radix source-combination enumeration are unified into shared helpers, and the ~1040-line
  `federation.go` is split into DSL, verification, and runtime files.

## [0.9.1] - 2026-09-25

### Added
- **Cyclic-federation obstruction diagnostic** (`Federation.DiagnoseCycle`, `CycleDiagnostic`): when a
  cyclic morphism network is rejected, identify the offending loop and, by iterating its repair from
  the zero seed, report whether the loop settles or oscillates (with an orbit witness); `Build`'s
  cycle-rejection error now names the loop. This is the loop-composite fixed-point obstruction from
  the categorical account: a cycle converges iff its loop composite has a reachable fixed point, so a
  monotone loop always settles (Knaster-Tarski) and a non-monotone one (for example a negation loop)
  orbits. Cycle-local and best-effort (a representative zero seed); it explains a rejected cyclic
  Build rather than re-deciding convergence.

## [0.9.0] - 2026-09-25

### Added
- **Effective-registry certificates** (`Federation.Certify`, `Federation.EmbedCertified`, `Certificate`, `MorphismTable`, `Certificate.Verify`): package a verified sub-federation as a portable certificate and reuse it without re-verifying its internals. `Certify` builds and verifies a subsystem and records the verdict, each morphism/resolver in extensional table form (reified from the finite, source-determined maps), and a tamper-complete digest over the component policies plus those tables. `EmbedCertified` composes a subsystem on its certificate: `Build` checks only the seam (boundary morphisms) and the whole-graph acyclicity, skipping per-component CC re-enumeration and internal-morphism re-verification (a `Build`/`build(runCC)` split trusts CC for certified components while still constructing their runtime machine and checking WFC). `Certificate.Verify` is an independent differential re-checker: given the consumer's own component registries, it re-derives validity preservation (M1/R2) from the tables rather than the producer's closures and matches the digest, so a composition is confirmed without trusting the producer's code. **Input ports** (`Certify(Port{...})`): a subsystem may declare free shared variables (no internal morphism writes them) that an outer morphism may drive once embedded; `Build` verifies each inbound boundary morphism at the seam (M1/R2) and rejects a write to any sealed variable, and the port declaration is folded into the tamper-complete digest. This is the assume-guarantee (Theorem 2') case. An axiom-free-Coq-extracted federation oracle remains future work. See CERTIFICATE-DESIGN.md.

## [0.8.0] - 2026-09-24

### Added
- **State digest** (`State.Digest`, `StateDigestVersion`): a stable, domain-separated SHA-256 over a state's packed value, as lowercase hex. Meaningful alongside a policy digest (which pins the variable layout the packing depends on): the pair names a state unambiguously, and a reference build of the same policy replaying the same events reproduces the same digest. This lets an external audit trail bind an action to its exact resulting state in one committed leaf, so a verifier replaying the policy can confirm the runtime's state matched the reference at each transition (a per-run differential check, not a refinement proof for all inputs).

## [0.7.0] - 2026-09-24

### Added
- **Combinator rule vocabulary** (`DeclInvariant`, `DeclEvent`, `DeclEventGuarded`, and the `V`/`Lit`/`Add`/`Sub`/`Le`/`Lt`/`Eq`/`And`/`Or`/`Not`/`Set`/`Do` combinators): express invariants and events as data built from a fixed, gsm-owned vocabulary instead of arbitrary Go closures. Still reads as Go (`Le(V(a), Lit(3))`, `Set(a, Add(V(a), Lit(1)))`) but produces an expression tree gsm can both evaluate and analyze. Footprints are derived from the tree (the variables it reads and writes), so combinator rules are footprint-conformant by construction: no `Watches`/`Writes` needed, and nothing to check because the declaration is the footprint. This is the additive, closure-free surface that makes rules inspectable and serializable (the precondition for a verified verifier and portable policies); the closure API is unchanged. Prototype.
- **Ergonomic surface over the combinator primitives** (`sugar.go`): read-as-English helpers (`AtMost`, `AtLeast`, `Below`, `Above`, `Is`, `IsNot`, `InRange`; `SetTo`, `Inc`, `Dec`, `IncBy`, `DecBy`, `Raise`, `Lower`) and fluent builders (`r.Rule(name).Require(pred).RepairWith(xform).Add()`, `r.On(name).Does(xform).OnlyIf(guard).Add()`) that **lower to the same combinator AST** the analyzable core is built from. The layering is deliberate and load-bearing: the friendly layer is for humans, the primitive layer is what is serialized, tested, and handed to the machine-checked checker. `sugar_test.go` pins it: a machine written with the sugar serializes (`WriteMachineAST`) to byte-identical AST as the same machine written with raw combinators, so the sugar can never smuggle in semantics the verified oracle does not see.
- **Differential testing against verified oracles** (`Machine.WriteConvergenceTables`, `Registry.WriteMachineAST`): re-certify a built machine, independently of this Go code, using checkers extracted from an axiom-free Coq proof (`normalization-confluence/coq/extraction`). Two angles: the **table oracle** consumes emitted step tables (per-event step functions commute and stay in range; valid states remapped to a compact index; `oracle_test.go`, `GSM_CONVERGENCE_CHECKER`); the **rules oracle** consumes the serialized combinator rules and recomputes convergence straight from the expression trees, so it trusts neither gsm's enumeration nor its normalization (`astoracle_test.go`, `GSM_AST_CHECKER`). A bug in gsm's own verification cannot make a non-convergent machine pass either oracle. `WriteMachineAST` serializes the fragment the Coq model covers (variables with any nonnegative minimum; comparison/`and`/`or`/`not` predicates; `Set`/`Add`/`Sub` transforms; events with an optional guard) and refuses anything outside it, so a passing cross-check always compares like semantics.
- **Footprint-local verification** (`Registry.BuildCompositional`): certify WFC and CC per footprint component instead of over the whole state space. `Build` is capped by global enumeration (20 bits); `BuildCompositional` partitions variables into footprint-connected components and verifies each over its own (small) subspace, so a machine with many independent small invariants certifies even when its global state space is astronomically large. Cross-component event pairs commute by footprint disjointness (the mechanized `disjoint_events_commute` result); shared-footprint pairs are brute-forced within their component. Returns a lazy `Machine` that computes `Apply`/`Normalize` at runtime from the rules (no global tables, so `Export` is unavailable). Preconditions: every invariant declares a footprint, every event declares its writes, the zero state is valid, and no single component exceeds the enumeration budget.
- **Policy as a verifiable artifact** (`Registry.PolicyBytes`, `Registry.PolicyDigest`): a stable, domain-separated SHA-256 over the canonical `WriteMachineAST` serialization, so a combinator policy can be named and anchored independently of who built it. The digested bytes are exactly the verified AST oracle's input, so the anchored digest and the re-checked artifact cannot diverge, and a rule written with the sugar surface digests identically to the equivalent primitive combinators (tested). This is the stable interchange contract an external audit layer (for example an agent SDK's transparency log) pins to.

### Changed
- The convergence theorem is now **machine-checked** (axiom-free Coq/Rocq; see the `normalization-confluence` `coq/` artifact and the `proof: machine-checked` badge). No library behavior change.

## [0.6.0] - 2026-09-23

Compensation synthesis grows up: it now scales (backtracking + forward-checking instead of brute force), takes a preference to steer the repair it chooses, can return the provably minimum-cost repair, and surfaces a concrete witness when convergence is impossible. Breaking: two `Synthesis` fields were renamed.

### Added
- **Optimal synthesis** (`Optimal` option): branch-and-bound for the provably minimum-cost convergent repair, rather than the first ordering-biased one. Uses the accumulated cost plus an admissible lower bound to prune branches that can't beat the best found. When the search completes (`Exhaustive`) the result is guaranteed optimal; if the budget is hit first it's the best-so-far. `Synthesis.Cost` reports the representative repair's total cost.
- **Preference-guided synthesis** (`Registry.SynthesizeWith`, `Prefer`): supply a `cost(from, to)` over repairs and synthesis returns the least-costly convergent one it finds (an ordering bias — tries lower-cost targets first). Encodes a domain policy ("prefer hold over cancel", "toward a safe state", "avoid destructive repairs") so the synthesized repair is the one you *want*, while gsm keeps the convergence guarantee. It only chooses among convergent repairs. `Synthesize()` is now `SynthesizeWith()` with the default policy below.
- **Least-invasive repair by default**: with no preference, synthesis orders each invalid state's candidate repairs by how few variables they change (nearest valid state first), so the representative compensation is the minimal, likely-sensible one — e.g. "clamp the violating variable" rather than an arbitrary reset. Directly mitigates the convergent-≠-desirable caveat.
- **Impossibility witness** (`Synthesis.Witness`): when no compensation converges, synthesis surfaces a concrete critical pair — two events that from a valid state reach *distinct already-valid states* — that no repair can reconcile. Makes "redesign the events" actionable instead of a bare verdict.

### Changed
- **Compensation synthesis now scales via backtracking + forward-checking** (was brute-force enumeration over `|valid|^|invalid|`). Same completeness — it finds a convergent compensation iff one exists, and proves impossibility by exhaustion — but it prunes the assignment tree, handling spaces far beyond the old `2^20` cap (a test solves an `8^8 ≈ 16.7M`-assignment problem in ~10 nodes). Still worst-case exponential (CC synthesis is NP-hard); a search budget now distinguishes **provably impossible** (exhaustive) from **undetermined** (budget hit — a SAT/SMT encoding would settle those). `Synthesis` fields changed: `Alternatives`/`Searched` → `Exhaustive`/`Nodes`.

## [0.5.0] - 2026-09-23

Compensation synthesis: gsm can now *generate* a convergent compensation, not just verify one — or prove none exists. Additive over v0.4.x.

### Added
- **Compensation synthesis** (`Registry.Synthesize`): instead of verifying a compensation you wrote, gsm can now *generate* one. Given the invariants' validity predicates and the events (Repair omitted), it searches for a normal-form map on invalid states that satisfies CC — returning a representative convergent compensation as a ready-to-use `Machine` plus an inspectable repair map (`Synthesis.Repairs`), reporting how many alternatives exist, or **proving that no compensation can make the registry converge** (the invariants + events must be redesigned). Convergent ≠ desirable: the synthesized repair only makes orderings agree, so inspect it and judge acceptability. Brute-force over `|valid|^|invalid|`, bounded by an internal cap (a SAT/SMT encoding would lift the ceiling).
- Invariants may now be declared **without a `Repair`** (validity predicate only) for use with `Synthesize`; `Build` still requires a Repair and now returns a clear error (rather than panicking) when one is missing.

## [0.4.2] - 2026-09-23

Documentation and CI hygiene; no code or API change.

### Changed
- **README de-staled**: fixed a contradiction (Limitations claimed cyclic networks are rejected while the body documents `AllowMonotoneCycles`), added the missing `Embed`/compositionality section, removed a fabricated `Checked in:` line from the verification-report example (`Report.String` never prints it), and corrected the "Relationship to the Paper" cross-references (WFC/CC are axioms in §3, convergence is §5, the verification calculus is §10; added multi-source, monotone-cycle, and compositionality rows).

### Fixed
- CI lint (`errcheck`): `TestFederation_ReportAndAccessors` discarded `Build`'s error into `_`; flattened it to check the error. Test-only.

## [0.4.1] - 2026-09-23

### Changed
- **Relicensed from MIT to Apache License 2.0.** Adds an explicit patent grant (relevant for a library implementing novel, published algorithms) and a `NOTICE` file citing the underlying papers. Copyright held by Dayna Blackwell, Blackwell Systems. Still fully permissive; no code or API change; the papers remain CC-BY-4.0.

## [0.4.0] - 2026-09-23

Federation beyond trees: monotone cyclic networks (`AllowMonotoneCycles`) and compositional construction (`Embed`). Additive over v0.3.0.

### Added
- **Compositionality** (`Federation.Embed`): compose a sub-federation into a larger one — its component registries, internal morphisms, and resolvers are brought in as a unit, so a subsystem can be defined and verified independently (its own `Build`) and reused. Realizes the paper's compositional-collapse result: the composed federation runs as the flat convergent machine (a `FedState` holds one `State` per component, so no product state space is materialized), and `Build` re-checks the local conditions on the combined network. Cycles introduced across an embed boundary follow the usual rules (rejected unless `AllowMonotoneCycles` and monotone).
- **Monotone cycles** (`Federation.AllowMonotoneCycles`): cyclic morphism networks are now supported when repair is monotone. By default the network must be acyclic; with this opt-in, `Build` instead requires every morphism/resolver to be **monotone** with respect to the componentwise order on variable values (verified per-node by finite enumeration), and `Normalize` computes the federated normal form by **Kleene iteration to the least fixed point** (reset shared components to ⊥, iterate repair to a fixed point) rather than a one-shot topological pass. This is the paper's *Monotone Convergence Despite Cycles* theorem (Knaster–Tarski + chaotic iteration): on ordered shared domains a monotone repair operator converges order-independently even on arbitrary cyclic graphs. Non-monotone cyclic networks (e.g. the negation counterexample) are rejected, as are cycles without the opt-in. State-based CRDTs are the compensation-free special case of this regime.

## [0.3.0] - 2026-09-23

Multi-source federation: the tree restriction is lifted to any acyclic network. A target with several sources declares a resolution operator that deterministically merges them — convergence guaranteed by the paper's *Federated Convergence with Resolution* theorem, whose preconditions gsm verifies exhaustively at build time. Additive over v0.2.0; single-source (tree) federations are unchanged.

### Added
- **Multi-source federation via resolvers** (`Resolver`, `Federation.Resolve`): a federation target may now have more than one source (an acyclic DAG, not just a tree). Such a target declares a `Resolver(dst, sources)` that deterministically merges its sources' states into its shared component (priority, AND/OR, most-restrictive, etc.) — a merge no single-authority morphism can express. Convergence is the paper's *Federated Convergence with Resolution* theorem (Section 8), which holds whenever the resolver is source-determined (R1) and validity-preserving (R2) — the multi-source generalization of the single-source M1 condition, with single-source authority as the special case. gsm certifies exactly those hypotheses: `Build` **exhaustively verifies** (over every reachable combination of valid source states) that the resolver writes only shared variables, satisfies R1, and satisfies R2 — the same verify-the-preconditions contract gsm applies to single-registry WFC/CC and tree-federation M1. A multi-source target without a resolver — or a resolver that violates R1/R2 (reads local state, can produce an invalid target, or writes non-shared variables) — is rejected. Single-source (tree) federations are unchanged.

## [0.2.0] - 2026-09-23

Federated registry networks: gsm now composes multiple registries connected by directed morphisms and proves the whole network converges (Section 8 of the paper), in addition to the single-registry model. Additive — no breaking changes to the single-registry API.

### Fixed
- **Export() file permissions**: Changed from 0644 (world-readable) to 0600 (owner-only)
- **State space overflow**: Added overflow guard before multiplication in Build() to prevent silent int overflow on large variable domains
- **Var ownership validation**: getRaw/setRaw now panic with a clear message if a Var from a different Machine is used on a State, preventing silent data corruption

### Added
- **Federated registries** (`Federation`, `MorphismBuilder`, `FedMachine`, `FedState`): compose multiple component registries connected by directed registry morphisms encoding cross-registry constraints, per §8 of *Normalization Confluence in Federated Registry Networks*. `FedMachine` applies the constructive two-phase normalizer ρ_Fed (Corollary 8.10) — normalize each component, then propagate shared components through morphisms in topological order — without ever materializing the product state space. Exposes `NewState`/`Of`/`Apply`/`Normalize`/`IsValid`. Directed morphisms give coordination-free conflict resolution via the authority argument (§8.3): a source registry deterministically fixes its targets' shared components.
- **Federated build-time verification**: `Federation.Build` refuses any network the theory proves cannot converge — each component must satisfy WFC + CC; the network must be a tree/forest (no cycles, Prop 8.13; at most one incoming morphism per registry — multi-source is rejected per Remark 8.15); component names must be distinct; and every morphism must satisfy M1 validity-preservation-under-overwrite (Prop 8.14, verified by finite enumeration over valid states) with a `Map` that writes only its declared `Shared()` variables. A `FedMachine` exists only if federated convergence is guaranteed — the federated analogue of gsm's single-registry contract.
- `FedMachine.ApplyNamed(state, registry, event)` and `FedMachine.Registries()`: name-keyed application and component enumeration, for event-sourced replay where a durable log holds `(registry, event)` strings rather than live `*Registry` handles. `ApplyNamed` returns an error (rather than panicking) on an unknown registry or event so a stale/corrupt log fails gracefully on reconstruction.
- **Worked federation example** (`ExampleFederation`): a runnable, godoc-rendered manufacturer→supplier catalog federation showing shared-vs-local state, the authority argument (a supplier delist is ignored while the manufacturer stays authoritative), and morphism propagation on publish. Verified by the test suite.
- **Partial synchronization** (`Projection`, `FedMachine.SharedProjection`, `FedMachine.Component`, `Machine.MergeProjection`): the distributed form of the constructive normal form (Corollary 8.10). Each node runs only its own component `Machine`, applies local events, and exchanges small shared-component messages (`ϕ_ij(σ_i)`) along tree edges — no node holds the full federated state. A target merges its parent's projection with `MergeProjection` (validity preserved by M1, no re-normalization needed). `Build` now also verifies **source-determinacy** — a morphism's shared image must depend only on the source, not the target's local state — so projections are well-defined; a `Map` that reads the target's local component is rejected. Proven equivalent to the centralized `FedMachine` by test.
- `State.TrySet()` — error-returning alternative to `Set()` for use with user input or external values
- Clearer panic message in `Set()` showing variable name and invalid value

### Tests
- **Confluence property tests**: an all-independent machine now proves the core convergence guarantee at full strength — every one of the 720 permutations of a 6-event multiset reaches an identical normal form (Theorem 5.4), plus 500 randomized-multiset shuffles and a 2000-step "normal form is always valid" (WFC) walk. Exercises both CC-verification paths (footprint-disjoint and brute-force).
- **Federation scale tests**: a 10-registry chain (a root flag propagates through all ten levels in one ρ_Fed) and a 6-registry branching tree (fan-out plus two-level depth) confirm federations handle many components and arbitrary tree shapes — no product state space is built.
- **Validation / guardrail tests**: panic paths (unknown event, invalid enum value, foreign `Var`, enum with <2 values, `Int` max<min, invariant/event builders missing their functions, `Independent` on an unknown event) and behavioral edges (`TrySet` valid/error, `SetInt` clamping, negative-min offset, guarded-out no-op, oversized-state-space build rejection, name accessors). Coverage 89.7% → 96.3%; suite is race-clean.

### Changed
- **Docs repositioned for federation**: gsm is no longer described as single-registry-only. The package doc comment (godoc landing text) now covers federation; README gains a "Federated Registries" section (morphisms, the authority argument, the extended build-time contract) plus intro/when-to-use/relationship-to-paper updates; THEORY.md §11.4 now records federation as implemented (Section 8), with multi-source as the remaining boundary. Corrected stale "Section 7 / not yet implemented" references (federation is Section 8 in the published paper).
- README: Added "Act like UDP, receive like TCP" tagline and CRDT positioning paragraph
- README: Trimmed quick example for scannability
- README: Documented `Set()` panic and `SetInt()` clamping behavior in Writing State section

## [0.1.5] - 2026-02-20

### Changed
- Fixed variable naming consistency in README code examples (b → r)
- Removed duplicate comment in Independence Declarations section

## [0.1.4] - 2026-02-20

### Changed
- Removed "The Problem" section from README
- Removed summary section from CONCEPTS.md

## [0.1.3] - 2026-02-20

### Changed
- **BREAKING**: Renamed `Builder` to `Registry` and `NewBuilder()` to `NewRegistry()`
  - The registry is the central authority that holds invariants, compensation rules, and events
  - This naming better reflects the conceptual model: the registry governs convergent state
  - Update your code: `gsm.NewBuilder(name)` → `gsm.NewRegistry(name)`
  - File renamed: `builder.go` → `registry.go`
- **API simplification**: `Independent()` now automatically switches to declared-only mode
  - No need to call `OnlyDeclaredPairs()` first
  - `OnlyDeclaredPairs()` still exists for explicitness but is no longer required
  - Old: `r.OnlyDeclaredPairs(); r.Independent("e1", "e2")`
  - New: `r.Independent("e1", "e2")` (auto-switches)
- Rewrote problem section in README for clarity

### Added
- CONCEPTS.md (541 lines) - foundational concepts, definitions, and glossary
- THEORY.md (836 lines) - mathematical foundations and proofs
- Strategic code comments for bitpacking operations and footprint calculus
- Cross-references between documentation files

## [0.1.2] - 2026-02-19

### Changed
- **BREAKING**: Renamed `InvariantBuilder.Over()` to `Watches()` for better clarity
- **BREAKING**: Renamed `InvariantBuilder.Check()` to `Holds()` to align with formal methods terminology
- **BREAKING**: Renamed `Builder.DeclaredIndependence()` to `OnlyDeclaredPairs()` for clearer semantics
- Expanded "CC" abbreviation to "Compensation Commutativity" in all user-facing messages and reports
- Updated all code examples and documentation to use new API

### Added
- New "Core Concepts" section in README explaining invariants, compensation, events, and independence
- Detailed explanations of WFC (Well-Founded Compensation) and CC requirements
- Examples showing footprint usage and event declaration patterns

## [0.1.1] - 2026-02-19

### Added
- CODEOWNERS file for automatic review assignments on pull requests

### Changed
- Improved documentation formatting

## [0.1.0] - 2026-02-18

### Added
- Initial release of governed state machines library
- Builder API for defining state machines with fluent interface
- State variable types: Bool, Enum, Int with finite domains
- Invariant declaration with footprint tracking and repair functions
- Event declaration with write sets, guards, and effect functions
- Build-time WFC verification via exhaustive state-space enumeration
- Build-time CC verification (CC1 and CC2) with counterexample generation
- Footprint-based optimization for disjoint event pairs
- Immutable Machine type with precomputed lookup tables
- O(1) runtime event application via Step table
- State normalization and validity checking
- Bitpacked state representation (uint64) for efficient table indexing
- Comprehensive verification reports with WFC depth and CC pair statistics
- Independence declarations for restricting CC checks to relevant pairs
- JSON export format for portable multi-language runtime support
- Full test suite covering WFC, CC, compensation, and failures
- Documentation with usage examples, API reference, and design rationale

[Unreleased]: https://github.com/blackwell-systems/gsm/compare/v0.13.0...HEAD
[0.13.0]: https://github.com/blackwell-systems/gsm/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/blackwell-systems/gsm/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/blackwell-systems/gsm/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/blackwell-systems/gsm/compare/v0.9.2...v0.10.0
[0.9.2]: https://github.com/blackwell-systems/gsm/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/blackwell-systems/gsm/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/blackwell-systems/gsm/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/blackwell-systems/gsm/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/blackwell-systems/gsm/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/blackwell-systems/gsm/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/blackwell-systems/gsm/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/blackwell-systems/gsm/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/blackwell-systems/gsm/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/blackwell-systems/gsm/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/blackwell-systems/gsm/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/blackwell-systems/gsm/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/blackwell-systems/gsm/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/blackwell-systems/gsm/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/blackwell-systems/gsm/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/blackwell-systems/gsm/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/blackwell-systems/gsm/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/blackwell-systems/gsm/releases/tag/v0.1.0
