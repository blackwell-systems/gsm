# Roadmap

What gsm plans next, and why. Each item is backed by a theory item in normalization-confluence's
[`docs/ROADMAP.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/docs/ROADMAP.md).
gsm ships a feature only when its guarantee is mechanized there, so the theory item lands first.
Nothing here is implemented yet.

## 1. Verify realistic domains

**Why.** `Build` checks convergence by enumerating states, so a registry has to use small finite
domains. Real data does not look like that: money as 64-bit amounts, string identifiers, thousands
of independent items. Today you shrink the model by hand (counts from 0 to 5, say) and nothing
proves the check carries over to the real system. This is the first limit a team adopting gsm hits.

**What it would do.** Let you declare realistic types and still get a guarantee, by one of these
reductions, each a theorem about the conditions `Build` already checks:

- **Data independence and symmetry.** When items are independent and governed identically (one
  stock count per product, say), checking one item covers all of them.
- **Abstraction.** A rule that depends only on how values relate ("stock is at least the quantity
  ordered") is checked over one representative of each relation instead of every value. A symbolic
  solver can do this, justified by the abstraction theorem rather than trusted.
- **Compositional checking by default.** Each rule is checked against only the variables it
  touches, so the cost grows with each piece, not with the whole system. `BuildCompositional`
  already does this for footprints; it would become the main path, with its soundness stated for
  every condition.

**Done when.** gsm verifies a registry with realistic domains through one of these reductions, and
the report names the reduction used and what it assumed.

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
