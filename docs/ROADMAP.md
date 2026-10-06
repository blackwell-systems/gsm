# Roadmap

What gsm plans next, and why. Each item is backed by a theory item in normalization-confluence's
[`docs/ROADMAP.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/docs/ROADMAP.md).
gsm ships a feature only when its guarantee is mechanized there, so the theory item lands first.
Nothing here is implemented yet.

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

- **When it applies.** State is a collection of items (per product, per customer, per account)
  governed by the same rules, where an event on one item reads and writes only that item.
- **The theorem.** For independent, identically governed items, each convergence condition holds
  for any number of items if and only if it holds at a small cutoff (one item, or two for
  conditions that relate two events on different items).
- **Example.** Per-product inventory with reserve, release and restock: verify one product, and
  the result covers a catalog of any size.
- **Not covered.** Aggregates that make items interact ("total reserved across all products is at
  most warehouse capacity"). The check detects them and refuses this reduction.

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
3. **Compositional by default** last, since most of the machinery exists.

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
