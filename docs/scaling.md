# Scaling your model

`Build` proves convergence by checking every state of a registry, so its plain form stops at 2²⁰
(about a million) states. Real systems are bigger than that: thousands of products, amounts in the
millions, many groups of variables. gsm has three ways to check a large model through a small one,
and in each the result is proved to hold for the real model, not only the small one (the theorems
are in normalization-confluence and mapped one by one in [Theory §11.8 to §11.10](theory.md)).

This page says which one to reach for. Each has its own section with the details:
[Collections](getting-started.md#collections-one-template-every-key),
[Abstraction](verification.md#abstraction-check-relationships-not-values),
[Compositional verification](verification.md#compositional-verification).

## Which one fits

| Your model looks like | Use | What gsm checks | Report line |
|---|---|---|---|
| Many items governed by the same rules (per product, per customer, per account), where an event on one item never reads another | **A collection**: `NewCollection[K](name, template).Build()` | The rules of one item | `Verified by symmetry over ProductID (items independent; cutoff 1)` |
| Integer fields with wide ranges, where the rules only compare and copy values and a few constants (a cap, a threshold) | **Abstraction**: `r.Abstract(constants...).Build()` | A handful of representative values per field | `Verified by abstraction over stock (rules compare values only; constants {5}; 7 representatives)` |
| Separate groups of variables with their own rules (orders and inventory), too large together to enumerate | **Nothing to declare**: `Build` checks each footprint component automatically | Each group on its own | `Verified compositionally: 2 components (...); 6 cross-component pairs need no check ...` |

In every case a pass is a guarantee for the whole model, and a failure is a real failure, reported
with a counterexample from the model you declared.

## Questions to ask about your model

1. **Is the state a set of similar items?** If events on one item never read another, write the
   rules for one item and use a collection. One product's rules cover a catalog of any size.
2. **Do any rules relate items to each other?** A rule like "total reserved across all products is
   at most the warehouse capacity" cannot be written in a collection, on purpose: such a rule can
   converge on one item and diverge on two (`aggregate_diverges`), so checking one item would prove
   nothing. Put the items such a rule relates into one registry, and `Build` checks it directly.
3. **Are the fields wide numbers?** If every rule only compares values ("below", "at most"),
   copies one field into another, or uses a few fixed constants, declare those constants with
   `Abstract`. The range can be as wide as you like.
4. **Do rules add or subtract?** Then abstraction does not apply yet: an order-pattern check can be
   fooled by arithmetic (`triangle_diverges`), so gsm refuses it. Keep those fields' ranges small
   enough for `Build`, or move the arithmetic out of guards where you can. The arithmetic route
   (solver-checked formulas) is on the [roadmap](ROADMAP.md#1b-abstraction-check-relationships-not-values).
5. **Is the model wide but loosely coupled?** Do nothing: when the whole machine is too large to
   enumerate, every rule is a combinator and it splits into components, `Build` checks it per
   component. Write rules as combinators (`On`, `Rule`) so gsm can see what each one reads; a rule
   written as a Go closure keeps the model on the global path.

## Combining them

| Combination | Supported | Notes |
|---|---|---|
| Collection + abstraction | Yes | A template may declare `Abstract`. The report carries both lines, and the result holds for every value at every key |
| Collection + compositional | No | A template is always checked as one registry: no combined theorem is stated yet |
| Abstraction + compositional | No | `Abstract` takes precedence and checks the registry over its representatives as a whole |
| Any of these + federations | Partly | A collection cannot be a federation component, a registry inside a federation is checked whole (never per component), and federations refuse a component declared with `Abstract` |

The unsupported combinations are not refused silently: the report says which path ran
(`Report.GlobalReason` prints `Checked globally: ...` with the reason).

## What each one refuses, and why

Each reduction is sound only for models in its fragment, so gsm checks the fragment first and
refuses anything outside it, naming the rule. Every refusal is backed by a counterexample showing
what would go wrong.

| Reduction | Refused | Why |
|---|---|---|
| Collection | Rules across items | Not expressible by construction (`aggregate_diverges`) |
| Abstraction | Go closures | gsm cannot see that a closure only compares and copies |
| Abstraction | Literals you did not declare (`Is(amount, 13)` without `Abstract(13)`) | The check could pass and the model still diverge (`exact13_diverges`) |
| Abstraction | Arithmetic in guards, invariants or writes | Same reason (`triangle_diverges`) |
| Abstraction | `Bool` and `Enum` variables, writes that could saturate | Outside what the theorems model |
| Compositional | A read missing from a footprint | Reads are always included for combinators; with writes alone the check would pass and the machine diverge (`ws_diverges`) |
| Compositional | A repair that writes outside its component | Components are merged instead, or a closure repair is refused (`rc_diverges`) |

## Assurance

`Report.Assurance` says what certified the result:

| Path | Assurance |
|---|---|
| Plain `Build` (whole machine) | `AssuranceOracleTablesAndRules` (both extracted oracles) |
| Collection | The template's assurance |
| Abstraction | `AssuranceOracleRepresentatives`: the table oracle certifies the representative tables |
| Per component | `AssuranceOracleComponents`: the table oracle certifies every component's tables |

A model small enough for plain `Build` is checked whole even when it decomposes, because that gives
the strongest assurance and the step tables `Export`, federations and certificates need.

## When nothing fits

- **Arithmetic on wide ranges:** narrow the ranges until `Build` can enumerate them, or restructure
  so the arithmetic happens outside guards.
- **One large, tightly coupled model:** look for a split. Often a rule couples groups only through a
  flag that a separate registry could own, with the groups connected in a [federation](federation.md).
- **Closure rules:** rewrite the hot rules as combinators, or use `BuildCompositional` with
  `TrustClosureFootprints` (footprints tested by perturbation, not proved; see
  [Verification](verification.md#buildcompositional)).

The [roadmap](ROADMAP.md) lists what is coming next: the arithmetic route, partial-order reduction
(checking fewer event orders), and combining these reductions further.
