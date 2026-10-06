# Verification

What `Build` checks, what its report says, what certifies a machine, and what that costs. For why
the two properties it checks (WFC and CC) imply convergence, see [Concepts](concepts.md#the-two-properties);
for the formal algorithm and its soundness, [Theory §9](theory.md#9-verification-algorithm).

## Contents

- [The proof behind the check](#the-proof-behind-the-check)
- [How Build verifies](#how-build-verifies)
- [Verification report](#verification-report)
- [What the extracted oracles check](#what-the-extracted-oracles-check)
  - [The oracle gate](#the-oracle-gate)
  - [Assurance levels](#assurance-levels)
  - [The rules oracle and its fragment](#the-rules-oracle-and-its-fragment)
  - [Federations are not oracle-gated](#federations-are-not-oracle-gated)
  - [Memory, process limits, and what the gate trusts](#memory-process-limits-and-what-the-gate-trusts)
  - [Cross-checks against the OCaml checkers](#cross-checks-against-the-ocaml-checkers)
- [Certificates: what gsm recomputes itself](#certificates-what-gsm-recomputes-itself)
- [Compositional verification](#compositional-verification)
- [Abstraction: check relationships, not values](#abstraction-check-relationships-not-values)
- [Compensation synthesis](#compensation-synthesis)
- [Performance](#performance)
- [Limitations](#limitations)

---

## The proof behind the check

The convergence theorem itself is **machine-checked**: an axiom-free Coq/Rocq proof (`Print Assumptions` reports "Closed under the global context") with CI that gates on it. See the [mechanized proof](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq) and the [regime field guide](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIMES.md) for when a governed network converges.

And gsm's own verification is **checked again by the proof itself, in-process**. When `Build`, `BuildOrSynthesize`, `SynthesizeWith` (and so `Synthesis.Machine`) or `BuildCompositional` succeeds in Go, the machine's tables go to the table oracle. That oracle is `check_fn` from the Coq/Rocq development (the table oracle over accessor functions, proven equal to `check_tables` and to the list oracle `check_fast`), generated as Go from the extraction and vendored in `internal/oracle`. It reads the machine's tables in place through accessors, so it copies nothing. The machine is returned only if the oracle certifies the tables; otherwise the build fails closed, with no machine. `Report.Assurance` says what certified a machine. A bug in gsm's Go verification therefore cannot hand you a machine whose tables do not converge. The extracted checkers can also re-certify a machine you export (`Machine.WriteConvergenceTables`, `Registry.WriteMachineAST`). See [`coq/goextract`](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq/goextract) and [What the extracted oracles check](#what-the-extracted-oracles-check).

---

## How Build verifies

### Build Time (Verification)

When you call `registry.Build()`:

1. **Enumerate state space** - All combinations of variable values (finite per variable; `BuildCompositional` enumerates per footprint component instead of the global product)
2. **Compute normal forms** - For every state, apply compensation until valid
3. **Verify WFC** - Compensation terminates and reaches valid states
4. **Build step table** - For every (event, state) pair, precompute the normal form after applying the event
5. **Verify CC** - For every independent event pair and every valid state, both orderings reach the same normal form (two step-table lookups per state; no pair is skipped)
6. **Oracle gate** - The table oracle generated from the Coq proof re-checks the normal-form and step tables; if it does not certify them, there is no machine (see [What the extracted oracles check](#what-the-extracted-oracles-check))

If verification passes, you get an immutable `Machine` with precomputed lookup tables.

If verification fails, you get a detailed report showing:
- Which events violate CC
- At which state the violation occurs
- The divergent traces (order 1 vs order 2)

Along the way `Build` also measures what the guarantee assumes about delivery (events that need
deduplication, undeclared pairs that need causal order) and reports it without failing; see
[Deployment](deployment.md#delivery).

### Runtime (O(1) Execution)

<!-- gocheck: check machine -->
```go
machine.Apply(state, "request_shipment")
```

This does **one table lookup**: `step[event_index][state_id]` returns the precomputed normal form (plus one `nf` lookup first when the input is an invalid state, restored from storage or built by hand, so every order of events starts from a valid state).

**No compensation logic runs at runtime.** All the complexity is resolved at build time. The tables and their layout are in [ARCHITECTURE.md](design/ARCHITECTURE.md#runtime-execution).

---

## Verification report

The `Report` returned by `Build()` shows (for the README's order machine):

```
Machine: order_fulfillment
  Variables: 4
  States: 24
  Events: 3

  WFC: PASS (max repair depth: 1)
  CC (Compensation Commutativity): PASS (3 pairs: 0 disjoint, 3 brute-force)

  Convergence: GUARANTEED
  Assurance: tables certified by the verified table oracle
  Rules oracle: not run: not a combinator machine (gsm: invariant "status_matches_facts" has no combinator AST (declare it with DeclInvariant to export))
```

**WFC (Well-Founded Compensation)**: Compensation terminates from every state. The report shows the maximum number of repair steps needed.

**CC (Compensation Commutativity)**: For every independent pair of events, both orderings reach the same normal form from every valid state (and from the zero state `NewState` returns). The report shows:
- **Disjoint pairs** - Skipped because the two events lie in different footprint components, after the footprint check (`BuildCompositional` only; always 0 for `Build`)
- **Brute-force pairs** - Checked exhaustively, state by state

**Assurance** and **Rules oracle**: what certified the machine, and why the rules oracle did not run (here, the rules are closures); see [Assurance levels](#assurance-levels). Every field is listed in [Reference](reference.md#report).

If verification fails, you get a counterexample. For the guarded-shipment draft ([Getting started](getting-started.md#why-not-guard-the-shipment-on-payment)), reduced to two flags:

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

A collection's report (`NewCollection(...).Build()`, [Getting started](getting-started.md#collections-one-template-every-key))
is its template's report with one more line under the convergence line:

```
  Convergence: GUARANTEED
  Verified by symmetry over ProductID (items independent; cutoff 1)
```

`Build` checked the template as one item, and every key of the collection runs it, so the result
holds for any number of keys (`Report.Symmetry`; the theorems are in [Theory §11.8](theory.md#118-keyed-collections-symmetry)).
The delivery lines then apply per key: `Delivery: exactly once per ProductID for ...`, and a
convergence line under causal delivery ends `on the same ProductID`.

---

## What the extracted oracles check

Two checkers extracted from the axiom-free Coq proof re-certify a machine independently of gsm's
Go verification.

### The oracle gate

**In-process, on every success path.** The table oracle also exists as Go,
generated mechanically from the proof's extraction. It is not written by hand: see
`internal/oracle/PROVENANCE`, and the proof repository's `coq/goextract`, which also checks the
generator's arithmetic against its Rocq definitions. Every success path runs it on the tables
gsm's verification produced:

- `Build`, and so `BuildOrSynthesize` when the rules converge as written;
- `SynthesizeWith`, and so `BuildOrSynthesize`'s synthesized path and `Synthesis.Machine`;
- `BuildCompositional`, once per footprint component, over that component's subspace.

If the oracle does not certify the tables (it rejects them, or it cannot check them), the call
returns an error and no machine, and `Report.OracleDisagreement` holds the oracle's error. For
rules that are deterministic, a disagreement points at a bug in gsm's verification or in the
oracle's generator, not at your rules. One cause does come from your rules: an impure rule (one
that reads a clock, a counter or other outside state) can give different results when it runs
again. `BuildCompositional` runs every rule again to build the component tables, so such a rule
can make the two checks see different tables.

### Assurance levels

`Report.Assurance` (and `Synthesis.Assurance`) records what certified the machine:

- `AssuranceOracleTablesAndRules` (`Build`): the table oracle certified the tables, and the rules
  oracle certified the machine from its combinator rules (below).
- `AssuranceOracleTables`: gsm's verification and the table oracle both certified the machine's
  tables; the rules oracle did not run, and `Report.RulesOracleSkipped` says why.
- `AssuranceOracleComponents` (`BuildCompositional`): the oracle certified every component's
  tables. That cross-component pairs commute rests on gsm's footprint check, which the oracle does
  not see; with combinator rules that check is exact.
- `AssuranceOracleComponentsTested` (`BuildCompositional` with `TrustClosureFootprints`, on a
  machine with closure rules): as above, but the footprint check for closures is a perturbation
  test, not exact.
- `AssuranceOracleRepresentatives` (`Build` on a registry declared with `Abstract`): the oracle
  certified the representative tables (steps land on valid states, and the checked pairs commute
  at every valid representative state). That this carries over to every value rests on gsm's
  syntactic check that the rules only compare and copy, which the oracle does not see
  ([Abstraction](#abstraction-check-relationships-not-values)).
- `AssuranceNone`: not certified (the build failed, or the oracle rejected the tables).

### The rules oracle and its fragment

**The rules oracle in the gate (`Build`).** After the table oracle, `Build` also runs the rules
oracle, `checkBuild` from the proof (`AstChecker.v`), generated as Go the same way. It reads the
machine's combinator rules (exactly what `WriteMachineAST` and `WriteDeclaredPairs` write), not its
tables: it re-derives every step by evaluating the expression trees, so it does not trust gsm's
tables at all. It runs when all of these hold, and `Report.RulesOracleSkipped` says which did not:
- every rule is a combinator (`DeclInvariant`, `DeclEvent`, `DeclEventGuarded`), so the machine
  has rules to read;
- its work is at most `RulesOracleMaxWork` (2^29). The work (`oracle.RulesCost`) is an upper
  bound on the steps the generated `checkBuild` takes, counted from the expression trees, the
  checked pairs (every pair when none is declared) and the repair depth (`Report.MaxRepairLen`).
  Per state it counts what the oracle evaluates there: normalizing the state (each repair step
  evaluates every invariant's predicate and one repair), and for each checked pair both events'
  guards and effects, each followed by a normalization, twice. So rule size, pairs, invariants
  and repair depth all count. Measured on the generated Go, the rules oracle takes at most about
  4 ns per step (the worst, 4.04 ns, on a wide Int domain; 0.7 to 3.2 ns elsewhere). Its memory is
  mostly the box, about states × (variables + 1) list cells: from about 100 bytes per state at 2
  variables to about 220 at 20, plus a few MB. Within the cap it adds at most about 2.2 s and
  under about 170 MB (the most measured within it: 160 MB, at 2^20 states of 10 four-valued Ints);
- the machine is inside the rules oracle's fragment, which two static checks on the rules decide:
  no expression can leave |2^31-1| (`bounded`), and no write can store a negative value into a
  two-valued variable with minimum 0 (`signSafe`; a gsm `Bool` stores value != 0 where the model
  clamps). The oracle has no result outside it.

If it runs and does not certify the machine (repair does not terminate, or a declared pair does
not commute), or gives no result, `Build` fails closed like the table oracle. `SynthesizeWith`
and `BuildCompositional` run the table oracle only: synthesized repairs are not rules, and a
component is not a registry.

**Out of process.** Either spelling of combinator rules ([Getting started](getting-started.md#declarative-rules-combinators)) can be cross-checked against the verified **rules oracle**: `WriteMachineAST` emits the machine as S-expressions and the OCaml `astchecker` (extracted from the axiom-free Coq proof in `normalization-confluence`) recomputes convergence straight from those rules. It also certifies the **CRDT-fragment classification** (a machine-checked `compensation_free` result: whether repair is ever needed), so a consumer can confirm from the rules whether a machine is a plain CRDT or a compensation-bearing governed machine. See `astoracle_test.go` (`GSM_AST_CHECKER`).

**Serializable fragment.** Only rules built from the combinator vocabulary can be exported to the oracle: variables over `min .. min+domain-1` (any `min`, negative included); the comparison predicates `Le`/`Lt`/`Eq`/`Ge`/`Gt`/`Ne` and boolean `And`/`Or`/`Not`; the transforms `Set`/`Add`/`Sub`; and events with an optional guard. Closure-based invariants and events cannot be serialized, so `WriteMachineAST` returns an error rather than emit something the checker would misread. gsm's own `Build` verification has no such restriction; the fragment is only the boundary of what the extracted oracle can independently re-certify. Why the grammar is mirrored once in Coq rather than per machine is in [Theory §9.7](theory.md#97-machine-checked-meta-theory).

### Federations are not oracle-gated

**`Federation.Build` and certificates.** These rebuild every component with `Build`, so each
component's tables are oracle-gated. The federation-level checks are gsm's Go code and are not
oracle-gated:
- the morphism condition M1, and R1/R2 for resolvers;
- the event-order checks C1 (`CrossOrderError`) and C2 (`SameTargetOrderError`);
- the projection-merging check XU (`FedReport.ProjectionSafe`, `ProjectionOrderError`), reported, and
  required only under `RequireProjectionSafe`;
- the acyclicity and topological order;
- the Kleene iteration of monotone cycles;
- a certificate's digest and morphism tables.

`FedReport.Assurance` says this for each built federation, and `FedReport.Checks` lists the
federation-level checks that ran; `FedReport.String()` prints both above the component reports.
For a federation with `EmbedCertified` subsystems, `FedReport.Runtime` states that each certified
sub executes its verified tables (or which targets run closures, and why).

This matches [CERTIFICATE-DESIGN.md](design/CERTIFICATE-DESIGN.md): an extracted federation oracle is planned separately.

### Memory, process limits, and what the gate trusts

**Memory and process limits.** The oracle reads `Build`'s own tables through accessors and copies
nothing; the gate adds only the renumbering of the in-domain states (about 12 MB at 2^20 states).
Measured with `BenchmarkBuild_WideFlags20` (2^20 states, 20 events, every pair), peak RSS of
three `Build` calls is about 330 MB with the gate and 240 MB without it (it was 2.1 to 2.4 GB when
the oracle took the tables as Coq lists).

Running out of memory, or out of goroutine stack, kills the process. That is not a fail-closed
error return: plan memory for the largest machines you build.

What the gate trusts:

- that the tables are what your rules compute (gsm runs your closures to produce them);
- Rocq's extraction;
- the generator;
- the Go toolchain.

CI regenerates `internal/oracle/oracle_gen.go` from the pinned proof commit, in the pinned prover
image, and requires the same bytes. Its cost grows with states times declared pairs (see [Performance](#performance)).

### Cross-checks against the OCaml checkers

gsm's CI also builds both extracted checkers as OCaml
binaries, from a pinned, hash-checked proof commit (`.github/oracle/`). It cross-checks every
machine the test suite passes to `Build` against them: the documented examples, the
property-tested machines, and 600 random combinator machines. There the oracle tests are required,
not skipped, and any disagreement with `Build` fails the run. Locally they run when
`GSM_CONVERGENCE_CHECKER` / `GSM_AST_CHECKER` point at built binaries. The rules oracle also runs
in-process, in `Build` (above).

Both checkers decide the property `Build` checks, so they agree with `Build` on every machine,
except that the rules oracle refuses some arithmetic it does not model (below). That property:
repair terminates from every state; and every pair of events declared `Independent` (every pair
when none is declared) commutes on every valid state and on the zero state `NewState` returns,
valid or not. The guarantee that follows is that event sequences differing only by the order of
declared-independent events reach the same state from those states.

- **Table oracle** (`checker`, input from `Machine.WriteConvergenceTables`): reads the step
  tables, the normal-form table and the declared pairs, and checks that every normal form and
  every step lands on a valid state and that every declared pair commutes on the valid states and
  the zero state, by enumeration, with no footprint shortcut. It trusts that the tables are what
  the rules compute (gsm produces them from your closures). Only `Build` machines have tables.
- **Rules oracle** (`astchecker`, input from `Registry.WriteMachineAST`, plus the declared pairs
  from `Registry.WriteDeclaredPairs`): recomputes every event step from the combinator rules and
  checks the same property, with no footprint shortcut. Without the pairs file it checks every
  pair, which is stronger, so its result then holds for any declaration. The pairs are a separate
  file so that `PolicyBytes` and `PolicyDigest` stay as they were (certificate digests and
  `PolicyIdentityDigest` do cover the declared pairs). Closure rules cannot be exported.
  Its arithmetic is gsm's
  (signed integers, signed comparisons, clamped writes). It refuses to certify two things it does
  not model: an expression that could exceed 2^31-1 in magnitude (Go's `int` wraps there on
  32-bit platforms), and a write that could store a negative value into a two-valued variable
  with minimum 0 (a `Bool` stores `value != 0`; the format does not say which variables are
  `Bool`s, so the rule covers every such variable).

Both reject the [guarded-shipment machine](getting-started.md#why-not-guard-the-shipment-on-payment). Neither shares the footprint assumption that
`Build`'s former shortcut relied on. How the differential test classifies a disagreement is in
[ARCHITECTURE.md](design/ARCHITECTURE.md#differential-testing-via-extracted-oracles).

---

## Certificates: what gsm recomputes itself

Independently of the oracles, gsm does not trust a stored
result. `Certificate.Verify` recomputes the certificate digest from the consumer's registries,
re-derives validity preservation (M1/R2), table completeness, input-port freeness, and
acyclicity (or monotonicity, for a cyclic `Monotone` certificate) from the certificate's morphism
tables, and rebuilds every component with `Build` (WFC and CC). When `Build` runs on a federation
containing `EmbedCertified` subsystems, it recomputes the digest, re-derives the same conditions
from the tables for the internal morphisms, re-verifies the live internal closures over every
valid source and target state, and rebuilds every certified component with `Build`. The machine then executes the tables it checked for the internal morphisms, not the closures. The certificate's recorded `Report` is never used for a decision.
All of this is gsm's Go code: it removes trust in stored results, not in the Go verifier.

How to make and embed a certificate is in [Federation](federation.md#composing-federations-embed-and-certificates).

---

## Compositional verification

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

// Rules written as Go closures need an explicit opt-in (see below):
m, rep, err = r.BuildCompositional(gsm.TrustClosureFootprints())
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
else (like the guarded `ship` in [Getting started](getting-started.md#why-not-guard-the-shipment-on-payment)) is rejected with a footprint violation naming the variable.
For combinator rules the check is syntactic and exact. A closure is opaque, so gsm tests it: from
every state of its component, it changes each outside variable, and each pair of outside
variables, to every other value and confirms the closure's result does not change. That catches
dependence on one or two outside variables (such as `paid && inStock`), but not a closure that
depends only on three or more outside variables jointly. Because of that gap, `BuildCompositional`
accepts closure rules only when you pass `gsm.TrustClosureFootprints()`, acknowledging that each
closure reads and writes only its declared footprint and is deterministic. Without the option, a
registry with any closure rule (`Holds`/`Repair`/`Apply`/`Guard`) is rejected with an error naming
the first one. With it, `Report.Assurance` is `AssuranceOracleComponentsTested`, whose text says
the footprint check for closures is a perturbation test, not exact. If your rules are closures
with wide guards, use `Build` (exact, no footprint assumption) or the combinator vocabulary, which
`BuildCompositional` accepts with no option.

**Trade-offs.** The returned `Machine` is *lazy*: it computes `Apply`/`Normalize` at runtime from
the rules instead of via a precomputed table lookup, and `Export` is unavailable (there are no
global tables to serialize). Each closure result is checked to be a state of the machine as it is
computed, and `Apply` panics if one is not. `Apply` and `Normalize` also panic, with an
explanation, if the repairs take more steps than the build verified any repair chain can (the sum
of each component's deepest chain): that only happens when a rule breaks its footprint or is not
deterministic, and the repairs could otherwise cycle forever. Preconditions: every invariant
declares its footprint and every event its write set (both automatic with the combinator
vocabulary), every event reads only what it writes, the zero state is valid, every variable's
range fits its bit field, and the machine fits in 64 bits of state. A closure whose footprint test
would take more than 2^28 closure calls (wide variables outside its footprint) is rejected up
front.

The theorem this rests on, and why its read-set hypothesis is essential, is
[Theory §8](theory.md#8-footprint-calculus).

---

## Abstraction: check relationships, not values

`Build` enumerates every value of every variable. When the rules only **compare and copy** values
("stock is below the shipment level", "copy the level into stock", "stock is at most the cap 5"),
they cannot tell apart two states whose values stand in the same order relative to each other and
to the constants the rules mention. Declare those constants with `Registry.Abstract`, and `Build`
checks a small set of representative values instead, with the result holding for every value:

<!-- gocheck: run -->
```go
r := gsm.NewRegistry("inventory")
stock := r.Int("stock", 0, 1_000_000)  // 2^60 states together: far too many to enumerate
shipA := r.Int("ship_a", 0, 1_000_000) // the level shipment A restocks to
shipB := r.Int("ship_b", 0, 1_000_000)

r.Rule("cap").Require(gsm.AtMost(stock, 5)).RepairWith(gsm.SetTo(stock, 5)).Add()
r.On("receive_a").OnlyIf(gsm.BelowVar(stock, shipA)).Does(gsm.Copy(stock, shipA)).Add()
r.On("receive_b").OnlyIf(gsm.BelowVar(stock, shipB)).Does(gsm.Copy(stock, shipB)).Add()

m, report, err := r.Abstract(5).Build() // 5 is the one constant the rules use
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}
fmt.Println(report.Abstraction)
// Verified by abstraction over stock, ship_a, ship_b (rules compare values only; constants {5}; 7 representatives)

s := m.NewState().SetInt(shipA, 3).SetInt(shipB, 900_000)
fmt.Println(m.Apply(m.Apply(s, "receive_a"), "receive_b").GetInt(stock)) // 5: received, then capped
```

**What Build checks.** With n variables and constants C, the representatives are C, the n integers
above each constant, and the n integers below the least one (0 to n-1 when there is no constant):
at most |C|(n+1)+n values, here 5 and 6, 7, 8 and 4, 3, 2. `Build` runs its usual checks over every
state built from them (7³ = 343 states here): WFC from every representative state, and CC for the
checked pairs at every valid one. The verified table oracle certifies those tables, and
`Report.Assurance` is `AssuranceOracleRepresentatives`. The theorems (normalization-confluence
`AbstractionCutoff.v`; [Theory §11.9](theory.md#119-integer-variables-abstraction)) say the result
over the representatives is the result over all integers: a pass is a guarantee for every value,
and a failure is a real failure, reported with a witness state. WFC transfers by `term_abs`. CC1 at the valid states, for the checked pairs, holds over every integer iff it holds at the valid representative states (normalization-confluence `AbstractionGsm.v`, `cc1_valid_abs`); with repair verified within K steps over the representatives, this is exactly the condition for every reordering of independent events from a valid state to reach the same state (`gsm_abs_exact`, through `Trace.run_tequiv`, the base of `check_tables_converges`). Because `Apply` normalizes its input first, runs from every integer state, the zero state included, reach the same state under every reordering of independent events (`gsm_abs_sound`; any permutation when every pair is checked, `gsm_abs_sound_all`).

**What it accepts.** `Build` checks, from the rules' combinator trees, that the registry is in the
fragment the theorems cover, and refuses it otherwise with an `*AbstractionError` naming the rule
and the reason (also in `Report.AbstractionRefused`). Nothing is checked after a refusal.

| Refused | Why |
|---|---|
| A rule written as a Go closure (`Event().Apply`, `Invariant().Holds`, a `Guard` closure) | gsm cannot inspect a closure, so it cannot show the rule only compares and copies. Write it with combinators (`On`, `Rule`, `DeclEvent`, `DeclInvariant`) |
| A literal that is not a declared constant (`Is(amount, 13)` without `Abstract(13)`) | An exact test against an undeclared value can pass on every representative and still diverge: `exact13_diverges`. Declaring 13 makes the check see it |
| Arithmetic (`Add`, `Sub`, `Inc`, `IncBy`, ...) in a guard, an invariant or a write | An order-pattern check can pass on a rule that adds and the machine still diverge: `triangle_diverges`. Use `Build` without `Abstract` over a bounded range |
| A `Bool` or `Enum` variable | The theorems are about integer variables. Use an `Int` whose values are declared constants |
| A copy into a narrower range, or a constant written outside its target's range | The write could saturate, which the theorems (over all integers) do not model |
| More than 64 bits of state, more than 2²⁰ representative states, or a constant within n of the int limits | The state must fit one machine word, the check must stay enumerable, and the representatives must not overflow |

**Ranges and wrap-around.** The theorems are about mathematical integers. The fragment has no
arithmetic, so a rule never computes a value: it can only write a value some variable already
holds or a declared constant. With the copy and constant checks above no write saturates, so
on every state within the declared ranges the machine computes exactly what the theorems model,
and nothing can wrap around. A representative may lie outside a variable's range (a range that
starts at the least constant has no room for the representatives below it). That does not weaken
a pass. A failure whose witness lies outside the ranges is reported as such
(`CCFailure.Abstract.InRange` is false, and the report says "outside the declared ranges"): it is a
failure over the integers, so abstraction cannot certify the machine, but the machine as declared
may still converge. For ranges small enough, `Build` without `Abstract` decides it.

**The machine.** The machine computes at run time from the rules rather than by table lookup (like
`BuildCompositional`'s), so `Export` and `WriteConvergenceTables` are unavailable. `Apply`
normalizes every invalid input before applying the event, the zero state included: abstraction
certifies CC at the valid states, not at the zero state, which `Build` without `Abstract` also checks.

**Delivery.** Idempotence transfers too: an event is idempotent at every valid integer state iff at every valid representative state (`idem_valid_abs`; for the runtime step from every integer state, `idem_runtime_abs`). So `NotIdempotent` is computed from the representatives and is exact for every value: deduplicate exactly the listed events.

**With collections.** A collection template may declare `Abstract`: the report then carries both
lines, `Verified by symmetry over ...` and `Verified by abstraction over ...`, and the result holds
for every value at every key. Federations do not accept a component declared with `Abstract`.

**Not covered yet.** Rules that add or subtract (a wallet whose balance changes by an amount) need
the linear route of the theory, where the conditions become integer-arithmetic formulas checked by
an SMT solver; gsm does not implement it ([Roadmap 1b](ROADMAP.md#1b-abstraction-check-relationships-not-values)).

---

## Compensation synthesis

You don't have to *design* the compensation. Declare the invariants (what "valid" means) and
the events, **omit `Repair`**, and gsm will **generate** a convergent compensation, or prove
none exists.

<!-- gocheck: check -->
```go
r := gsm.NewRegistry("order")
// ... variables, invariants (Holds only, no Repair), events ...

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

- **Convergent**: a repair was found; `Machine()` is ready, `Repairs()` shows it.
- **Impossible** (`Exhaustive`, not convergent): no compensation converges. `Witness()` gives
  a concrete reason (e.g. two events reaching *distinct already-valid states*: no repair can
  reconcile them), so you know to redesign the **events**.
- **Undetermined** (search budget hit): none found within budget; one may exist. (A SAT/SMT
  backend would settle these.)

Impossibility is the **ceiling** of the compensation regime, the dual of the CRDT floor. A
`compensation_free` machine sits at the bottom (it is a CRDT: repair is never needed); an
`Impossible` witness marks the top, where no repair converges and only coordination (consensus,
locking) can. gsm certifies both edges of what compensation reaches: the floor by embedding (every
CRDT is a gsm machine), the ceiling by counterexample (the witnessed critical pair). The caveat on
"ceiling" is in [Theory §10.6](theory.md#106-the-convergence-lattice-floor-and-ceiling).

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
LLM-authored policy: describe the rules, get a convergent machine with a proof.

`BuildOrSynthesize` builds the rules as written and falls back to synthesis only when the
compensation is what failed. When it returns a synthesized machine, that machine does not run your
invariants' `Repair` functions: the returned `*Synthesis` is non-nil, `Synthesis.Substituted` is
true, `Synthesis.BuildError` holds the `Build` error that triggered the fallback, and
`Synthesis.String()` opens with `REPAIR SYNTHESIZED`.

---

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

**The oracle gate's cost.** The table oracle re-checks the tables after gsm's own checks, reading them in place through accessors (a closure call per read), so its cost per state and declared pair is higher than `Build`'s own CC check. Measured locally on an Apple M-series laptop (Build with the gate vs without):

| Machine | Without the gate | With the gate |
|---------|------------------|---------------|
| 24 states, 3 pairs | 0.32 ms | 0.35 ms |
| 1,024 states, 10 events, 45 pairs | 2.2 ms | 3.0 ms |
| 2²⁰ states, 20 events, 10 declared pairs (oracle alone) | | ~0.3 s |
| 2²⁰ states, 20 events, every pair (190) | 0.76 s | 5.6 s (peak RSS about 330 MB, against 240 MB without the gate) |

The rules oracle's own cost, per step of its work (`oracle.CheckRules` alone, Apple M-series;
`GSM_RULES_COST=14 go test -run TestRulesOracleCostPerStep -v .`). They are large, some above the
cap (2^29 steps), so that the times are measurable; within the cap the most it adds is about 4 ns
x 2^29, 2.2 s (the worst per step, 4.04 ns, was measured on a wide Int domain in the review of #17):

| Combinator machine | Work (steps) | Rules oracle | Per step | Live heap |
|---------|------------------|---------------|---------------|---------------|
| 2²⁰ states, 1 event | 9.3 × 10⁸ | 1.65 s | 1.8 ns | 203 MB |
| 2¹⁴ states, 20 events, every pair (190) | 2.3 × 10⁹ | 7.4 s | 3.2 ns | 6 MB |
| 2 states, 280 events, every pair | 8.7 × 10⁷ | 0.25 s | 2.9 ns | 6 MB |
| 3⁵ states, 4 events with 1,400-node guards and effects, every pair | 8.3 × 10⁷ | 0.14 s | 1.7 ns | 4 MB |
| 2¹³ states, repair depth 8191 | 1.5 × 10¹⁰ | 30 s | 2.0 ns | 4 MB |
| 2¹⁴ states, 200 invariants | 1.0 × 10⁸ | 0.10 s | 1.0 ns | 3 MB |
| 2¹⁴ states, 100 writes in an event | 5.9 × 10⁸ | 1.2 s | 2.0 ns | 6 MB |
| 2¹⁴ states, guards of 100 terms | 1.7 × 10⁸ | 0.12 s | 0.7 ns | 4 MB |

Within the cap, `Build` with both oracles takes 0.70 s on 2¹⁹ states with one event (work
4.4 × 10⁸, 0.55 GB allocated in all) and 1.6 s on 2¹² states with 20 events and every pair
(5.3 × 10⁸). 2²⁰ states with one event (9.3 × 10⁸) and 2¹³ states with 20 events and every pair
(1.1 × 10⁹) are above it: the table oracle alone certifies them.

For a large machine, declaring only the pairs that need to commute (`Independent`) keeps the gate fast.

### Runtime

Event application: **O(1)** - single array lookup, no computation.

Memory: One `uint64` per state for normal form table, plus one `uint64` per (event, state) pair for step table. For 1M states × 10 events = ~80MB.

---

## Limitations

- **Finite variable domains** - Each variable's domain must be finite (no arbitrary strings or lists). Integer variables whose rules only compare and copy can have ranges far too wide to enumerate, through `Abstract` (see [Abstraction](#abstraction-check-relationships-not-values)). This is a per-variable constraint, not a global ceiling: `BuildCompositional` certifies machines whose product state space is astronomically large, as long as each footprint component is small
- **Build-time cost** - Global `Build` enumerates the state space and hard-errors above 2²⁰ (~1M) states (a fixed cap, not configurable), so it does not degrade gracefully past that wall; use `BuildCompositional` for machines that decompose into small footprint components (see [Compositional verification](#compositional-verification)), where cost scales with the largest component rather than the whole machine
- **Verification requires Go** - Runtime portable via JSON export ([Reference](reference.md#multi-language-runtime)), but verification engine is Go-only
- **Federation** - Tree networks, multi-source acyclic DAGs (resolution operators), and monotone *cyclic* networks are all covered by the paper's proofs (Section 8). gsm establishes the theorems' preconditions (morphism M1, resolver R1/R2, the event-order conditions C1/C2, monotonicity) by exhaustive build-time verification. *Non-monotone* cycles are rejected unless coordinated (`BuildCoordinated`), and multi-source targets without a resolver are rejected. On monotone cycles, event interleavings are certified by the same C1/C2 checks (`cyc_check_gc_lfp`, see [monotone cycles](federation.md#escape-hatch-2-monotone-cycles)). C1/C2 certify the `FedMachine` model; a deployment that merges projections on separate nodes needs XU, which `Build` reports (`FedReport.ProjectionSafe`) and requires only under `RequireProjectionSafe`, and which is certified only on acyclic networks without multi-source targets ([Deployment](deployment.md#projection-deployments)). The federation-level checks are gsm's Go code, not oracle-gated
- **No runtime monitoring** - Once built, machine is immutable (cannot add events/invariants dynamically)
- **Synthesis is worst-case exponential** - CC synthesis is NP-hard; the backtracking search prunes hard but can return UNDETERMINED beyond its budget (a SAT/SMT encoding would extend the reach). `Optimal` (branch-and-bound) is more expensive than the default first-found repair
