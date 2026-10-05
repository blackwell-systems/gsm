# Architecture

This document explains the internal design of gsm, how convergence verification works, and why runtime event application
is O(1) with zero compensation overhead.

For other documentation:
- **Usage guide**: [README.md](README.md)
- **Conceptual introduction**: [CONCEPTS.md](CONCEPTS.md)
- **Mathematical foundations**: [THEORY.md](THEORY.md)

## Overview

gsm separates verification (build-time) from execution (runtime):

- **Build time**: Exhaustive state-space enumeration verifies WFC and CC properties
- **Runtime**: O(1) table lookups apply events and return normal forms

All compensation is precomputed during `Build()`. The runtime `Machine` contains only lookup tables.

There is also a build-time *synthesis* path (`synthesis.go`, `Registry.Synthesize`): the
inverse of verification. Instead of checking a compensation you supplied, it searches — via
backtracking with forward-checking — for a normal-form map on invalid states that satisfies CC,
generating a convergent compensation (or proving none exists, with a witness). It shares the
state-space enumeration and CC machinery with `Build`.

Two later sections cover paths that extend this core: [Compositional
Verification](#compositional-verification-buildcompositional) verifies machines whose global
state space is too large to enumerate by checking each footprint component independently, and
[Differential Testing via Extracted Oracles](#differential-testing-via-extracted-oracles)
re-certifies a built machine against checkers extracted from an axiom-free Coq proof.

## State Representation

### Bitpacked Encoding

States are stored as `uint64` values with variables bitpacked sequentially:

```
Example: status (2 bits) + paid (1 bit) + count (4 bits)

uint64: ...0000 0011 1101
              ││││ ││└─ count (13)
              ││└┴─────── paid (1)
              └┴────────── status (0)
```

**Benefits:**
- States are contiguous integers: 0, 1, 2, ..., N-1
- Direct use as array indices for O(1) table lookup
- Compact representation (up to 20 bits = ~1M states)

### Variable Layout

Variables are assigned offsets and bit widths during declaration:

<!-- gocheck: check registry -->
```go
status := b.Enum("status", "pending", "paid", "shipped")
// domain=3, bits=2, offset=0

paid := b.Bool("paid")
// domain=2, bits=1, offset=2

count := b.Int("count", 0, 15)
// domain=16, bits=4, offset=3
```

Total state space: 3 × 2 × 16 = 96 states
Total bits: 2 + 1 + 4 = 7 bits

## Build-Time Verification

### Phase 1: Normal Form Computation (WFC Check)

For each state `s`:

1. Start with `s`
2. While any invariant is violated:
   - Apply the first violated invariant's repair
   - Track visited states to detect cycles
   - If cycle detected or depth exceeds state count: **WFC fails**
3. Result is `NF[s]` - the normal form of `s`

**WFC (Well-Founded Compensation)** passes if:
- All states reach a valid fixpoint
- No infinite compensation loops exist
- Maximum repair depth is bounded

Example cycle (WFC failure):

```
s1 --repair1--> s2 --repair2--> s1
```

### Phase 2: Step Table Computation

For each event `e` and state `s`:

```
Step[e][s] = NF(apply(e, s))
```

This precomputes the result of:
1. Applying event `e` to state `s` (may violate invariants)
2. Running compensation to reach the normal form

**Result:** `Step[e][s]` is the final converged state after event `e` from state `s`.

### Phase 3: Compensation Commutativity Check (CC)

For each independent event pair `(e1, e2)`:

#### CC1: Event Commutativity

For all valid states `s` (and the zero state `NewState` returns, when it is not valid):

```
Step[e1][Step[e2][s]] == Step[e2][Step[e1][s]]
```

Check if different orderings converge to the same normal form. `Build` runs this check for every
pair, with no shortcut: the step tables are already built, so a pair costs two lookups per state.
(Before the fix recorded in the CHANGELOG, `Build` skipped pairs whose triggered invariant
footprints were disjoint. That ignored what an event's guard or effect reads, and ignored write
sets no invariant watches, so it certified machines like the guarded pay/ship pair that diverge.)

#### CC2: Compensation Stability (Implicit)

CC2 (`NF(apply(e, NF(s))) == NF(apply(e, s))`) is not checked, and it does not hold in general:
an event whose guard reads a variable that repair changes behaves differently before and after
normalization. It is not needed for the runtime guarantee, because the runtime only ever applies
events to normal forms: every `Step` entry is a normal form, so a run that starts from a valid state
(or from `NewState`, which the CC1 domain includes) stays on valid states, where CC1 is checked.
A state the caller builds by hand (or restores from storage) that violates an invariant is
normalized by `Apply` before the event is applied (`Apply(s, e) == Apply(Normalize(s), e)`), so a
run from it also stays in the checked domain.

**CC passes if:** All independent event pairs commute on every valid state (and on `NewState`).

### Phase 4: Reported Obligations

Some conditions the convergence guarantee depends on are about the deployment, not the rules, so
`Build` does not fail on them; it measures them while building and from the step tables, and names
them in the `Report`:

- **Undeclared pairs** (`Report.CausalOrderRequired`, `Report.PairsUndeclared`). In declared-only
  mode (`Independent`), every undeclared pair is still checked; each one that does not commute is
  listed with a witness, and must be delivered in causal order.
- **Duplicates** (`Report.NotIdempotent`). Events whose second application changes the state; their
  redeliveries must be suppressed before `Apply`.
- **Saturation** (`Report.Saturations`). Rules whose write was clamped into a variable's range.

## Runtime Execution

### Machine Structure

<!-- gocheck: excerpt simplified view of the unexported Machine fields -->
```go
type Machine struct {
    name   string
    vars   []Var
    events map[string]int      // event name → index
    step   [][]uint64          // step[eventIdx][stateID] → normal form
    nf     []uint64            // nf[stateID] → normal form
}
```

### Event Application (O(1))

<!-- gocheck: excerpt simplified copy of the internal Apply -->
```go
func (m *Machine) Apply(s State, event string) State {
    ei := m.events[event]           // O(1) map lookup
    p := s.packed
    if p != 0 {
        p = m.nf[p]                 // O(1): normalize a hand-built invalid input first
    }
    return State{packed: m.step[ei][p], vars: m.vars}
}
```

**Why O(1):**
- No invariant checking
- No compensation logic
- Two array reads: `nf[stateID]` (the identity on valid states) and `step[eventIdx][stateID]`

### Example: Order System

State space:

```
status: pending(0), paid(1), shipped(2)
paid: false(0), true(1)

States:
0: {status=pending, paid=false}
1: {status=pending, paid=true}
2: {status=paid, paid=false}
3: {status=paid, paid=true}
4: {status=shipped, paid=false}
5: {status=shipped, paid=true}
```

Normal form table (after compensation):

```
NF[0] = 0  // valid
NF[1] = 1  // valid
NF[2] = 2  // valid
NF[3] = 3  // valid
NF[4] = 0  // invalid: shipped but unpaid → repair to pending
NF[5] = 5  // valid
```

Step tables:

```
Step[pay][0] = NF(apply(pay, 0)) = NF(3) = 3  // pending→paid, paid=true
Step[ship][0] = NF(apply(ship, 0)) = NF(2) = 0  // pending→shipped, unpaid → repair to pending
```

Runtime execution:

<!-- gocheck: check machine -->
```go
s := machine.NewState()          // s.packed = 0
s = machine.Apply(s, "ship")     // s.packed = step[ship][0] = 0
s = machine.Apply(s, "pay")      // s.packed = step[pay][0] = 3
```

Final state: `{status=paid, paid=true}` - converged despite invalid intermediate state.

## Compensation System

### Priority Ordering

Invariants are checked in declaration order. When multiple invariants are violated, the first one fires:

<!-- gocheck: check registry -->
```go
b.Invariant("inv1") // Higher priority
b.Invariant("inv2") // Lower priority
```

If both are violated, `inv1`'s repair fires first. After repair, if `inv2` is still violated, it fires next.

### Footprint Isolation

Each invariant declares its footprint - which variables it reads/writes:

<!-- gocheck: check registry -->
```go
b.Invariant("cap").
    Watches(count). // Footprint: {count}
    Holds(func(s State) bool { return s.GetInt(count) <= 10 }).
    Repair(func(s State) State { return s.SetInt(count, 10) }).
    Add()
```

Repairs must only modify variables in the footprint, and checks and repairs must only read it.
`Build` does not rely on this: it checks every event pair exhaustively from the step tables.
`BuildCompositional` does rely on it, so it checks it first (`verifyFootprints`): exactly from the
tree for combinator rules, and by perturbation for closures (every value of each outside variable
and each pair of outside variables). Events must also read only their write set there. Only then
does it skip event pairs that lie in different footprint components.

### Idempotence on Valid States

A critical property: **compensation must be identity on valid states**.

If `s` is valid (all invariants hold):

```
NF(s) == s
```

This holds by construction: compensation fires a repair only while some invariant is violated,
so it never runs on a valid state, and the normal form of a valid state is the state itself.

## Export Format

The `Export()` method serializes verification tables to JSON for multi-language runtimes:

```json
{
  "name": "order_system",
  "version": 2,
  "vars": [
    {"name": "status", "kind": "enum", "domain": 3, "labels": ["pending", "paid", "shipped"]},
    {"name": "paid", "kind": "bool", "domain": 2}
  ],
  "events": ["pay", "ship"],
  "nf": [0, 1, 2, 3, 0, 5],
  "step": [
    [3, 3, 3, 3, 3, 5],  // pay
    [0, 1, 5, 5, 0, 5]   // ship
  ],
  "verification": {
    "wfc": true, "cc": true, "max_repair_depth": 0, "state_count": 6, "event_count": 2,
    "all_pairs": true,
    "pairs": [["pay", "ship"]]
  },
  "exported_at": "2026-02-18T07:00:00Z"
}
```

`verification.pairs` (added in format version 2) lists the event pairs CC was checked for, by event
name, and `verification.all_pairs` says whether that is every pair. A pair left out (declared away
with `Independent`) is not guaranteed to commute.

Runtimes in Python, JavaScript, Rust, etc. can load this JSON and implement O(1) event application with the same
convergence guarantees.

## Scalability

### State Space Limits

Maximum: 1,048,576 states (2^20)

This is checked during `Build()`:

<!-- gocheck: excerpt simplified copy of the internal size check -->
```go
if b.totalBits > 20 {
    return nil, nil, fmt.Errorf("state space too large")
}
```

Why 20 bits:
- Reasonable for exhaustive verification (< 1 second on modern CPUs)
- Keeps table sizes manageable (~8MB for Step tables with 10 events)
- Covers most business logic state machines

### Memory Usage

For a machine with `N` states and `E` events:

- `NF` table: `N × 8 bytes` (uint64)
- `Step` tables: `E × N × 8 bytes`
- Total: `(E + 1) × N × 8 bytes`

Example:
- 10,000 states, 5 events
- Memory: 6 × 10,000 × 8 = 480 KB

### CC Checking Complexity

Worst case: O(E² × N) where E = number of events, N = state count

**Optimizations:**
1. **Declared independence** - Only check explicitly declared pairs
2. **Early termination** - Stop on first CC violation

Each pair costs two table lookups per state, far less than building the step tables (one event
application and normalization per event per state), so CC is rarely the dominant cost.

## Compositional Verification (BuildCompositional)

The Phase 1 and Phase 3 algorithms above enumerate the **global** state space, which caps
`Build` at the 20-bit ceiling. But WFC and CC are local properties when every rule reads and writes
only its declared footprint: events that read and write disjoint variables commute by structure
(the mechanized `disjoint_events_commute` result, whose precondition is exactly that an event reads
only its own footprint), and with normalization acting per component the pair satisfies CC1 at
every valid state (`calc_components_cc1_valid`). At an invalid state it need not
(`calc_components_cc1_iff`), which is why `Machine.Apply` normalizes an invalid input before the
event. So a registry partitions into footprint-connected **components** (of state variables) that
never interact, and it suffices to verify each component over the subspace of its own variables.

`Registry.BuildCompositional` does exactly this:

1. **Partition** variables into footprint-connected components using union-find: two variables
   join the same component when some invariant footprint or event write set mentions both.
2. **Check footprint conformance** (`verifyFootprints`). Every event's guard and effect must read
   and write only its write set; every invariant's check and repair only its footprint. Combinator
   rules are checked syntactically (exact). Closures are checked by perturbation: from every state
   of the component, every value of each outside variable and of each pair of outside variables.
   That catches dependence on one or two outside variables but not a joint dependence on three or
   more, so for closures the check is a test, not a proof, and `BuildCompositional` accepts closure
   rules only with `TrustClosureFootprints()` (the report's assurance is then
   `AssuranceOracleComponentsTested`). A violation rejects the machine and the report names it (WFC
   and CC are shown as not evaluated).
3. **Verify each component locally.** Enumerate only that component's subspace and run the WFC
   and CC checks over its valid states. A component of `k` variables costs the product of *those*
   domains, not the whole machine's.
4. **Skip cross-component pairs.** Events in different components read and write disjoint
   variables, and so do the repairs they trigger (a component is closed under shared writes and
   chained invariant footprints), so they commute; only same-component pairs are brute-forced
   (within their small subspace).

**Complexity:** exponential in the *largest component*, not in the whole machine. A registry of
many independent small invariants certifies even when its global state space is astronomically
large.

**Trade-off:** `BuildCompositional` returns a **lazy** `Machine` that computes `Apply` and
`Normalize` at runtime from the rules (there are no global tables to precompute), so `Export` is
unavailable (it needs the flat step tables that only global `Build` materializes). Runtime is no
longer a single array lookup; it evaluates the rules for the touched component. The repair loop is
bounded by the sum of each component's deepest verified repair chain, and `Apply` panics past it
(only a rule that breaks its footprint or is nondeterministic can get there).

**Preconditions:** every invariant declares its footprint, every event declares its write set
(both automatic when rules are written with the combinator vocabulary, see "Rule layers" below),
every event reads only what it writes, the zero state is valid, every variable's range fits its
bit field, and the machine fits in 64 bits of state (`State` is one `uint64`). No single component
may exceed the enumeration budget, and a closure whose perturbation test would exceed 2^28 calls is
rejected before any check runs.

## Differential Testing via Extracted Oracles

gsm's own verification is written in Go. To guard against the class of bug where a hand-written
verifier wrongly accepts a non-convergent machine, a built machine can be re-certified by
**checkers extracted from an axiom-free Coq proof** (the sibling `normalization-confluence` repo,
under `coq/extraction`). Because the checking logic is extracted from a machine-checked proof, a
bug in gsm's Go cannot make a non-convergent machine pass. There are two, at different trust
boundaries:

1. **Table oracle** (`Machine.WriteConvergenceTables`, cross-checked in `oracle_test.go` when
   `GSM_CONVERGENCE_CHECKER` points at the built binary). gsm emits the machine's step tables;
   the checker re-verifies that the per-event step functions commute and stay in range. It trusts
   that gsm computed the tables, and independently confirms those tables converge.

2. **Rules oracle** (`Registry.WriteMachineAST`, cross-checked in `astoracle_test.go` when
   `GSM_AST_CHECKER` points at the built binary). gsm serializes the combinator **rules**
   themselves (as S-expressions); the checker recomputes each event's step function by evaluating
   the rule AST (apply the event, then normalize by iterated repair) and confirms convergence from
   scratch. It trusts neither gsm's enumeration nor its tables: it re-derives the verdict straight
   from the declarations.

Neither oracle uses a footprint shortcut: both check every ordered pair of events exhaustively, so
both reject the guarded pay/ship machine that `Build`'s former shortcut certified. They differ from
`Build` in scope: they check all pairs whether or not they were declared `Independent`, and the
table oracle checks every encodable state in the tables (including invalid states no run reaches),
so either can reject a machine `Build` correctly accepts. The OCaml binaries do not run as part of
`Build` or any other entry point: the guarantee they add holds for the machines they are actually
run on. gsm's CI builds both from a pinned proof commit, checks them against pinned hashes
(`.github/oracle/`), and runs the whole test suite with `GSM_REQUIRE_ORACLES=1`, so the oracle
tests cannot skip. The table oracle also runs in-process on every success path. `internal/oracle`
holds it, generated as Go from the proof's extraction (`PROVENANCE`; CI regenerates it and
requires the same bytes). `Build`, `SynthesizeWith` (and through them `BuildOrSynthesize` and
`Synthesis.Machine`) and `BuildCompositional` (per component) return no machine unless it certifies
the tables (`oracle_gate.go`, `Report.Assurance`). The rules oracle is generated as Go the same way
and runs in-process in `Build` when every rule is a combinator and its work is within
`RulesOracleMaxWork` (`Report.RulesOracleSkipped` says why it did not run).
Only `Build` machines have tables, and only combinator rules serialize.

**Differential cross-check** (`oracle_differential_test.go`). Every `Build` in the test run is
recorded through a test-only observer, and after all tests `TestMain` runs both oracles on every
distinct machine (the hand-written ones plus 600 random combinator machines from
`oracle_random_test.go`). Each disagreement is classified, with Go-side evidence, as one of the
known spec differences (declared pairs; the zero state; invariant-invalid encodings in the
tables; `Build` requiring repair to terminate on every encoding where the rules oracle requires it
only where an event reaches) or as unexplained, which fails the run.

The rules oracle evaluates with gsm's arithmetic: signed integers and comparisons, clamped
writes. It refuses to certify an expression that could exceed 2^31-1 in magnitude (Go's `int`
wraps there on 32-bit platforms) and a write that could store a negative value into a two-valued
variable with minimum 0 (a `Bool` stores `value != 0`). Before this, it subtracted over the
naturals, truncating at 0, and certified machines `Build` correctly rejects (the guard
`Lt(Sub(V a, V b), Lit 0)` was never true); the differential test found 36 such disagreements on
random combinator machines, all on the oracle's side.

If an oracle rejects a machine `Build` accepted, and the failing pair is one `Build` checks on a
state in `Build`'s domain, one of them has a bug. The proof covers the checker's logic, not
whether its model of the rules matches gsm's Go semantics, so the bug can be on either side: the
arithmetic mismatch above was the oracle's. (The oracles' "FAIL" output does not name the pair or state; with every pair checked
by default and a machine whose zero state is valid, a disagreement is always such a case for the
rules oracle.) See the extraction README in `normalization-confluence` for
the file formats and how to build the binaries.

### Rule layers

Combinator rules (see README and CONCEPTS) come in two spellings that both lower to the **same**
primitive AST: an ergonomic surface (`AtMost`, `Inc`, `SetTo`, and the `Rule`/`On` builders) for
humans, and the primitive combinators (`Le`, `Set`, `Add`, `DeclInvariant`, `DeclEvent`) that are
serialized, tested, and handed to the oracle. A test pins that the two spellings serialize to
byte-identical rules, so the friendly layer cannot carry any semantics the analyzable core (and
therefore the verifier) does not see. Only rules built from this vocabulary are serializable;
closure-based invariants and events cannot be exported, and `WriteMachineAST` returns an error
rather than emit something the oracle would misread.

`Registry.PolicyBytes` exposes those serialized bytes and `Registry.PolicyDigest` a stable,
domain-separated SHA-256 over them, so a policy is a portable, nameable artifact: the digested
bytes are exactly the oracle's input, so an anchored digest and the re-checked artifact cannot
diverge. This is what lets an external audit layer commit a policy's identity in a log and hand
the same bytes to the rules oracle. The format addresses variables and events by position, so
`PolicyDigest` does not cover names, enum labels, variable kinds or the declared pairs;
`Registry.PolicyIdentityDigest` (over `PolicyBytes` and `PolicyNames`) and certificate digests do. `State.Digest` is the companion primitive for the resulting
state (a stable, domain-separated hash over the packed state value, well defined because the policy
pins the layout), so the same audit layer can attest which state an action produced and reproduce
it by replaying the same events over a reference build.

## Federations

`Federation.Build` (`federation.go`, `federation_verify.go`, `federation_monotone.go`) builds every
component with `Registry.Build`, so each component's tables are oracle-gated, then runs the
federation-level checks in Go: certificate validation for `EmbedCertified` subs, names and shared
variables, M1 for each morphism (R1/R2 for resolvers), cross-registry CC (C1: a target event
commutes with every source-driven change of its shared component, `CrossOrderError`), repaired CC
(C2: two target events commute with the morphism repair between them, `SameTargetOrderError`), and
acyclicity, or under `AllowMonotoneCycles` monotonicity over every state the Kleene iteration can
visit. `FedReport.Checks` lists what ran and `FedReport.Assurance` says it is Go-checked.

The runtime `FedMachine` holds one component `State` per registry. `Apply` runs the component's
table step, then repairs the shared components in topological order (or by Kleene iteration on a
monotone cycle). A target inside an `EmbedCertified` sub is repaired by a lookup in the
certificate's verified table instead of its `Map` or `Resolver` closure (`certificate_exec.go`;
`FedReport.Runtime` says which targets do). See CERTIFICATE-DESIGN.md.

## Related Work

This design is based on the theory in:

[*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18677400) (Blackwell, 2026)

The paper proves:

1. **WFC** ensures compensation terminates (well-founded ordering)
2. **CC** ensures event orderings converge (commutativity modulo normalization)
3. Together, WFC + CC guarantee eventual consistency

This library implements the verification algorithm and provides a practical runtime based on those proofs.
