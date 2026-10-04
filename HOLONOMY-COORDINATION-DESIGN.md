# Holonomy-minimal coordination (design note)

Status: **proposed, not implemented in gsm.** This note specifies a sharper replacement for the plan
that `Federation.CoordinationPlan` returns today. The theory behind it is machine-checked axiom-free
in the papers repo, [normalization-confluence](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq)
(CI-verified on Coq 8.18, 8.20, and Rocq 9.3): the cycle-basis criterion in `coq/CohomologyGraph.v`,
and the soundness of the plan itself in `coq/CoordinatedCycles.v` (see
[Mechanized soundness](#mechanized-soundness-coordinatedcyclesv)). It is additive: `Build`,
`DiagnoseCycle`, `CoordinationPlan`, and `BuildCoordinated` keep their current behavior.

## Problem

When a federation has a non-monotone cycle, `Build` rejects it, and `CoordinationPlan` names
morphism edges to place under external coordination: the back edges of a depth-first search, which
make the morphism graph acyclic. That plan is always correct, but it is blind to *what the cycle
does*. It cuts every cycle, including cycles that are already consistent.

The clearest case is a **lossless round-trip**: A copies a value to B and B copies it back, or a
unit conversion that maps there and back exactly, or a bijective schema mapping in both directions.
Going around such a cycle returns every value to itself (trivial holonomy), so the constraints never
conflict. Today gsm still rejects the cycle and asks for coordination on it. The theory says no
coordination is needed there, and that coordination is needed only on cycles whose holonomy is
non-trivial.

## What the theorems give

Model: in the **invertible fragment**, each morphism transports a shared value by a bijection of the
shared fiber (a copy, a permutation, a bijective unit or schema map). Pick a spanning tree of a
connected network. Every non-tree edge closes one **fundamental cycle**, and its holonomy is the
composite of the transports around that cycle. The machine-checked results (any group, abelian or
not):

| Lemma (`CohomologyGraph.v`) | What it licenses in gsm |
|---|---|
| `cycle_basis_criterion` | A consistent global state exists **iff every fundamental cycle has trivial holonomy**. Checking one holonomy per non-tree edge is complete; no other cycles need checking. |
| `keep_balanced_suffices` | Keeping any set of balanced non-tree edges leaves a consistent state. Balanced edges need **no coordination**. |
| `unbalanced_blocks` | Keeping any unbalanced non-tree edge destroys every consistent state. Each one **must** be coordinated. |
| `tree_has_section`, `tree_unique` | The tree alone is always consistent, and its consistent states are unique up to one constant. |
| `sat_iff_trivial_holonomy` | "Balanced" is exactly "the holonomy `s(v)^-1 g s(u)` is the identity", a finite, total check. |
| `H1_classification`, `betti_number` | The obstruction lives in at most `|E| - |V| + 1` generators, one per non-tree edge. |

So, relative to a chosen spanning tree, the edges that must be coordinated are **exactly** the
unbalanced fundamental edges: no fewer (necessity) and no more (sufficiency). The `S_3` result
(`coq/CohomologyMin.v`, `theta_separation`) adds a correctness warning: sizing this by an abelian
invariant (a parity or sign check) under-counts, so the check must compose the actual permutations.

## The authority subtlety (required reading)

A trivial holonomy proves a consistent state **exists**. It does not by itself make the normal form
**unique**. In the copy-back loop, both "A and B hold 0" and "A and B hold 1" are consistent, and
which one a run reaches depends on which morphism fires last. `tree_unique` makes this precise:
consistent states differ by a constant. Normalization confluence requires one normal form regardless
of order, so a balanced cycle still needs a tie-break.

The theorems supply it: **drive values along the spanning tree from a designated root, and demote
each balanced non-tree edge from a writer to a checked constraint.** The tree is acyclic, so the
existing authority argument makes its normal form unique; `keep_balanced_suffices` guarantees the
demoted edges hold at that normal form. No consensus is involved; the root is an authority, exactly
as a source is in today's acyclic federations.

This is a semantic choice, so it must be explicit: the plan reports **which registry is the
authority root** for each accepted cycle, and the user can override it. The proof shows the report
is required, not cosmetic: the normal form is unique *given the root*, and a different root can
reach a different normal form from the same state (`root_choice_matters`: the copy-back loop rooted
at A reaches A = B = 0, rooted at B reaches A = B = 1, and both are consistent with the whole loop). Demoting an edge also means
a local write to the demoted target's shared variables no longer propagates backward along that
edge; the report must say so. "Coordination-free" in this note always means *coordination-free given
the reported authority root*.

## Scope by phase

gsm's federation model is directed, and a target with several incoming morphisms already requires a
resolver. That shapes the rollout.

**v1: simple invertible cycles.** Within a strongly connected component where every registry has one
incoming morphism, the component is a single directed cycle (`|E| = |V|`, one fundamental cycle). For
each such cycle whose morphisms are all invertible on a common shared carrier:
- holonomy is the identity permutation: **accept coordination-free**, rooted at the reported authority;
- holonomy is non-trivial: **coordinate one edge** (minimal: a cycle needs exactly one cut);
- any morphism non-invertible: **fall back** to today's behavior (`DiagnoseCycle` plus
  `CoordinationPlan`).

v1 never coordinates more than `CoordinationPlan` and coordinates strictly fewer exactly when a
balanced cycle exists. It needs no new resolver semantics.

**v2: general invertible components (an agreement resolver).** To use the full theorem, a target with
several invertible sources needs a resolver mode meaning *all sources must agree*: one source (the
tree edge) drives the value, and the others are checked constraints. With that mode, compute a
spanning tree of the component, classify every non-tree edge by its fundamental holonomy, and
coordinate exactly the unbalanced ones. This is where `cycle_basis_criterion` pays off in full: one
holonomy check per non-tree edge decides the whole component.

**v3: choosing the tree.** The minimal set is minimal *relative to the tree*. The global minimum
over all trees is the group feedback edge set problem: NP-hard in general, fixed-parameter
tractable in the size of the coordinated core. Ship a deterministic default (breadth-first from the
root), then optionally a best-of-several-trees search and an exact search for small cores, reporting
which was used.

## Certification

Like the existing table and rules oracles (`checker`, `astchecker`), the plan should be re-checkable
without trusting gsm's Go:

- gsm emits, per morphism in the component, its transport as a permutation table on the shared
  fiber, plus the chosen tree, the root, and the classification (balanced or unbalanced per
  non-tree edge).
- A Coq-extracted checker re-verifies (a) each table is a bijection, (b) the tree is a spanning tree
  rooted at the reported root, (c) each reported holonomy, recomputed by composing tables, is the
  identity exactly when the edge is reported balanced, and (d) the tree-driven state satisfies every
  kept edge.
- Soundness: (d) is a direct section check. Necessity of each coordinated edge follows from
  `unbalanced_blocks` once (a) to (c) hold.

Design risk: the abstract theorems quantify over a group given by section hypotheses. Instantiating
them at "permutations of a finite set" requires either a subset type of valid permutations (and care
with proof-relevant equality) or a checker whose soundness is proved directly over concrete tables.
The direct route is likely simpler and matches how `Checker.v` was done. Decide before building.

## Proposed API shape (sketch)

```go
// HolonomyPlan classifies each cyclic component and returns the minimal coordination relative to
// a spanning tree, with the authority root for every component accepted without coordination.
func (f *Federation) HolonomyPlan(opts ...HolonomyOption) (*HolonomyReport, error)

type HolonomyReport struct {
	Components []ComponentPlan
}

type ComponentPlan struct {
	Members    []string            // registries in the strongly connected component
	Root       string              // authority root (overridable via an option)
	Tree       []CoordinationPoint // morphisms that drive values
	Balanced   []CoordinationPoint // demoted to checked constraints, no coordination
	Coordinate []CoordinationPoint // unbalanced: must be externally coordinated
	Fallback   bool                // some morphism is non-invertible; uses CoordinationPlan
	Tables     []PermutationTable  // for the extracted oracle
}

// BuildHolonomy builds the federation under a HolonomyReport: tree morphisms drive, balanced
// morphisms become verified constraints, coordinated morphisms become external inputs.
func (f *Federation) BuildHolonomy(r *HolonomyReport) (*FedMachine, *FedReport, error)
```

`HolonomyReport.Coordinate` uses the existing `CoordinationPoint` type, so `BuildCoordinated`-style
consumers keep working. `WithRoot(registry)` overrides the authority root.

## Soundness condition

A plan is sound when every morphism in a non-fallback component is a bijection on one shared carrier,
the tree spans the component, every edge in `Balanced` has identity holonomy against the tree, and
every edge in `Coordinate` is removed from the driving network. Under those conditions the
tree-driven normal form is unique given the root (acyclic authority) and satisfies every balanced
edge (`keep_balanced_suffices`), and no plan relative to the same tree can keep any `Coordinate` edge
(`unbalanced_blocks`). This is now a mechanized theorem, below.

## Mechanized soundness (`CoordinatedCycles.v`)

`coq/CoordinatedCycles.v` in normalization-confluence proves the plan sound, axiom-free. Its model:
registries are the vertices of a group-labeled graph, the shared fiber of every registry is the
group itself acting on itself (the **regular action**; `Z/2` with xor covers gsm's copy and `1 - x`
loops), and an edge `(u, v, g)` is a morphism transporting the value at `u` to `g * s(u)` at `v`, so
every transport is **invertible**. A plan, relative to a designated authority root `r`, splits the
edges of a component into a spanning tree `T` grown from `r` (its edges drive values, against a
morphism's direction where needed, which is where invertibility is used), balanced non-tree edges
`B` (demoted to checked constraints), and coordinated non-tree edges `C` (removed from the
federation). The driving network is acyclic, so the acyclic federation results apply to it
unchanged.

| Theorem | What it says |
|---|---|
| `coordinated_sound` | With `T` a spanning tree and every edge of `B` balanced: for every initial state and every topological order of the driving network, the normal form satisfies every tree edge and every balanced constraint, keeps the root's value, is the **unique** state satisfying `T ++ B` with that root value, and every other order reaches the same state. |
| `coordinated_unique_nf` | The same, as existence and uniqueness of the normal form for every authority value. |
| `coordination_needed` | Keeping any unbalanced edge of `C`, as a writer or as a constraint, leaves no consistent state at all. |
| `plan_exact` | A set of non-tree edges can be kept with a consistent state **iff** it avoids `C`: the coordinated set is exactly the unbalanced edges, no fewer and no more. |
| `coordinated_events_converge` | Any two permutations of local events converge under the plan, provided the root's own events commute; events on non-root tree registries are overwritten by the drive (the demotion warning above). |

The same file records where the naive statement is false, and each one is a requirement on the
implementation:

- **The root must be reported** (`root_choice_matters`, above). Uniqueness holds only given the
  root, so `ComponentPlan.Root` is part of the result, not a detail.
- **An authority is needed at all** (`copyback_without_authority`): with both copy-back edges kept as
  writers there are two consistent states and two propagation orders reach different ones. Trivial
  holonomy gives existence, not uniqueness.
- **Transports must be invertible** (`noninvertible_balance_not_static`): with a constant back edge,
  whether the edge holds at the normal form depends on the authority value, so "balanced" is not a
  property of the cycle. This is why a non-invertible morphism forces `Fallback`.
- **"No consistent state" needs the regular action** (`nonfree_holonomy_counterexample`): a
  bijection acting on a fiber that is not the group itself (a swap of `{0, 1}` acting on
  `{0, 1, 2}`) has non-identity holonomy yet admits a consistent state (value 2). The edge is still
  violated for another authority value, so a plan sound for every root value must still coordinate
  it; only the "no consistent state at all" reading of `unbalanced_blocks` is lost.

**Follow-up (gsm, not implemented).** Today's `Federation.CoordinationPlan` returns a list of
`CoordinationPoint`s (`Src`, `Dst`, `Shared`) and reports no authority root. That is consistent
with what it does (it removes a feedback edge set and every remaining source is an authority, as in
any acyclic federation), but the edges it cuts come from a depth-first search in component order,
so which registries end up authoritative is implicit. Any implementation of this design
(`HolonomyPlan` / `BuildHolonomy`) must report the root per component, and the report and
`FedReport` should name it, as `root_choice_matters` requires. Reporting the implied authorities of
today's plan is a smaller follow-up worth considering in the same change.

## Tests and examples to ship with v1

- Copy-back loop (`TestDiagnoseCycle_IdentitySettles`'s network): accepted, zero coordination,
  root reported; today `CoordinationPlan` cuts one edge.
- Negation loop (`TestDiagnoseCycle_NegationOrbit`): one edge coordinated, matching today.
- A round-trip unit conversion (bijective on a finite domain): accepted coordination-free.
- A 3-cycle with a 3-cycle permutation as holonomy: coordinated, though a parity check would call it
  balanced (the `S_3` warning in miniature).
- Mixed: one non-invertible morphism on the cycle forces `Fallback`.
- Differential test against the extracted oracle on every accepted plan.

## Non-goals

- Not changing `Build`'s default: cyclic non-monotone federations stay rejected unless the caller
  opts into `HolonomyPlan` / `BuildHolonomy`.
- Not the global minimum in v1 or v2 (that is v3, and NP-hard in general).
- Not the non-invertible case. There the obstruction is the dynamical fixed-point condition that
  `DiagnoseCycle` already explores, not group holonomy.
- Not monotone cycles, which `AllowMonotoneCycles` already accepts by Kleene iteration.
