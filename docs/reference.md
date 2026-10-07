# Reference

The fields, errors, options and file formats you meet when using gsm. The authoritative API
documentation is the godoc at
[pkg.go.dev/github.com/blackwell-systems/gsm](https://pkg.go.dev/github.com/blackwell-systems/gsm);
this page is the map. Field descriptions here are summaries of the godoc.

## Contents

- [Build entry points and options](#build-entry-points-and-options)
- [Report](#report)
- [FedReport](#fedreport)
- [Synthesis](#synthesis)
- [MigrationReport](#migrationreport)
- [Errors](#errors)
- [Names are unique within a registry](#names-are-unique-within-a-registry)
- [Multi-language runtime](#multi-language-runtime)
- [Exports for the extracted checkers](#exports-for-the-extracted-checkers)
- [Policy as a portable artifact](#policy-as-a-portable-artifact)
- [Running the test suite](#running-the-test-suite)

---

## Build entry points and options

| Call | Returns | What it does | Explained in |
|---|---|---|---|
| `Registry.Build()` | `*Machine, *Report, error` | Enumerates the state space (at most 2²⁰ states), verifies WFC and CC, runs the oracle gate. A larger registry of combinator rules that splits into footprint components is checked per component instead (a lazy machine; `Report.Compositional`); `Report.GlobalReason` says why a machine was enumerated whole | [Verification](verification.md#how-build-verifies), [Compositional](verification.md#compositional-verification) |
| `Registry.BuildCompositional(opts...)` | `*Machine, *Report, error` | Verifies each footprint component on its own, at any size; returns a lazy machine. Option: `TrustClosureFootprints()` to accept closure rules | [Verification](verification.md#compositional-verification) |
| `Registry.Synthesize()` / `SynthesizeWith(opts...)` | `*Synthesis, error` | Generates a convergent compensation or proves none exists. Options: `Prefer(cost)`, `Optimal()` | [Verification](verification.md#compensation-synthesis) |
| `Registry.BuildOrSynthesize(opts...)` | `*Machine, *Synthesis, error` | `Build`, falling back to synthesis when the compensation is what failed | [Verification](verification.md#compensation-synthesis) |
| `NewCollection[K](over, template).Build()` | `*CollectionMachine[K], *Report, error` | Builds the template with `Build` (every check, the oracle gate; checked as a whole, never per footprint component) and returns a machine that runs it at every key of type `K`; sets `Report.Symmetry` | [Getting started](getting-started.md#collections-one-template-every-key) |
| `Registry.Abstract(constants...)` | `*Registry` | Declares the constants the rules compare and copy; `Build` then verifies by abstraction, over the representative values instead of every value, and returns a lazy machine; sets `Report.Abstraction`. Refuses a registry outside the fragment with an `*AbstractionError` | [Verification](verification.md#abstraction-check-relationships-not-values) |
| `Registry.Independent(e1, e2)` / `OnlyDeclaredPairs()` | `*Registry` | Declared-only mode: certify only declared pairs, report the rest | [Getting started](getting-started.md#independence-declarations), [Deployment](deployment.md#causal-order-for-undeclared-pairs) |
| `Federation.Build()` | `*FedMachine, *FedReport, error` | Builds every component, then checks M1 (R1/R2), C1, C2, acyclicity, and reports XU | [Federation](federation.md) |
| `Federation.AllowMonotoneCycles()` | `*Federation` | Accept cycles whose morphisms and resolvers are all monotone | [Federation](federation.md#escape-hatch-2-monotone-cycles) |
| `Federation.RequireProjectionSafe()` | `*Federation` | Make XU a build requirement | [Deployment](deployment.md#projection-deployments) |
| `Federation.DiagnoseCycle()` | `*CycleDiagnostic, error` | Walk a rejected cycle: settle, orbit, or obstruction | [Federation](federation.md#this-is-exactly-what-diagnosecycle-computes) |
| `Federation.CoordinationPlan()` / `BuildCoordinated(plan)` | `[]CoordinationPoint` / `*FedMachine, *FedReport, error` | Name the edges to coordinate externally; build the residual | [Federation](federation.md#escape-hatch-3-coordinate-the-obstruction) |
| `Federation.Embed(sub)` | `*Federation` | Reuse a sub-federation, re-verified in the whole | [Federation](federation.md#composing-federations-embed-and-certificates) |
| `Federation.Certify(ports...)` / `EmbedCertified(sub, cert)` / `Certificate.Verify(comps)` | | Package a verified sub-federation, embed it pinned to the certificate, re-check it as a consumer. `Port{Registry, Var}` declares an input port | [Federation](federation.md#composing-federations-embed-and-certificates) |
| `CheckMigration(from, to, migrate, events, opts...)` | `*MigrationReport, error` | Classifies a change from one registry to another before you deploy it: safe online, safe behind a barrier, unsafe with a witness, or unknown. Enumerates the states runs reach (at most 2²⁰ per side); the registries need not pass `Build`. Option: `MigrationFrom(states...)` to check from given states instead of the zero state | [Deployment](deployment.md#changing-a-running-system) |

Opt-ins belong to the federation they are called on: an embedded sub's `AllowMonotoneCycles` or
`RequireProjectionSafe` does not apply to the parent.

A `CollectionMachine[K]` has `NewState()`, `Apply(s, key, event)` (updates the `*CollectionState[K]`
in place and returns the key's new item `State`), `Over()` and `Item()` (the template's `Machine`).
A `CollectionState[K]` has `Item(key)` (the template's `NewState` for a key no event has reached),
`Keys()`, `Len()` and `Clone()`. `Apply` panics on an unknown event, a nil state, or a state of
another collection machine.

---

## Report

`Report` is what `Registry.Build`, `BuildCompositional`, and each federation component return.
`Report.String()` prints it ([example](verification.md#verification-report)).

| Field | Meaning |
|---|---|
| `Name`, `StateCount`, `VarCount`, `EventCount` | The machine and its size |
| `WFC`, `MaxRepairLen` | Whether repair terminates from every state; the longest compensation chain (on the per-component path, the sum of the components' deepest chains, which is the machine's repair bound) |
| `CC`, `PairsTotal`, `PairsDisjoint`, `PairsBrute`, `CCFailure` | Whether every checked pair commutes; how many pairs needed no check because their events lie in different footprint components (per-component path only; 0 when the whole state space was enumerated) or were checked exhaustively; the counterexample (`*CCFailure`) when CC failed |
| `PairsUndeclared`, `CausalOrderRequired` | In declared-only mode: how many undeclared pairs were also checked, and the ones that do not commute (each a `CCFailure` with a witness), which must be delivered in causal order ([Deployment](deployment.md#causal-order-for-undeclared-pairs)) |
| `NotIdempotent` | Events whose second application changes the state: deduplicate their redeliveries ([Deployment](deployment.md#duplicates-and-redelivery)) |
| `Saturations` | Rules whose write was clamped into a variable's range on some state `Build` ran them on (`Saturation{Rule, Var, States}`). Clamping is verified semantics, but an invariant meant to catch the overflow never sees it |
| `Coordinated` | On a federation component built with `BuildCoordinated`: the removed edges into it, whose shared variables are external inputs. gsm does not check that coordination |
| `Components`, `MaxComponentStates`, `FootprintChecked`, `FootprintViolation` | Per-component path (`Build` on a large combinator registry, or `BuildCompositional`): the number of footprint components, the largest subspace enumerated, whether footprint conformance held, and the violation that rejected the machine (WFC and CC were then not evaluated) |
| `Compositional` | Per-component path: `*CompositionalReduction{Components, Vars, LargestStates, StatesChecked, GlobalStates, CrossPairs, RepairBounds, FootprintsExact}`, printed as `Verified compositionally: 2 components (largest 131,072 states; 131,136 states checked instead of 8,388,608); 6 cross-component pairs need no check (disjoint footprints, reads included); footprints checked exactly (combinators)` (`Checked compositionally: ...` on a failure). `StateCount` is then 0, and `Assurance` is `AssuranceOracleComponents` (`AssuranceOracleComponentsTested` with closures). `FootprintsExact` is false when closure footprints were tested by perturbation ([Verification](verification.md#compositional-verification)) |
| `GlobalReason` | `Build` enumerated the whole state space: why it did not check per component (small enough to enumerate, a closure rule, one component, an invalid zero state, a component above 20 bits, a collection template or federation component), printed as `Checked globally: ...`. Empty on the per-component and abstraction paths |
| `DomainViolation` | A rule returned something that is not a state of the machine; verification stopped there ([Getting started](getting-started.md#rules-must-return-a-state-of-their-own-machine)) |
| `Assurance` | What certified the machine ([Assurance levels](verification.md#assurance-levels)); `AssuranceNone` unless a machine was returned |
| `OracleDisagreement` | gsm's verification passed but the table oracle did not certify the tables; there is no machine |
| `RulesOracleSkipped` | Why the rules oracle did not run (no combinator rules, above `RulesOracleMaxWork`, outside its fragment, or checked per component) |
| `Abstraction` | `Build` by abstraction (`Registry.Abstract`): `*AbstractionReduction{Over, Constants, Cutoff, Representatives, States}`, printed as `Verified by abstraction over stock (rules compare values only; constants {5}; 7 representatives)`. `StateCount` is then the number of representative states, and `Assurance` is `AssuranceOracleRepresentatives`. Also set on a WFC or CC failure found that way ([Verification](verification.md#abstraction-check-relationships-not-values)) |
| `AbstractionRefused` | The registry declared `Abstract` but is outside the fragment; holds the `*AbstractionError` text, and nothing was checked |
| `Symmetry` | On a collection's report: `*SymmetryReduction{Over, Cutoff}`, printed as `Verified by symmetry over <Over> (items independent; cutoff 1)`. The template was checked as one item and the result holds at every key; `NotIdempotent` and `CausalOrderRequired` then apply per key ([Getting started](getting-started.md#collections-one-template-every-key)) |

---

## FedReport

`FedReport` is what `Federation.Build` and `BuildCoordinated` return. `FedReport.String()` prints the
federation-level lines above the component reports.

| Field | Meaning |
|---|---|
| `Name`, `Components`, `Edges` | The federation, each component's `*Report`, and the number of morphism edges |
| `Assurance` | What certified the federation as a whole: components are oracle-gated, the federation-level checks are gsm's Go code ([Verification](verification.md#federations-are-not-oracle-gated)) |
| `Checks` | The federation-level checks that ran and passed, in order, ending with the projection line |
| `ProjectionSafe`, `ProjectionLine`, `ProjectionWitnesses` | Whether distributed projection merging is certified (XU), the one-line result, and one `*ProjectionOrderError` per failing target ([Deployment](deployment.md#projection-deployments)) |
| `Runtime` | For each `EmbedCertified` sub: whether its internal targets execute the certificate's tables, or which run closures and why |

`CycleDiagnostic` (from `DiagnoseCycle`) has `Cycle`, `Shared`, `Converges`, `Orbit`, `AllSeeds`,
`Seeds`, `SectionExists`, `Section` and `Obstructed()`; [Federation](federation.md#this-is-exactly-what-diagnosecycle-computes)
explains how to read them. `CoordinationPoint` has `Src`, `Dst`, `Shared` and `Authority`.

---

## Synthesis

| Field or method | Meaning |
|---|---|
| `Convergent`, `Exhaustive` | A convergent compensation was found; the search completed (`!Convergent && Exhaustive` means provably impossible) |
| `Nodes`, `Cost` | Backtracking nodes explored; total cost of the repair (minimum when `Optimal` and `Exhaustive`) |
| `Assurance` | `AssuranceOracleTables` when the table oracle certified the synthesized tables |
| `Substituted`, `BuildError` | Set by `BuildOrSynthesize` when it returned the synthesized machine instead of the rules as written, and the `Build` error that caused the fallback |
| `Machine()`, `Repairs()`, `Witness()`, `String()` | The certified machine (nil unless convergent and certified), the repair per invalid state, the impossibility witness, the readable report |

---

## MigrationReport

`MigrationReport` is what `CheckMigration` returns; `MigrationReport.String()` prints it
([example](deployment.md#changing-a-running-system)). `migrate` is a `Migration`,
`func(old, blank State) State`: it builds a state of the new registry (`blank` is its zero state)
from a state of the old one. `events` maps an old event name to the new event an in-flight
occurrence becomes; an old event it does not name becomes the new event of the same name.

| Field | Meaning |
|---|---|
| `From`, `To` | The two registries' names |
| `Outcome` | `MigrationSafeOnline`, `MigrationSafeBehindBarrier`, `MigrationUnsafe` or `MigrationUnknown` (the zero value), printed as `SAFE ONLINE`, `SAFE BEHIND A BARRIER`, `UNSAFE`, `UNKNOWN` |
| `Reason` | Why, in one line |
| `Starts`, `StatesFrom`, `StatesTo` | The starts checked (the old registry's zero state unless `MigrationFrom` gave others), the old registry's states reachable from them, and the new registry's states reachable after a switch |
| `Conditions`, `Condition(key)` | Every condition evaluated (`MigrationCondition{Key, Mode, Desc, Evaluated, Decided, Holds, Detail}`), with where it fails. Keys: online, `MigrationPermBStart` (the new registry converges from the migrated start) and `MigrationDS1` (in-flight events commute with the switch); barrier, `MigrationPermBEvery` (the new registry converges from every migrated reachable state), `MigrationPermA` (the old registry converges from the start), `MigrationFaithful` (the migration is injective on the reachable states) and `MigrationAmodM` (two orders of the same old events never migrate apart; searched only when the change is not safe online, `PermB-every` holds and `PermA` fails, and `Decided` only when a witness is found, which under `Faithful` is always) |
| `Failed` | The key of the condition that decided a negative outcome; empty when safe online |
| `Faithful`, `Collision` | Whether the migration is injective on the reachable states, and two old states with one image (`*MigrationCollision{State1, State2, Image}`) when it is not |
| `LiveWitness` | Why the change is not safe online: a `*MigrationWitness{Condition, Start, Run1, Run2, Result1, Result2, Barrier}`, two runs of the same events from `Start` ending in different states of the new registry. A `MigrationRun{Before, InFlight, After}` applies `Before` under the old registry, switches with `InFlight` still in flight, then applies `After` (the in-flight events' translations first) under the new one |
| `BarrierWitness` | Why the change is unsafe: two runs that both switch at a barrier and diverge. Set only when `Outcome` is `MigrationUnsafe` |
| `Theorems` | The normalization-confluence theorems behind the outcome ([Theory §11.11](theory.md#1111-changing-a-running-system-migration)) |
| `SearchStopped` | Set when the search for a `MigrationAmodM` witness stopped at the state limit |

`CheckMigration` returns an error, not a report, for a registry declared with `Abstract` or with
`Independent` pairs, more than 64 bits of state, an invariant without a `Repair`, an event map
naming an event the registries lack, a migration or rule returning a state of another registry,
a repair that does not terminate on a state a run reaches, or a side that reaches more than 2²⁰
states.

---

## Errors

| Error | Returned when | Explained in |
|---|---|---|
| `*CCFailure` (in `Report.CCFailure`) | Two events, a state, and the two results of a CC failure | [Verification](verification.md#verification-report) |
| `*AbstractionError` | `Build` on a registry declared with `Abstract` that abstraction does not cover: `Rule` names the rule (or variable) and `Reason` says why (a closure rule, an undeclared literal, arithmetic, a Bool or Enum variable, a write that could saturate, a domain too large) | [Verification](verification.md#abstraction-check-relationships-not-values) |
| `CCFailure.Abstract` (`*AbstractWitness`) | A CC failure found by abstraction: the witness and both results as integer values, and `InRange`. Outside the declared ranges, `State`, `Result1` and `Result2` are zero | [Verification](verification.md#abstraction-check-relationships-not-values) |
| `*CrossOrderError` | C1 fails: a target event and a source-driven change of its shared component diverge | [Federation](federation.md#event-order-across-registries-c1-and-c2) |
| `*SameTargetOrderError` | C2 fails: two target events diverge once the morphism repair runs between them | [Federation](federation.md#event-order-across-registries-c1-and-c2) |
| `*ProjectionOrderError` | XU fails at a target (reported in `ProjectionWitnesses`; returned under `RequireProjectionSafe`). Unwraps to `ErrProjectionNotCertified` | [Deployment](deployment.md#projection-deployments) |
| `ErrProjectionNotCertified` | Wrapped by every `RequireProjectionSafe` failure: an XU witness or a structural reason (a cycle, a multi-source target, a check space over the cap). Test with `errors.Is` | [Deployment](deployment.md#projection-deployments) |
| `ErrStaleProjection` | Wrapped by `MergeProjectionAfter` for a projection whose `Version` is not newer than the last one applied from that edge | [Deployment](deployment.md#projection-deployments) |

The federation errors are static: their witnesses quantify over every valid state, so a witness
can be a state a given run never reaches.

---

## Names are unique within a registry

An event is addressed by name after it is declared (`Apply`, `Independent`, `ApplyNamed`, replay logs), and a variable by name in certificate tables, input ports, and shared projections. `Build` returns `gsm: registry "orders": duplicate event name "ship"` (or `duplicate variable name`, or `enum "status" has duplicate label "paid"`) for a registry that declares two events, two variables, or two labels of one enum with the same name, and so do `BuildCompositional`, `Synthesize` (`Synthesis.Machine` is the machine as synthesized, so a later declaration does not reach it), `BuildOrSynthesize`, `Federation.Build`, `Certify`, `Certificate.Verify`, and the exports for the checkers (`WriteMachineAST`, `PolicyBytes`, `PolicyDigest`, `WriteDeclaredPairs`). `Build`, `BuildCompositional`, `Synthesize`, `Federation.Build` and `Certify` also reject a registry that a rule or morphism closure changes while they verify it (a `Holds` that declares an event, say), so a declaration cannot slip in after their check. Invariant names only label diagnostics and need not be unique.

---

## Multi-language runtime

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
- Verification metadata, including (format version 2) the event pairs CC was checked for
  (`verification.pairs`, by event name) and whether that is every pair (`verification.all_pairs`).
  Pairs outside that set are not guaranteed to commute. Version 2 only adds fields to version 1.

A full example file is in [ARCHITECTURE.md](design/ARCHITECTURE.md#export-format). Only machines
`Build` enumerated export: a lazy `BuildCompositional` machine, or one `Build` verified by
abstraction, has no global tables.

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
        if state != 0:
            state = self.nf[state]  # normalize an invalid input first, as Machine.Apply does
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

The `nf` lookup before the step matters: CC was verified only from valid states and the zero
state, so a runtime that skips it can disagree on two orders of the same events from an invalid
state (one restored from storage, say).

**Verification complexity** stays in Go. **Runtime simplicity** is universal.

---

## Exports for the extracted checkers

| Export | Input to | What it contains |
|---|---|---|
| `Machine.WriteConvergenceTables(path)` | the table oracle (`checker`) | The step tables, the normal-form table, and the declared pairs |
| `Registry.WriteMachineAST(w)` | the rules oracle (`astchecker`) | The combinator rules as S-expressions; an error for closure rules |
| `Registry.WriteDeclaredPairs(w)` | the rules oracle | The declared `Independent` pairs, as a separate file |

What each oracle checks is in [Verification](verification.md#cross-checks-against-the-ocaml-checkers).

---

## Policy as a portable artifact

`Registry.PolicyBytes` returns the canonical serialization of the combinator rules (the S-expressions `WriteMachineAST` emits) and `Registry.PolicyDigest` a stable, domain-separated SHA-256 over them, so a policy can be named and anchored independently of who built it (a rule written with the sugar surface digests identically to the equivalent primitive combinators). The digested bytes are exactly the oracle's input, so the anchored digest and the re-checked artifact cannot diverge. That format addresses variables and events by position, so `PolicyDigest` does not cover the names the rules are addressed by: two registries that differ only in a variable, event or enum-label name, a variable's kind, or the declared `Independent` pairs have the same `PolicyDigest`. `Registry.PolicyNames` serializes those, and `Registry.PolicyIdentityDigest` is a fingerprint over the rules and the names together (certificate digests cover the names too). Moving the names into the serialized format, and so into `PolicyDigest`, is planned before 1.0. This is the interchange contract an external audit layer pins to: it commits the digest in a log and hands the same bytes to `astchecker`. `State.Digest` is the companion primitive for the resulting state: a stable, domain-separated hash over a state's packed value (meaningful because the policy pins the layout), so a layer that anchors a policy can also attest which state an action produced and reproduce it by replaying the same events over a reference build.

The version strings are the constants `PolicyFormatVersion`, `PolicyIdentityVersion` and `StateDigestVersion`.

---

## Running the test suite

```bash
go test -v
```

Tests cover:
- WFC verification (termination, cycles, depth)
- CC verification (exhaustive pairs, compositional footprint components, failures), including a property test against brute-force enumeration of orderings
- Every Go block in the docs (`TestDocSnippets`: blocks marked `run`, like the order-fulfillment example, are executed, so an example whose machine does not Build fails CI)
- Event order independence
- Compensation behavior
- State encoding/decoding

The oracle tests run when `GSM_CONVERGENCE_CHECKER` / `GSM_AST_CHECKER` point at built checker
binaries; CI requires them ([Verification](verification.md#cross-checks-against-the-ocaml-checkers)).
