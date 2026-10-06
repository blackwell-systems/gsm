# Roadmap

What gsm plans next, and why. Each item is backed by a theory item in normalization-confluence's
[`docs/ROADMAP.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/docs/ROADMAP.md).
gsm ships a feature only when its guarantee is mechanized there, so the theory item lands first.
Item 1a (symmetry) is implemented; nothing else here is implemented yet.

## 1. Verify realistic domains

**Why.** `Build` proves convergence by checking every combination of state values, and that count
explodes: 3 variables of 10 values is 1,000 states and instant; 10 variables of 1,000 values is
10^30 and impossible. Real data is worse: money as 64-bit amounts, string identifiers, thousands
of products. Today you shrink the model by hand (stock counts 0 to 5, two products) and nothing
proves the check carries over. That is the difference between a verified model of your system and
verification of your system, and it is the first limit a team adopting gsm hits.

**What it would do.** Let you declare realistic types and still get a guarantee, through
reductions that each come with a theorem saying "checking this small thing proves the property for
the real one". The theorems are about the conditions `Build` already checks (WFC, CC, C1, C2, XU,
the at-least-once conditions), so the guarantee means exactly what it means today.

### 1a. Symmetry: check one item, conclude for all

- **Implemented.** `NewCollection[K](over, template).Build()` verifies the template (one item)
  with `Build` and returns a machine that runs it at every key; the report reads "Verified by
  symmetry over ProductID (items independent; cutoff 1)" (`Report.Symmetry`). Theory:
  normalization-confluence `coq/SymmetryCutoff.v`. See
  [Getting started](getting-started.md#collections-one-template-every-key) and
  [Theory §11.8](theory.md#118-keyed-collections-symmetry). 1b, 1c, 1d remain.
- **When it applies.** State is a collection of items (per product, per customer, per account)
  governed by the same rules, where an event on one item reads and writes only that item.
- **The theorem.** For independent, identically governed items, each convergence condition holds
  for any number of items if and only if it holds at a small cutoff (one item, or two for
  conditions that relate two events on different items). gsm repairs each key's item on its own,
  so every condition its report states has cutoff 1 ([Theory §11.8](theory.md#118-keyed-collections-symmetry)).
- **Example.** Per-product inventory with reserve, release and restock: verify one product, and
  the result covers a catalog of any size.
- **Not covered.** Aggregates that make items interact ("total reserved across all products is at
  most warehouse capacity"). As implemented they cannot be written through a collection at all:
  every template rule sees one item's state and no key, so the hypotheses hold by construction
  (`lift_idgov`) and there is nothing to detect. An aggregate goes in one registry that holds the
  items it relates, which `Build` checks directly.

### 1b. Abstraction: check relationships, not values

- **When it applies.** Rules that only compare or add values ("stock is at least the quantity
  ordered", "balance minus amount stays non-negative"), not rules that test exact constants.
- **The theorem.** Checking over one representative of each relationship between the values a
  rule compares (or symbolically, with an SMT solver over all integers) implies the condition for
  every value. The solver's answer counts because the theorem says it answered the right question.
- **Example.** A wallet with 64-bit balances: verified without enumerating balances.
- **Not covered.** Rules that inspect exact values ("if amount = 13"), detected and refused.

### 1c. Compositional checking by default

- **When it applies.** Always, when rules declare or reveal what they read and write.
- **The theorem.** Each condition decomposes over footprints: checking every pair of rules over the
  variables they touch implies the condition for the whole registry. `BuildCompositional` already
  enumerates per footprint component; this makes it the default and states its soundness for every
  condition, not only CC.
- **Effect.** Cost grows with the largest piece, not with the product of everything.

### 1d. Partial-order reduction: check fewer event orders, not just fewer states

- **When it applies.** The checks that explore reachable states by running event sequences: the
  exact conditions on cycles (GC), the reachable-state forms of the federation and projection
  conditions, and the planned migration check. Single-registry CC already checks pairs and needs
  no reduction.
- **The idea.** When two events commute at a state, the two orders reach the same state, so the
  explorer needs to follow only one of them. Standard partial-order reduction (persistent or ample
  sets, sleep sets) prunes the orders a commutation proof makes redundant.
- **The theorem.** Exploring only the reduced set of orders reaches every state the exact
  condition quantifies over (or a representative of it under the equivalence the condition
  respects), so the condition decided on the reduced exploration is the condition on the full one.
  The commutation results gsm already relies on (trace equivalence, `run_tequiv`) are the main
  ingredient; what is new is the soundness of the pruning for each condition.
- **Example.** A cyclic federation whose events are mostly independent: the reachable states are
  explored along one order per independent group instead of every interleaving.
- **Not covered.** Events whose independence holds only at some states: the reduction uses only
  commutation that the check has itself established, never a declared one it has not verified.

- **Independence is checked, not declared.** The reduction only uses pairs the check has itself
  shown independent at the states concerned: no overlapping reads and writes (or proved
  commutation), neither event disabling the other, no causal dependency between them, and, under
  at-least-once delivery, duplicates that land compatibly. Each of those restrictions is backed by a
  mechanized counterexample in normalization-confluence.
- **The payoff.** For n events that are pairwise independent, checking every order means n!
  schedules; the reduction checks one. With partial independence, it checks one order per
  dependency-respecting trace.
- **Combined with symmetry.** Symmetry removes the need to check every item; this removes the need
  to check every order of events. Together, a catalog of thousands of products under thousands of
  events is checked through one representative product and one trace per class.
- **How it would read in the report.**

  ```
    Verified by symmetry over ProductID (items independent; cutoff 1)
    Verified by history reduction (independent event orders collapsed: 31 independent pairs,
      7 dependency classes)
  ```

This is the history-side counterpart of 1a to 1c: symmetry and abstraction collapse states the
rules cannot tell apart, and partial-order reduction collapses event orders the rules cannot tell
apart. In normalization-confluence's terms the first is state descent and the second history
descent, used to make checking cheap.

### How it shows up in gsm

- Declarations with realistic types: `Int64` amounts, string identifiers, keyed collections
  (`Map[ProductID]Item`).
- `Build` picks a reduction that applies, runs the small check, and the report names it and its
  assumptions, for example: "Verified by symmetry over ProductID (items independent); by
  abstraction over Stock (rules compare values only)."
- If no reduction applies, the report says so and names the rule that blocks each one. It never
  shrinks a domain silently.

### Order of work

1. **Symmetry first**: the most common shape in real systems, the cleanest theorem, and the
   largest immediate gain.
2. **Abstraction second**, starting with comparison-only rules.
3. **Compositional by default**, since most of the machinery exists.
4. **Partial-order reduction** for the checks that explore reachable states, once the checks that
   need it (cyclic GC, the migration check) are in use.

Each step ships on its own.

**Precedents.** Data independence (Wolper, 1986), cutoff results in parameterized verification,
predicate abstraction, and solver-backed verifiers such as Ivy and Apalache. The new part is
applying them to gsm's convergence conditions with mechanized soundness.

**Done when.** gsm verifies a registry with realistic domains through at least the symmetry
reduction, the report names the reduction and what it assumed, and a test checks that no reduction
is applied where its hypotheses fail.

**Theory item.** normalization-confluence roadmap item 8.

## 2. Check a change before you deploy it

**Why.** Systems change while running. You add a service between two others, migrate a schema, or
change a rule (a limit from 5 items to 10) while events and projections are still in flight. gsm
checks a fixed configuration; nothing says whether a live change keeps convergence. Deploys and
migrations are where real systems break.

**What it would do.** A migration check that takes the old and the new configuration and classifies
the change:

- **Safe online:** events and projections from both configurations can mix, and every run still
  converges.
- **Safe behind a barrier:** drain or start a reset epoch, then switch. A change at a barrier is
  already covered by the existing guarantees, applied to each configuration.
- **Unsafe:** a concrete sequence of events that diverges across the change.

**Done when.** The exact conditions for a live change are mechanized, and the migration check
reports one of the three outcomes with its witness.

**Theory item.** normalization-confluence roadmap item 9 (`REGIME-AUDIT.md` gap 20).

## 3. A regime report: one paragraph that says what you have

**Why.** The guarantees gsm gives depend on what you built and how you deploy it: one registry or a
federation, declared independent pairs or not, cycles, projection deployments, at-least-once or
causal delivery. Today the pieces are spread across `Report`, `FedReport` and the docs pages, and a
user has to know which [Deployment](deployment.md) rules apply to their setup. Much of the newer
theory (duplicate delivery on streams, buffered guards, projection channels) reaches users only as
documentation.

**What it would do.** A single summary, printed with the report and available as a field, that
names the regime and states its contract in three parts:

```
  Regime: acyclic federation, projection deployment, at-least-once delivery
  Guaranteed: every interleaving converges to the FedMachine result once channels drain
  You must provide: versions in send order on the snapshot sent; a send after every source
    change; deduplication for withdraw, refund
  Not covered: none
```

- **Regime** is derived from what was built and declared (topology, cycle opt-ins, declared pairs,
  projection mode, and the delivery model you state), not guessed.
- **Guaranteed** lists only what the checks that ran actually certify, each line traceable to a
  mechanized theorem in normalization-confluence.
- **You must provide** collects every obligation the report already computes
  (`NotIdempotent`, `CausalOrderRequired`, projection versioning) plus the deployment rules for
  that regime.
- **Not covered** names anything outside the mechanized model for that configuration (a cyclic
  projection deployment, a multi-source target, live reconfiguration), with a pointer to the open
  gap, so the report never implies more than is proved.

**Done when.** `Build` and `Federation.Build` produce the summary for every supported
configuration, each "Guaranteed" line cites its theorem, and a test checks that no configuration
reports a guarantee its checks did not establish.

**Theory item.** None new: it packages normalization-confluence's regime map
([`REGIME-AUDIT.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md),
[`docs/COVERAGE.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/docs/COVERAGE.md))
for one user's configuration.

## Smaller items

- **Oracle-certified federation checks.** Federation-level checks (M1, R1/R2, C1, C2, XU, cycles)
  run in Go; single-registry builds are already certified by an extracted oracle. normalization-confluence
  roadmap item 5.
- **Holonomy-minimal coordination.** `CoordinationPlan` cuts every cycle. The sharper plan,
  coordinating only cycles whose holonomy is non-trivial, has a mechanized soundness theorem and a
  design ([design note](design/HOLONOMY-COORDINATION-DESIGN.md)), but is not implemented.
- **Reset epochs for cyclic projection deployments.** Cyclic projection deployments are reported as
  not certified. Barrier reset epochs would certify them (`epoch_agree_iff`), but gsm does not
  implement epochs ([Deployment](deployment.md#cycles-ghosts-and-reset-epochs)).
