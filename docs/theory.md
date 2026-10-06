# Mathematical Foundations

This document provides the formal mathematical foundations of governed state machines. It is intended for readers with background in formal methods, rewriting theory, or distributed systems who want to understand the rigorous theoretical basis for convergence guarantees.

For other audiences, see:
- **Practical usage**: [README.md](../README.md) and [Getting started](getting-started.md)
- **Conceptual introduction**: [Concepts](concepts.md)
- **Implementation**: [ARCHITECTURE.md](design/ARCHITECTURE.md)
- **Research paper**: [Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)

This page covers the theory behind what gsm checks. Which regimes have an exact condition, a
hardness result, or an open gap, and where gsm sits in each, is the
[regime audit](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md)
in normalization-confluence; it is not restated here.

## Table of Contents

1. [Abstract Rewriting Systems](#1-abstract-rewriting-systems)
2. [Confluence and the Church-Rosser Property](#2-confluence-and-the-church-rosser-property)
3. [Newman's Lemma](#3-newmans-lemma)
4. [Governed State Machines as Rewriting Systems](#4-governed-state-machines-as-rewriting-systems)
5. [Well-Founded Compensation (WFC)](#5-well-founded-compensation-wfc)
6. [Compensation Commutativity (CC)](#6-compensation-commutativity-cc)
7. [The Convergence Theorem](#7-the-convergence-theorem)
8. [Footprint Calculus](#8-footprint-calculus)
9. [Verification Algorithm](#9-verification-algorithm)
10. [Comparison to Related Formalisms](#10-comparison-to-related-formalisms)
11. [Limitations and Extensions](#11-limitations-and-extensions)
12. [Relationship to the Paper](#12-relationship-to-the-paper)

---

## 1. Abstract Rewriting Systems

### 1.1 Definition

An **abstract rewriting system** (ARS) is a pair (A, →) where:
- **A** is a set (the objects)
- **→** ⊆ A × A is a binary relation (the rewrite relation)

We write a → b when (a, b) ∈ →, read as "a rewrites to b in one step."

### 1.2 Derived Relations

**Reflexive transitive closure** (→*):
- a →* a for all a ∈ A (reflexivity)
- If a → b and b →* c, then a →* c (transitivity)

**Equivalence closure** (↔*):
- Symmetric, reflexive, transitive closure of →
- a ↔* b iff a and b are joinable by some sequence of forward/backward rewrites

### 1.3 Normal Forms

An element a ∈ A is in **normal form** (irreducible) if there is no b such that a → b.

For any a ∈ A, an element b is a **normal form of a** if:
1. a →* b (b is reachable from a)
2. b is in normal form (no further rewrites possible)

### 1.4 Termination

An ARS is **terminating** (strongly normalizing) if there is no infinite sequence:

```
a₀ → a₁ → a₂ → a₃ → ...
```

**Equivalently**: Every reduction sequence starting from any element eventually reaches a normal form.

### 1.5 Confluence

An ARS is **confluent** if whenever a →* b and a →* c, there exists d such that b →* d and c →* d.

**Diamond property** (visual representation):
```
      a
     ↙ ↘
    b   c
     ↘ ↙
      d
```

**Key theorem**: If an ARS is confluent, then every element has at most one normal form.

**Proof sketch**: Suppose a has two normal forms n₁ and n₂. Then a →* n₁ and a →* n₂. By confluence, there exists d such that n₁ →* d and n₂ →* d. But n₁ and n₂ are normal forms, so n₁ = d and n₂ = d, thus n₁ = n₂.

---

## 2. Confluence and the Church-Rosser Property

### 2.1 Church-Rosser Property

An ARS has the **Church-Rosser property** if:

```
a ↔* b  ⟹  ∃c. a →* c ∧ b →* c
```

In words: If a and b are equivalent (connected by forwards/backwards rewrites), then they have a common reduct.

**Theorem**: An ARS is confluent if and only if it has the Church-Rosser property.

### 2.2 Local Confluence

An ARS is **locally confluent** if whenever a → b and a → c, there exists d such that b →* d and c →* d.

**Diamond property for one step**:
```
      a
     ↙ ↘  (single steps)
    b   c
     ↘ ↙  (multi-step)
      d
```

Local confluence is strictly weaker than confluence. A system can be locally confluent without being confluent.

**Counterexample** (non-terminating):
```
a → b
a → c
b → b  (loop)
c → d
```

This is locally confluent (after one step from a, we can join) but not confluent (b →* b but c →* d, and b ≠ d).

---

## 3. Newman's Lemma

### 3.1 Statement

**Newman's Lemma (1942)**: If an ARS is terminating and locally confluent, then it is confluent.

This is fundamental to gsm's convergence guarantee.

### 3.2 Proof Sketch

The proof uses **Noetherian induction**, a generalization of mathematical induction to well-founded relations. In a terminating ARS, define a ≻ b when a → b (one rewrite step). Since the system terminates, ≻ is well-founded: there are no infinite descending chains a₀ ≻ a₁ ≻ a₂ ≻ ... To prove a property P(a) for all elements, it suffices to show: if P(b) holds for all b with a ≻ b (all one-step successors of a), then P(a) holds. This is the inductive step - we assume the property for "smaller" elements (closer to normal form) and prove it for "larger" ones.

Assume the ARS is terminating and locally confluent. We prove confluence by Noetherian induction.

**Base case**: If a →⁰ b and a →⁰ c (zero steps), then b = a = c, so they trivially join.

**Inductive case**: Suppose a →* b and a →* c. We need to find d such that b →* d and c →* d.

Case 1: If b = a or c = a, trivial.

Case 2: Otherwise, a → b₁ →* b and a → c₁ →* c for some b₁, c₁.

By local confluence, there exists e such that b₁ →* e and c₁ →* e.

Since the system terminates, b₁ and c₁ are "smaller" than a (fewer steps to normal form).

By inductive hypothesis:
- b →* d₁ and e →* d₁ for some d₁ (because b₁ →* b and b₁ →* e)
- c →* d₂ and e →* d₂ for some d₂ (because c₁ →* c and c₁ →* e)

Since e →* d₁ and e →* d₂, and e is smaller than a, by inductive hypothesis there exists d such that d₁ →* d and d₂ →* d.

Therefore b →* d₁ →* d and c →* d₂ →* d, proving confluence. ∎

### 3.3 Significance

Newman's Lemma reduces the global property (confluence) to two local properties:
1. **Termination** - rewriting eventually stops
2. **Local confluence** - one-step divergences can be joined

Both properties are typically easier to verify than global confluence.

---

## 4. Governed State Machines as Rewriting Systems

### 4.1 Registry Definition

A **registry** R is a tuple (V, I, ρ) where:
- **V** is a finite set of variables, each with finite domain
- **I** = [inv₁, inv₂, ..., invₖ] is an ordered list of invariants
- **ρ** = [ρ₁, ρ₂, ..., ρₖ] are corresponding repair functions

Each invariant invᵢ is a predicate invᵢ : S → {true, false}.

Each repair ρᵢ : S → S modifies only variables in invᵢ's footprint.

### 4.2 State Space

The **state space** S is the Cartesian product of variable domains:

```
S = Dom(v₁) × Dom(v₂) × ... × Dom(vₙ)
```

Since each domain is finite, |S| is finite.

### 4.3 Validity Predicate

A state s ∈ S is **valid** if all invariants hold:

```
Vᵣ(s) = ⋀ᵢ invᵢ(s)
```

The set of valid states: Validᵣ = {s ∈ S | Vᵣ(s)}.

### 4.4 Compensation as Rewriting

Define the **compensation relation** →ᵣ as:

```
s →ᵣ s′  ⟺  ¬Vᵣ(s) ∧ s′ = ρᵢ(s)
```

where i is the smallest index such that ¬invᵢ(s).

This creates an abstract rewriting system (S, →ᵣ).

### 4.5 Normal Forms

The **normal forms** of (S, →ᵣ) are exactly the valid states:

```
NFᵣ = {s ∈ S | ¬∃s′. s →ᵣ s′} = Validᵣ
```

**Proof**: If s is valid, then Vᵣ(s) holds, so no repair fires, so s →ᵣ s′ for no s′.

Conversely, if s is not valid, then some invᵢ(s) is false, so s →ᵣ ρᵢ(s).

Therefore, s is in normal form iff s is valid.

### 4.6 Normalization Function

For a registry R, the **normalization function** NFᵣ : S → S is defined as:

```
NFᵣ(s) = the unique normal form of s (if it exists)
```

This is well-defined only if (S, →ᵣ) is terminating and confluent. WFC provides termination (Section 5) and CC provides local confluence (Section 6); together they yield confluence via Newman's Lemma, proven in Section 7.

---

## 5. Well-Founded Compensation (WFC)

### 5.1 Definition

A registry R satisfies **Well-Founded Compensation** (WFC) if the compensation relation (S, →ᵣ) is terminating.

**Formally**: There is no infinite sequence s₀ →ᵣ s₁ →ᵣ s₂ →ᵣ ...

### 5.2 Well-Founded Orderings

A binary relation < on a set X is **well-founded** if there is no infinite descending chain:

```
x₀ > x₁ > x₂ > x₃ > ...
```

**Equivalently**: Every non-empty subset of X has a minimal element.

Examples of well-founded orderings:
- Natural numbers with standard ordering (ℕ, <)
- Lexicographic ordering on finite tuples
- Multiset ordering

### 5.3 WFC via Well-Founded Measure

To prove WFC, we can define a **measure function** μ : S → ℕ such that:

```
s →ᵣ s′  ⟹  μ(s) > μ(s′)
```

If such μ exists, then (S, →ᵣ) is terminating because ℕ is well-founded.

**Example measure** (naive): Number of violated invariants.

```
μ(s) = |{i | ¬invᵢ(s)}|
```

This doesn't always work because a repair might violate other invariants.

**Better measure**: Lexicographic tuple (depth, violated count), but in general, proving termination requires invariant-specific reasoning.

**The potential need not be a natural number.** The convergence theorem only needs the measure to
be well-founded. The mechanized proof states WFC for a potential into any type with any
well-founded strict order (ordinals, lexicographic products, integers bounded below) and proves
termination, confluence and unique normal forms from it, for the all-pairs and the causal system
alike ([`GovernanceWF.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceWF.v): `governance_wf_confluent`,
`causal_governance_wf_unique_normal_forms`; the instance `zw_confluent` is a registry over the
unbounded integers). So the theorem is domain-independent; finiteness is what gsm's *verification*
needs (§5.4 enumerates), not what convergence needs.

### 5.4 Verification Algorithm

The gsm library verifies WFC by **exhaustive simulation**:

For each state s ∈ S:
1. Compute the compensation sequence: s₀ = s, sᵢ₊₁ = ρ(sᵢ)
2. Track visited states
3. If a state repeats: WFC fails (cycle detected)
4. If sequence exceeds |S| steps: WFC fails (impossible in terminating system)
5. If sequence reaches valid state: record depth

If all states reach validity, WFC passes.

**Complexity**: O(|S|²) in worst case (|S| states, each may need |S| repairs).

---

## 6. Compensation Commutativity (CC)

### 6.1 Event Application

Given a registry R and event set E, each event e ∈ E has:
- Write set Wₑ ⊆ V (variables it modifies)
- Guard gₑ : S → {true, false} (precondition)
- Effect φₑ : S → S (transition function)

Event application:

```
s →ₑ s′  ⟺  gₑ(s) ∧ s′ = φₑ(s)
```

If ¬gₑ(s), then s →ₑ s (no-op).

### 6.2 Normalized Event Application

Define the **normalized step** relation:

```
s ⇒ₑ s′  ⟺  s′ = NFᵣ(s →ₑ ·)
```

In words: apply event e, then normalize via compensation.

This is what the `step` table precomputes.

### 6.3 CC1: Event Commutativity

**CC1** requires that for all independent events e₁, e₂ and all valid states s:

```
s ⇒ₑ₁ ⇒ₑ₂ t₁  ∧  s ⇒ₑ₂ ⇒ₑ₁ t₂  ⟹  t₁ = t₂
```

**Visual**:
```
       s (valid)
      ↙ ↘
  [e₁]   [e₂]
     ↓     ↓
    s₁    s₂ (may be invalid)
     ↓     ↓
   NF(s₁) NF(s₂)
      ↙ ↘
  [e₂]   [e₁]
     ↓     ↓
    s₁′   s₂′ (may be invalid)
     ↓     ↓
   NF(s₁′) NF(s₂′)
      ↘ ↙
      must be equal
```

### 6.4 CC2: Compensation Absorption

**CC2** requires that for all events e and all states s:

```
NFᵣ(s →ₑ ·) = NFᵣ(NFᵣ(s) →ₑ ·)
```

In words: Normalizing before applying an event gives the same result as normalizing after.

**Intuition**: Once normalized, subsequent events should behave "the same" as if applied to the original state (modulo normalization).

### 6.5 Why CC2 Is Needed

CC2 closes a specific critical pair in the convergence proof. The governance rewrite system has two kinds of steps: apply steps (consume an event) and compensation steps (repair an invalid state). When a configuration (σ, B) is both invalid and has enabled events, two rewrites are possible:

1. **Apply first**: apply event e to get (apply(e, σ), B\{e}), then compensate to validity
2. **Compensate first**: compensate to get (ρ(σ), B), then apply event e, then compensate to validity

For local confluence, these must reach the same configuration. Path 1 produces NFᵣ(apply(e, σ)). Path 2 produces NFᵣ(apply(e, ρ(σ))). CC2 states exactly that these are equal.

Without CC2, the convergence proof breaks: a processor that eagerly compensates before applying an event could reach a different state than one that applies first and compensates after. CC1 alone only handles the case where two different events are applied from the same state - it says nothing about the apply-vs-compensate choice.

**Note**: gsm does not check CC2, and CC2 does not hold for every machine gsm accepts. A guard
that reads a variable a repair changes behaves differently on σ and on ρ(σ). For example, with
the invariant "shipped implies paid" repaired by resetting the status to pending, and a payment
event guarded on status = pending, the invalid state {shipped, unpaid} gives NFᵣ(pay(σ)) =
{pending, unpaid} but NFᵣ(pay(NFᵣ(σ))) = {paid, paid}. gsm's runtime does not need CC2 because
it fixes one strategy, eager compensation: every `step` entry is NFᵣ(s →ₑ ·), so after the first
event every state is a normal form, and events are only ever applied to normal forms. The
apply-versus-compensate critical pair never arises. What the runtime needs is CC1 on the states it
applies events to, which is the domain `Build` checks (§9.3): the valid states, plus the zero
state `NewState` returns. Runs started from any other invalid state are outside the guarantee.

### 6.6 Local Confluence

CC1 and CC2 together close all critical pairs of the governance rewrite system:

- **Two apply steps** (different events e₁, e₂ both enabled): closed by CC1
- **Apply step vs compensation step** (state is invalid and event is enabled): closed by CC2
- **Two compensation steps**: impossible (ρ is deterministic, so both steps produce the same successor)

This establishes local confluence of the full rewrite system.

---

## 7. The Convergence Theorem

### 7.1 The Governance Rewrite System

The proof operates on **configurations** (σ, B) where σ is a state and B is a set of received-but-unapplied events. The governance rewrite system G has two rewrite rules:

1. **Apply step**: (σ, B) → (apply(e, σ), B\{e}) when event e ∈ B is enabled (all causal dependencies already applied)
2. **Compensation step**: (σ, B) → (ρ(σ), B) when Vᵣ(σ) = false

A configuration is in **normal form** when B = ∅ and Vᵣ(σ) = true.

The nondeterminism is genuine: when multiple events are enabled and the state is invalid, the system can step via different apply rules or via compensation. Confluence means the choice doesn't affect the final result.

### 7.2 Termination

**Lemma**: Under WFC, G is terminating.

**Proof**: Define the measure μ(σ, B) = (|B|, Φ(σ)) taking values in N x N under the **lexicographic order**, where Φ(σ) is the compensation depth of σ: the number of repair steps in σ's normalization sequence, finite by WFC (Section 5), with Φ(σ) = 0 exactly when σ is valid.

The lexicographic order on N x N is **well-founded**: there is no infinite descending chain. This is because the first component is bounded below by 0, and for any fixed first component, the second component is also bounded below by 0. Any infinite descending chain would require the first component to decrease infinitely (impossible in N) or stay constant while the second decreases infinitely (also impossible in N).

- An apply step decreases the first component (|B| drops by 1), so μ strictly decreases regardless of the second component.
- A compensation step leaves the first component unchanged but strictly decreases the second by WFC.

Since μ maps into a well-ordered set and strictly decreases at every step, every reduction sequence is finite. ∎

### 7.3 Local Confluence

**Lemma**: Under CC, G is locally confluent.

**Proof**: Three critical pair types arise:

**Case 1 (apply/apply)**: Events e₁, e₂ both enabled from (σ, B). Since both are in B and both are enabled, neither is a causal dependency of the other, so they are causally independent. From X₁ = (apply(e₁, σ), B\{e₁}), compensate to validity and apply e₂. From X₂ = (apply(e₂, σ), B\{e₂}), compensate to validity and apply e₁. By CC1, the state components agree. The buffer components are both B\{e₁, e₂}.

**Case 2 (apply/compensate)**: State σ is invalid and event e is enabled. X₁ = (apply(e, σ), B\{e}) and X₂ = (ρ(σ), B). From X₁, compensate to reach (NFᵣ(apply(e, σ)), B\{e}). From X₂, apply e to get (apply(e, ρ(σ)), B\{e}), then compensate to reach (NFᵣ(apply(e, ρ(σ))), B\{e}). By CC2, the state components are equal.

**Case 3 (compensate/compensate)**: Both steps apply ρ to the same state, producing the same successor. Trivial. ∎

### 7.4 Unique Normal Forms

**Corollary**: Under WFC and CC, every configuration (σ₀, E) has a unique normal form.

**Proof**: By the termination lemma, G is terminating. By the local confluence lemma, G is locally confluent. By Newman's Lemma (Section 3), G is confluent. A terminating, confluent rewrite system has unique normal forms. ∎

### 7.5 Stream Convergence

**Theorem**: Let R satisfy WFC and CC. Let P₁, P₂ be processors consuming the same event set E from initial state σ₀, each applying events in some causality-respecting order and compensating after each application. Then P₁ and P₂ reach the same valid state.

**Proof**: Each processor's computation is a reduction sequence in G from (σ₀, E) to some normal form (σ*, ∅). Different causality-respecting orders correspond to different reduction sequences. By the unique normal forms corollary, all reduction sequences from (σ₀, E) reach the same normal form. Therefore σ* depends only on E, not on the order. ∎

### 7.6 Eventual Consistency

The convergence theorem provides **strong eventual consistency**: all replicas processing the same set of events (in any order) reach the same valid state, assuming WFC and CC hold. This is stronger than weak eventual consistency (which only guarantees convergence after quiescence).

### 7.7 The Converse: CC Is Exact on Reachable States

WFC and CC are not only sufficient. Under free delivery, a repair that returns a valid state, and
WFC, every event buffer delivered from a start s₀ has a unique normal form **iff** CC1 and CC2 hold
on the states reachable from s₀ ([`GovernanceConverse.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceConverse.v):
`cc_exact_from`, `cc_exact`; each failure at a reachable state is an observable divergence,
`cc1_fail_diverge_from`). Under causal delivery the exact condition is that governed steps of a
concurrent pair commute wherever the pair can be delivered (`causal_exact`). The naive converse
("CC fails somewhere, so some run diverges") is false: a failure at a state no run reaches, or one
that a later event erases, is harmless (`masked_cc1`, `naive_causal_converse_fails`).

This is why gsm's results read the way they do. `Build` checks CC over **every** valid state (plus
the zero state), not only the reachable ones, because it does not know the start a deployment will
use. That static check is sufficient, so a pass is a guarantee; a failure reports a witness state
from which the two orders diverge, which a particular deployment may never reach.

### 7.8 Delivery: Duplicates and Redelivery

The theorems above are about permutations of one multiset of events: each event delivered exactly
once. Transports usually promise at-least-once delivery. The mechanized account
([`AtLeastOnce.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/AtLeastOnce.v)):

- **A duplicate is absorbed when the event is idempotent** and commutes with what was delivered
  between the copies (`alo_absorbed`); when all delivered events commute, every at-least-once
  delivery of idempotent events reaches the exactly-once result (`alo_commuting_exactly_once`).
- **A non-idempotent event diverges when duplicated** (`non_idempotent_diverges`), from any state
  where its governed step is not idempotent: a capped counter increment delivered twice ends at 2
  where once ends at 1 (`inc_duplicate_diverges`).
- **Under causal delivery, redelivery must itself be causal**: no copy of an event may arrive after
  an event that causally follows it (`causal_alo_exactly_once`). Idempotence alone is not enough
  (`late_duplicate_diverges`): a late redelivery of `add` after its causal successor `remove` sets
  a flag that exactly-once delivery leaves cleared, though both events are idempotent and every
  concurrent pair commutes.

gsm reports which events need deduplication: `Report.NotIdempotent` lists every event whose second
application changes the state from some state `Build` checked, printed as "Delivery: exactly once
for ...". Those events need an event id and a dedupe set (or equivalent) before `Apply`. Events not
listed tolerate duplicates under the conditions above, which are mechanized for free and causal
delivery (normalization-confluence `alo_exact`, `causal_alo_exact`); in causal deployments a
redelivered copy must also not overtake an effect of its own event. For a registry with declared
`Independent` pairs under at-least-once delivery the exact condition is mechanized too
(normalization-confluence `AtLeastOnceDeclared.v`, `dalo_exact`): declared pairs commute at
reachable states, and every event is idempotent where it can occur. The list is sound at reachable
witnesses (`dalo_notidem_needs_dedup`), and it is complete, so deduplicating exactly the listed
events is enough (`dalo_unlisted_converge`, `dalo_gsm_build`), when `Build` passes, reachable states
are valid, and the transport keeps undeclared pairs ordered for redeliveries too. If a retry can
cross an undeclared partner, completeness fails (`fl_retry_order_needed`) and the requirement is
absorption after every history (`dalo_r_exact`).

---

## 8. Footprint Calculus

### 8.1 Variable Footprints

Each invariant invᵢ has a **footprint** Fᵢ ⊆ V, the set of variables it constrains.

The repair ρᵢ may only modify variables in Fᵢ.

**Formally**: For all s ∈ S and all v ∉ Fᵢ:

```
ρᵢ(s)(v) = s(v)
```

### 8.2 Event Write Sets

Each event e has a **write set** Wₑ ⊆ V, the variables it modifies.

**Formally**: For all s ∈ S and all v ∉ Wₑ:

```
φₑ(s)(v) = s(v)
```

### 8.2a Event Read Sets

Each event e also has a **read set** Rₑ ⊆ V: the variables its guard gₑ and effect φₑ depend on.

**Formally**: for all s, t ∈ S that agree on Rₑ, gₑ(s) = gₑ(t), and φₑ(s) and φₑ(t) agree on Wₑ.

The write set says nothing about the read set. A shipment event guarded on payment writes only
`status` but reads `paid`.

### 8.3 Triggered Invariants

Event e **triggers** invariant invᵢ if Wₑ ∩ Fᵢ ≠ ∅.

**Intuition**: If an event modifies a variable that an invariant watches, the invariant might be violated.

### 8.4 Event Footprints

The **footprint** of an event e is everything it reads or writes, closed under the invariants
that can fire as a consequence:

```
Footprint(e) = the least set F ⊇ Rₑ ∪ Wₑ such that Fᵢ ⊆ F for every invariant i with Fᵢ ∩ F ≠ ∅
```

The closure is transitive: if e writes v, invᵢ watches v, and invᵢ's repair writes u, then any
invariant watching u is reached too. gsm's footprint **components** (the union-find partition of
variables over invariant footprints and event write sets) compute this closure; an event's
footprint lies inside its component once its read set does.

### 8.5 Disjointness Theorem

**Theorem**: Suppose every invariant's check and repair read and write only its footprint Fᵢ,
and every event's guard and effect read only Rₑ and write only Wₑ. If two events e₁ and e₂ have
disjoint footprints, then from every valid state they commute.

**Proof sketch**: Applying e₁ changes only variables in Wₑ₁, computed from variables in Rₑ₁. The
invariants that can fire afterwards watch variables in Footprint(e₁), and their repairs change only
variables there. None of these is read or written by e₂ or by the repairs it triggers, so each
event's normalized effect is the same whether or not the other ran first. ∎

The read-set hypothesis is essential and is the precondition of the mechanized
`disjoint_events_commute`, which models an event that reads only its own footprint. Without it
the theorem is false: pay writes {paid}, a shipment guarded on paid writes {shipped}, no invariant
links them, and the two orders end in different states.

`disjoint_events_commute` is the raw half (the two effects commute before any repair). The
normalized statement is mechanized for events on separate components of a product state, with
normalization acting componentwise: CC1 holds at every **valid** state (`calc_components_cc1_valid`
in `Calculus.v`). "Valid" is part of the theorem. At an arbitrary state CC1 holds iff each event
absorbs its own component's repair, N(e(s)) = N(e(N(s))) (`calc_components_cc1_iff`), which an
event need not do: with x ∈ {0,1,2}, invariant x ≠ 2 repaired to 1, and an event that sets x := 0
when x = 2, the pair with any event on another component diverges from x = 2 when each event is
applied raw. gsm never applies an event raw to an invalid state: `Machine.Apply` normalizes an
invalid input first, so every order starts from a valid state, where the theorem applies
(`TestCompositional_InvalidStateCrossComponentPair` checks both halves).

The disjointness is of **state variables**, closed under the invariants that can fire (§8.4).
The paper's footprint theorem stated over disjoint *invariant* footprints with repair locality
is false (`base_thm_footprint_cc1_refuted`, `base_thm_footprint_cc1_raw_refuted`), and gsm does
not use it.

### 8.6 Where gsm uses it

`Build` does not use the theorem: it checks every pair exhaustively from the step tables (§9.3),
which costs two lookups per state per pair and needs no hypothesis about read sets.
`BuildCompositional` uses it to skip pairs whose events lie in different components, after
checking the hypotheses (§9.6).

(Before the fix recorded in the CHANGELOG, `Build` skipped pairs using Footprint(e) =
⋃{Fᵢ | Wₑ ∩ Fᵢ ≠ ∅}, which leaves out Rₑ, leaves out Wₑ itself when no invariant watches it, and
does not follow chains of invariants. Each omission let it certify machines that diverge.)

---

## 9. Verification Algorithm

### 9.1 Phase 1: Normal Form Computation (WFC Check)

**Algorithm**:
```
for each state s in S:
    visited = {}
    current = s
    depth = 0

    while not V_R(current):
        if current in visited or depth > |S|:
            return WFC_FAILURE

        visited.add(current)
        current = first_violated_repair(current)
        depth += 1

    nf[s] = current
    max_depth = max(max_depth, depth)

return WFC_SUCCESS(max_depth)
```

**Correctness**: If the algorithm terminates without failure for all states, then (S, →ᵣ) is terminating, proving WFC.

**Complexity**: O(|S|²) - for each of |S| states, may need up to |S| repair steps.

### 9.2 Phase 2: Step Table Construction

**Algorithm**:
```
for each event e in E:
    for each state s in S:
        s' = apply_event(e, s)
        step[e][s] = nf[s']
```

**Complexity**: O(|E| × |S|) - one event application and one table lookup per (event, state) pair.

### 9.3 Phase 3: CC Verification

**Algorithm**:
```
pairs = compute_independent_pairs(E)

D = { s ∈ S : V_R(s) } ∪ { zero state }      // the guarantee domain

for each (e1, e2) in pairs:
    for each s in D:
        s_12 = step[e2][step[e1][s]]
        s_21 = step[e1][step[e2][s]]

        if s_12 != s_21:
            return CC_FAILURE(e1, e2, s, s_12, s_21)

return CC_SUCCESS(len(pairs))
```

**Correctness**: If the algorithm succeeds, then CC1 holds for every pair checked, on every valid
state and on the zero state. Every step lands on a valid state, so by `run_perm_invariant`
(normalization-confluence `Checker.v`) any two permutations of a list of checked events reach the
same state from any start in D.

**Complexity**: O(|pairs| × |S|) table lookups, small next to the O(|E| × |S|) closure calls of
Phase 2.

### 9.4 Soundness and Completeness

**Soundness**: If the verification algorithm reports success, then WFC holds and CC1 holds for every declared-independent event pair.

**Proof**: Phase 1 simulates each state's compensation sequence to a fixpoint, failing on a repeat or an over-length run, so success means (S, →ᵣ) terminates from every state: WFC. Phase 3 compares the two normalized orderings for every declared-independent pair over every valid state (and the zero state), with no pair skipped, so success means each such pair commutes: CC1. Both are exhaustive over the finite |S|, hence sound. CC2 is not tested and not needed for the runtime, which only applies events to normal forms (§6.5).

**Completeness**: If WFC and CC hold, then the verification algorithm reports success.

**Proof**: Under WFC every compensation sequence reaches a valid state within |S| steps, so Phase 1 never trips its cycle-or-overflow guard and records a normal form for every state. Under CC1 every declared-independent pair commutes on every valid state (and, when the zero state is invalid, on it too, which the zero-state clause of D adds to CC1), so no comparison in Phase 3 fails. Both phases therefore succeed.

A property test (`soundness_property_test.go`) checks both directions on random small machines whose guards read other events' writes, against brute-force enumeration of event orderings: `Build` certifies exactly the machines with no divergent ordering of two events from D, and no certified machine has a divergent ordering of three.

**Scope**: Both directions are relative to the declared independence relation: gsm checks the pairs the registry declares independent (via `Independent`, or all pairs by default). A pair wrongly declared independent when it is in fact causally dependent is a specification error the checker does not police, not an incompleteness of the algorithm.

### 9.5 Synthesis (the inverse problem)

Verification asks: *does this compensation satisfy CC?* The inverse asks: *is there any
compensation that does?* Since one-step compensation is WLOG for the convergence question
(ρ* is a retraction of Σ onto the valid states, and any retraction is realized by a one-step
ρ), synthesis reduces to searching for a **normal-form map** N: Σ_invalid → V such that the
induced step tables satisfy CC1 and CC2. This is a finite CSP: each invalid state's repair
target is a variable over V; CC1/CC2 are the constraints.

`Registry.Synthesize` (see `synthesis.go`) solves it by backtracking with forward-checking
(pruning partial assignments as soon as a fully-determined CC constraint is violated), returning
a convergent compensation or, by exhaustion, proving none exists (with a witness: a critical
pair of already-valid states no repair can reconcile). Because it chooses *among* the many
convergent maps, a preference orders candidates (least-invasive by default) and branch-and-bound
returns the provably minimum-cost one. The problem is NP-hard in general (worst-case
exponential), but the finite state space makes it decidable, and pruning handles registries far
beyond naive enumeration.

### 9.6 Footprint-local verification (beyond global enumeration)

The Phase 1 and Phase 3 algorithms above enumerate the global state space S, which bounds them
to small machines. But WFC and CC are local when the hypotheses of §8.5 hold: events whose
footprints are disjoint commute from every valid state (mechanized: `disjoint_events_commute` for
the raw effects, `calc_components_cc1_valid` for CC1 after normalization). So a registry partitions
into footprint-connected **components** that do not interact, and it suffices to verify each
component over the subspace of its own variables. `Registry.BuildCompositional` does this:
certification cost is exponential in the largest component, not in the whole machine, so a
registry of many independent small invariants certifies even when |S| is astronomically large.

It relies on the hypotheses of §8.5, with each event's read set inside its write set, and checks
them first (`verifyFootprints`). For combinator rules the check is syntactic and exact. For
closures it is a perturbation test, run from every state of the component with all other
variables at zero: every value of each outside variable, and every pair of values of each pair of
outside variables. That detects dependence on one or two outside variables, but not a joint
dependence on three or more, so for closures the hypotheses are tested rather than proved. It
also requires the zero state to be valid and the machine to fit in 64 bits, and it returns a
machine that applies events by computing at runtime rather than by table lookup.

### 9.7 Machine-checked meta-theory

The Convergence Theorem (Newman's Lemma plus the WFC/CC discharge), the soundness of gsm's own
certification (footprint disjointness implies commutation at valid states; potential-decreasing repair
terminates), the federated monotone-cycles result in full (the least fixed point by Kleene
iteration, and asynchronous chaotic order-independent convergence to it), the strict subsumption
of CRDTs as the compensation-free fragment (op-based `cmrdt_SEC` and state-based `cvrdt_SEC`, with
witnesses `witness_not_cmrdt` / `witness_leaves_valid_space` showing the inclusion is proper), and
the decidability of the compensation-free classification (`compensationFree_step_no_repair`: under
it every event-step is exactly its guarded effect, with no repair) are mechanized in
Coq/Rocq, axiom-free (`Print Assumptions` reports "Closed under the global context"), with CI
that gates on the axiom-free property. The development has since grown to cover the converse of
CC (§7.7), WFC over any well-founded order (§5.3), at-least-once delivery (§7.8), the monotone
regime under the ascending chain condition instead of finite height (`ChaoticACC.v`), event
interleavings in federations (§11.4), and the soundness of a holonomy-minimal coordination plan
(`CoordinatedCycles.v`; see [HOLONOMY-COORDINATION-DESIGN.md](design/HOLONOMY-COORDINATION-DESIGN.md)). At the time of writing the gate
checks 254 headline results; the current list is in the
[mechanized proof](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq)'s README.

Beyond the meta-theory, gsm's own per-machine verification is **differentially tested** against
the proof, at two different trust boundaries. Both checkers are extracted from the Coq
development (`coq/extraction`) to runnable binaries, so a bug in gsm's hand-written Go verifier
cannot make a non-convergent machine pass:

Both checkers decide exactly what §9.4 says the algorithm verifies: WFC over the whole state
space, and CC1 for every declared-independent pair over the valid states plus the zero state.
The conclusion they are proven to support is the runtime guarantee of §6.5: from a valid state
or the zero state, event sequences that differ only by the order of adjacent declared-independent
events reach the same state (trace equivalence; with every pair declared, any permutation).

1. **Table oracle.** `Machine.WriteConvergenceTables` emits a built machine's step tables, its
   normal-form table, and the declared pairs, and the extracted checker re-certifies, independently
   of gsm's Go, that normal forms and steps land on valid states (`nf[s] = s`) and that the
   declared pairs commute on the valid states and the zero state. It trusts that gsm computed the
   tables, and confirms those tables converge. Proven sound in Coq via `check_tables_converges`
   (`TableCheck.v`); the extracted checker runs `check_fast` (`TableFast.v`), proven equal to
   `check_tables` (`check_fast_eq`), which handles gsm's largest machines. Cross-checked in `oracle_test.go` and `oracle_buildspec_test.go`
   (`GSM_CONVERGENCE_CHECKER`).
2. **Rules oracle.** `Registry.WriteMachineAST` serializes the combinator **rules** themselves
   (and `Registry.WriteDeclaredPairs` the declared pairs, as a separate file), and a second
   extracted checker recomputes each event's step function by evaluating the rule AST (apply the
   event if its guard holds, then normalize by iterated repair), checking WFC from every state and
   CC1 as above. It trusts neither gsm's enumeration nor its tables: it re-derives the verdict
   straight from the declarations. Proven sound in Coq via `checkBuild_converges` and
   `checkBuild_wfc_terminates` (`AstChecker.v`). Cross-checked in `astoracle_test.go` and
   `oracle_buildspec_test.go` (`GSM_AST_CHECKER`). The same oracle also emits a
   machine-checked `compensation_free` verdict (whether repair is ever needed on any in-domain
   state), so the CRDT-fragment classification is certified from the rules, not asserted (Coq:
   `compensationFree_step_no_repair`).

**Fragment covered by the rules oracle.** The Coq model of the rules is precise about its scope,
and `WriteMachineAST` returns an error for anything outside it, so a passing differential test
always compares semantics-identical checkers:

- **Variables** range over `min .. min+domain-1` for any `min`, negative included; the state
  stores the raw `0..domain-1` offset.
- **Predicates:** comparisons `le`, `lt`, `eq`, `ge`, `gt`, `ne`, and the boolean combinators
  `and`, `or`, `not`.
- **Transforms:** `Set`, `Add`, `Sub`, over gsm's signed integers, with a write clamping into the
  variable's range. The oracle refuses to certify (exit 1, "outside the certified fragment") an
  expression that could exceed 2^31-1 in magnitude, and a write that could store a negative value
  into a two-valued variable with minimum 0.
- **Events:** an effect transform with an optional guard predicate (a guarded event is a no-op
  when its guard is false).

Adding a construct to this fragment (as was done for disjunction, guards, and nonzero minimums)
is a deliberate extension of the grammar in both Go and Coq, not a per-machine cost.

**You do not continuously port machines to Coq.** What is mirrored across the two languages is
the fixed combinator *grammar* (the handful of expression, predicate, and transform forms above),
mirrored once in Go (`combinator.go`) and once in Coq (`AstChecker.v`). An individual machine is
*data* in that grammar; it is never ported or re-proven. You write your rules once in Go, and the
already-proven checker consumes them. Coq is touched again only when a brand-new grammar primitive
is added, which is rare and deliberate. And drift between the two mirrors is caught mechanically:
the differential test fails the moment gsm's Go evaluator and the Coq evaluator disagree on any
machine. This is the standard "trusted core mirrored in a proof assistant" pattern (a verified
compiler mirrors its source-language semantics in the proof assistant the same way), with cost
proportional to the size of the grammar, not to the number of machines built with it.

**The serialized policy is a portable, verifiable artifact.** `Registry.PolicyBytes` returns the
canonical serialization above, and `Registry.PolicyDigest` a stable, domain-separated SHA-256 over
it (a rule written with the sugar surface digests identically to the equivalent primitive
combinators). Because the digested bytes are exactly the rules oracle's input, an anchored digest
and the re-checked artifact cannot diverge. This is the interchange contract that lets a system
outside gsm commit a policy's identity (for example in a cryptographic audit log) and hand the same
bytes to the extracted oracle for an independent convergence verdict. The format addresses
variables and events by position, so the digest does not cover their names, enum labels, variable
kinds or the declared pairs; `Registry.PolicyIdentityDigest` covers those as well, and moving them
into the format is planned before 1.0. `State.Digest` is the
companion primitive for a state: a stable, domain-separated hash over the packed state value, well
defined because the policy pins the layout. Because the convergence engine is deterministic given a
policy and an event set, a verifier can reproduce a committed state digest by replaying the same
events over a reference build, a per-run check of the actual execution against the verified model
(distinct from the refinement question, which asks about all possible inputs and remains open).

---

## 10. Comparison to Related Formalisms

### 10.1 Conflict-Free Replicated Data Types (CRDTs)

**CRDTs** require operations to commute directly:

```
op₁ ; op₂ = op₂ ; op₁
```

**Difference from gsm**:
- CRDTs: operations naturally commute (no violation possible)
- gsm: operations can violate invariants, compensation restores convergence

**Example**:
- CRDT counter: increment operations commute naturally
- gsm counter: increment can violate max bound, compensation clamps value

**Trade-offs**:
- CRDTs: stronger requirement (hard to express business rules)
- gsm: weaker requirement (can enforce invariants, but requires verification)

**Subsumption (machine-checked)**: CRDTs are not merely comparable to gsm, they are a *special case* of it. A CRDT is a governed machine whose operations were designed so compensation never fires: a machine with compensation depth zero on every reachable state. Normalization confluence keeps the convergence guarantee after dropping that design restriction, so every CRDT embeds as a gsm registry, and the inclusion is **strict**: governed machines exist that no CRDT can express, because they leave the valid space and rely on compensation to return. This is proven axiom-free in `coq/CRDT.v` for both op-based (`cmrdt_SEC`) and state-based (`cvrdt_SEC`) CRDTs, with the strictness witnesses `witness_not_cmrdt` and `witness_leaves_valid_space`; the full statement is in [SUBSUMPTION.md](https://github.com/blackwell-systems/normalization-confluence/blob/main/SUBSUMPTION.md). Under causal delivery, where only concurrent operations must commute, the relationship is exact: a compensation-free governed system satisfies the causal convergence condition **iff** it is an op-based CRDT (`compensation_free_exact` in [`CausalReplay.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CausalReplay.v), with `causal_cmrdt_SEC` for the forward direction). So op-based CRDTs are *exactly* the compensation-free fragment, not just contained in it. The trade-off bullets above are the pragmatic view; the structural relationship is containment, not peerage. The compensation-free corner is itself a decidable, extracted classification (see §9.7), so gsm can tell you whether a given machine is in the CRDT fragment.

### 10.2 Operational Transformation (OT)

**OT** transforms concurrent operations to maintain convergence:

```
op₁ ; transform(op₂, op₁) = op₂ ; transform(op₁, op₂)
```

**Difference from gsm**:
- OT: dynamically transforms operations based on context
- gsm: statically precomputes all convergent results

**Trade-offs**:
- OT: flexible (infinite state spaces), but transform correctness is hard to verify
- gsm: verified (finite state spaces), but requires enumeration

### 10.3 Invariant Confluence (I-confluence)

**I-confluence** (Bailis et al., 2014) allows operations that preserve invariants to execute without coordination:

```
If inv(s) and op₁(s), op₂(s) both preserve inv, then they commute
```

**Difference from gsm**:
- I-confluence: operations must preserve invariants
- gsm: operations can violate, compensation repairs

**Trade-offs**:
- I-confluence: no compensation needed (faster), but restrictive
- gsm: compensation required (slower build), but more flexible

### 10.4 Transaction Processing

**ACID transactions** use locking/2PC to ensure linearizability:

```
Serialize all transactions to avoid conflicts
```

**Difference from gsm**:
- Transactions: coordinate to prevent divergence
- gsm: allow divergence, prove compensation converges

**Trade-offs**:
- Transactions: strong consistency, but poor availability
- gsm: eventual consistency, high availability

### 10.5 Abstract State Machines (ASMs)

**ASMs** (Gurevich) model state transitions with update rules:

```
if cond(s) then s' = update(s)
```

**Difference from gsm**:
- ASMs: general computational model
- gsm: specialized for convergent event processing

**Relationship**: gsm can be viewed as ASMs with specific termination and confluence guarantees.

### 10.6 The Convergence Lattice: Floor and Ceiling

Placing gsm among these formalisms gives its regime a floor and a ceiling, established by opposite kinds of argument.

**Floor.** CRDTs and invariant confluence are the *compensation-free* fragment: operations designed so repair never fires (compensation depth zero on every reachable state). This is a strict lower bound proven by construction: every such machine embeds as a gsm registry (§10.1, `cmrdt_SEC` / `cvrdt_SEC`), and membership is decidable and extracted (`compensationFree_step_no_repair`, §9.7). gsm can detect when a machine sits on the floor.

**Ceiling.** The top of the coordination-free lattice is normalization confluence itself. CC is necessary, not merely sufficient, once it is quantified over the states a run can reach (§7.7, `cc_exact_from`), so there is no strictly more general coordination-free convergence regime to climb into. What lies *above* is not a larger free-lunch regime but coordination: consensus and serialization (§10.4), which buy the cases compensation cannot. That boundary is CC-satisfiability, and it is marked constructively. `Registry.Synthesize` (§9.5) returns an **impossibility witness**, a critical pair of distinct valid states no repair can reconcile, exactly when a machine crosses it. Where the floor is detected by embedding, the ceiling is detected by counterexample: gsm certifies both edges of its own regime.

**A caveat on "ceiling".** This is the ceiling of *compensation as a mechanism*. Whether the CC-satisfiability frontier coincides with the absolute limit of coordination-freedom is subtler: invariant confluence (Bailis et al., §10.3) is a necessary-and-sufficient characterization for the invariant-preserving subcase, while WFC + CC is a constructive sufficient condition via one mechanism. A system that is coordination-free by some non-compensation argument could still fall on the impossible side of `Synthesize`. gsm marks the edge of what compensation reaches, which is the edge that matters when building on gsm.

---

## 11. Limitations and Extensions

### 11.1 Finite State Spaces

**Limitation**: gsm requires finite state spaces for exhaustive verification.

**Consequence**: Cannot model:
- Unbounded integers, strings, lists
- Recursive data structures
- Infinite domains

**Mitigation**:
- Bound domains to reasonable ranges (e.g., balance ∈ [0, 1000000])
- Use symbolic verification for unbounded domains (future work)

The limitation is gsm's, not the theorem's: convergence holds on infinite domains under any
well-founded potential (§5.3), and the monotone regime needs only the ascending chain condition,
not a finite lattice (`ChaoticACC.v`). What needs finiteness is exhaustive verification.

### 11.2 State Space Explosion

**Limitation**: The global state space grows as the product of variable domains, so `Build`'s
exhaustive enumeration is capped (currently 2²⁰ ≈ 1M states).

**Example**: 10 variables with 10 values each = 10¹⁰ states, too large for `Build`.

**Mitigation (implemented)**: `Registry.BuildCompositional` verifies each **footprint component**
independently over its own subspace, so certification cost is exponential in the largest
component rather than the whole machine (see §9.6). The 10-variable example above certifies
instantly when its invariants are footprint-local, even though the global space is 10¹⁰. This is
the modular-verification mitigation, resting on the mechanized footprint-disjointness results
(`disjoint_events_commute`, and `calc_components_cc1_valid` for CC1 at valid states, the only
states a compositional machine applies events to) and on a build-time footprint check (exact for
combinator rules; a perturbation test for closures, see §9.6).

**Further mitigations (future work)**: symmetry reduction (exploit equivalent states); partial
order reduction (ignore irrelevant interleavings); symbolic verification for unbounded domains.

### 11.3 Dynamic Event Sets

**Limitation**: Event set is fixed at build time.

**Consequence**: Cannot add new events at runtime without reverification.

**Use case**: Systems where event types evolve over time.

**Future work**: Incremental verification when adding events.

### 11.4 Multi-Registry Systems

This section states the federated results formally. How to use them is in [Federation](federation.md) and [Deployment](deployment.md).

**Implemented**: gsm federates multiple registries via directed morphisms (Section 8 of the paper), see `federation.go` (`Federation` / `FedMachine`). Cross-registry constraints are morphism invariants; the authority argument (a source deterministically fixes its target's shared component) makes inter-registry compensation coordination-free. For a tree-shaped network, M1 (every morphism preserves validity under overwrite) makes the federated repair terminate in a valid normal form; it does not by itself make event orders converge (the paper's federated-convergence theorem from M1 and component WFC + CC alone is false as stated: `FederationGRS.v`, `fed_thm_fed_convergence_refuted`; corrected with C1 + C2 in `fed_thm_fed_convergence_guarded`). Event order across registries is checked by the two conditions of the proof repository's `FederationEvents.v`: C1, cross-registry CC (a target event commutes with every source-driven change of its shared component, `CrossOrderError`), and C2, repaired CC (two target events that commute locally still commute with the morphism repair between them, `SameTargetOrderError`). Static C1 + C2 are sufficient for every interleaving of independent events to converge in an acyclic federation (`fed_events_commute`, `static_c1_c2_gc`) and on a monotone cycle (`FederationEventsCyclesCheck.v`, `cyc_check_gc_lfp`; see Monotone cycles). The exact condition is the same two equations restricted to the witnesses a run can produce: all trace-equivalent sequences from a start converge iff C1 and C2 hold at reachable witnesses (`FederationEventsConverse.v`, `fed_exact`), and the naive converse is false (`naive_converse_fails`). gsm checks the static form over every valid source state, so a pass is a guarantee and a failure means the federation *may* diverge from the reported witness. (These are the federated conditions C1 and C2 of `FederationEvents.v`; C2 here is "repaired CC", unrelated to the single-registry CC2 of §6.4.) The compositional-collapse result (Section 8) is realized as a portable certificate: `Federation.Certify` / `EmbedCertified` reuse a verified sub-federation pinned to its certificate (the seam is checked, and the internal morphisms are re-checked from their tables and their live closures), and `Certificate.Verify` re-derives the federated conditions from extracted morphism tables independently of the producer's closures (see [CERTIFICATE-DESIGN.md](design/CERTIFICATE-DESIGN.md)).

**Multi-source**: when a target has several independent sources the authority argument no longer picks a winner. The paper's *Federated Convergence with Resolution* theorem covers this: the target carries a `Resolver` (a source-determined (R1), validity-preserving (R2) merge) generalizing the authority function, and convergence holds on any acyclic network (single-source authority is the special case). Because the merge is domain policy, the theorem is stated conditionally on R1/R2, exactly as the single-source case is conditioned on M1. gsm certifies those hypotheses: `Build` **exhaustively verifies** (over every reachable combination of valid source states) that the resolver writes only shared variables, satisfies R1, and satisfies R2: the same verify-the-preconditions discipline gsm applies to single-registry WFC/CC. A multi-source target without a resolver, or a resolver that violates R1/R2, is rejected.

**Monotone cycles**: acyclicity is required only to tame *non-monotone* repair: the cyclic divergence counterexample uses the antitone negation `1-x`. When shared domains are ordered (lattice) and every morphism/resolver is **monotone**, the federated repair operator has a least fixed point (Knaster–Tarski) reached order-independently by chaotic iteration, so convergence holds on *any* cyclic graph (the *Monotone Convergence Despite Cycles* theorem). `Federation.AllowMonotoneCycles()` opts in: `Build` verifies monotonicity per node by enumeration and `Normalize` runs Kleene iteration to the least fixed point. State-based CRDTs are the compensation-free special case; non-monotone cycles are rejected. Acyclic structure and monotonicity are independent routes to confluence. For *events* on a monotone cycle the exact condition is global, that federated-independent events commute after full re-normalization at every reachable state (`FederationEventsCycles.v`, `gc_iff`, `cyc_events_converge_iff`; `cyc_counterexample` is a monotone cycle whose events diverge), but gsm's per-target C1 and C2 imply it (`FederationEventsCyclesCheck.v`, `cyc_check_gc`, `cyc_check_gc_lfp`). `normalizeCyclic` resets every shared variable to bottom, so the shared part of a normal form is a function of the locals and an event's shared writes are erased (`cyc_check_step`). With H_j the images of the repair into target j over every valid source state, C1 on a cycle says the locals of ρ_j(e(x, v')) are the same for every v' in H_j at valid (x, h) with h in H_j, and C2 says declared-independent pairs on j commute on locals with the shared part held at h between them; gsm's checks compare exactly the locals after the final overwrite over exactly that H_j (for a multi-source target, the resolver's image R over every combination of valid source states, the joint image of all incoming edges; the per-edge combination route, `multi_edge_c1` / `multi_edge_gc` in `FederationEventsCyclesMulti.v`, needs M1 and is not used, and per-edge images alone are insufficient for a resolver, `resolver_edge_insufficient`). Neither condition can be dropped: `cyc_counterexample`'s latch fails C1 (`check_rejects_latch`), and `c1_localcc_insufficient` is a monotone cycle where C1 and each registry's own CC hold, C2 fails, and two orders diverge. All three are regression tests in `federation_cycle_events_test.go`. When a cyclic network is rejected, `Federation.DiagnoseCycle` names the offending loop and reports whether its repair settles or orbits from the zero seed (the loop-composite fixed-point witness). An orbit from one seed is not an obstruction unless the loop acts freely (`CohomologyGeneral.v`, `c15_definitive_claim_false`); a section exists iff some seed reaches a fixed point (`thm_obstruction_reachable`, `c15_exact_refuter`), so when the zero seed orbits `DiagnoseCycle` checks every seed of the cycle's finite fiber and reports `Obstructed()` only when none is a fixed point. `Federation.CoordinationPlan` and `BuildCoordinated` accept a non-monotone cycle by removing a feedback edge set to external coordination; the target of each removed edge is the authority root its cycles are driven from, so the normal form is unique given the plan but depends on which edge was cut (`CoordinatedCycles.v`, `root_choice_matters`; `copyback_without_authority` shows a root is needed at all), and each `CoordinationPoint` names it (`Authority`); the sharper holonomy-minimal plan (coordinate only unbalanced cycles) now has a mechanized soundness theorem (`CoordinatedCycles.v`, `coordinated_sound`, `plan_exact`) and is designed, not yet implemented, in [HOLONOMY-COORDINATION-DESIGN.md](design/HOLONOMY-COORDINATION-DESIGN.md).

**Distributed propagation (projections)**: a deployment need not run the `FedMachine`. Each node can run only its own component machine (`FedMachine.Component`), apply local events, and merge its sources' shared projections (`FedMachine.SharedProjection`, `Machine.MergeProjection`, `MergeProjectionAfter`) whenever they arrive, so propagation is a separate step interleaved with local events rather than a repair after every event. This is the distributed model of `FederationEvents.v`: a run is any word of local events `DEv e` and propagation steps `DProp j`, where `DProp j` overwrites registry j's shared component with the image of its sources' current states. Writing `ow(b, v)` for the overwrite of b's shared component with image v and `ρ_B(e(·))` for the component machine's `Apply`, the condition is

- **XU**: for every target event e, every image v of a valid source state (or source combination), and *every* valid target state b, `ow(ρ_B(e(ow(b, v))), v) = ow(ρ_B(e(b)), v)`.

C1 is the same equation restricted to b whose shared component is itself an image; XU drops that restriction, because a node can apply an event to a state whose shared component a local event has just overwritten. Under XU and each registry's own CC, in an acyclic federation, every word, once propagation completes, reaches exactly the `FedMachine` run of its events (`propagation_flush`), so all words whose event sequences are federated-trace-equivalent agree (`dist_interleavings_converge`); XU and local CC imply C1 and C2 (`xu_implies_c1_c2`). C1 and C2 alone are not enough for this model (`FederationGRS.v`, `fed_grs_c1_c2_insufficient`, where every `FedMachine` order converges but the unrestricted system has two normal forms; corrected statements `fed_thm_fed_cc_corrected` and `fed_thm_fed_convergence_corrected` assume XU). `Federation.Build` therefore keeps C1 + C2 as its requirement, which is exact for the `FedMachine` model it certifies, and checks XU separately on an acyclic network, by enumeration over every valid target state, every target event and the image set C1 already computed (|events| x |valid(target)| x (|Img| + 1) table lookups per target). The result is reported in `FedReport.ProjectionSafe`, a `Checks` line, and `FedReport.ProjectionWitnesses` (`ProjectionOrderError`: target, event, state, source, and the merge-first and event-first results); `Federation.RequireProjectionSafe()` turns a failure into a build error (wrapping `ErrProjectionNotCertified`). `ProjectionSafe` is false on a cyclic network (the theorem uses the topological order) and on a network with a multi-source target (`SharedProjection` sends one edge's `Map` image, not the resolver's merge over all sources, which is the propagation step the theorem covers). M1 makes each merge preserve validity; it is not a convergence argument for this model.

**Distributed propagation on monotone cycles**: the cyclic "not certified" is required, and the conditions that would certify a cyclic projection deployment are known (`DistributedCycles.v` in [normalization-confluence](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/DistributedCycles.v), which lands with [PR #62](https://github.com/blackwell-systems/normalization-confluence/pull/62)). Model: a state is `(l, h)`, the locals of every node and every shared value in a finite lattice; `F l` is the synchronous repair, `Lfp l` its least fixed point (gsm's Kleene loop from bottom), and `Nc (l, h) = (l, Lfp l)` is `FedMachine.Normalize`. A run interleaves local events, per-target propagation steps `h := u l j h` that read the sources' current, possibly stale, values, and (only in the epoch protocol) a reset of every shared value to bottom. On a cycle `F l` can have several fixed points, and `normalizeCyclic` selects the least by resetting to bottom; projection nodes do not reset. Without events, every fair schedule from a start at or below `Lfp l` settles at `Lfp l` (`q1_below`), but from at or above a non-least fixed point no schedule reaches it (`q1_stuck`); from an arbitrary stale start the result can depend on the schedule (`dist_schedule_dependence`) or never settle (`dist_ring_livelock`, a monotone 3-cycle); every fair schedule from every start settles at `Lfp l` iff `F l` has one fixed point (`q1_unique_iff`). With events, `dist_cyc_ghost` is the obstruction: on the flag cycle of `cyc_instance` (`sa := lb || sb`, `sb := la || sa`) with raise and clear events, cyclic C1 and C2 hold, the cyclic reachable XU (`XUcR`: an event's local outcome is the same whether the shared part is stale or at `Lfp`) holds from every start, and the `FedMachine` converges from every normal form, yet `RaiseA; propagate B; propagate A; ClearA` is quiescent at the ghost `((false, false), (true, true))` while the `FedMachine` gives `((false, false), (false, false))`, and no schedule leaves the ghost. Two deployments are certified, neither implemented as a gsm API:

- **Reset epochs (the general fix).** An epoch is a barrier at which every node resets its shared values to bottom, then propagation to quiescence (at most the `FedMachine`'s Kleene bound in sweeps of every target), with no event inside it; any reset followed by propagation to quiescence ends at `Nc t` (`epoch_flush`). Comparing after a final epoch, every run agrees with the `FedMachine` iff `XUcR` (`epoch_agree_iff`), and all interleavings agree iff `XUcR` and `FedMachine` convergence from `Nc s0` (`epoch_conv_iff`, the cyclic form of `dist_exact_tc`). gsm's per-target cyclic C1 and C2 give both when the image set covers the reachable shared values: the start's values, each slot's bottom value, the morphism images and the values events write (`lens_epoch`, `hs_reach`); the cost is today's cyclic C1/C2 enumeration over that larger set. A staggered reset (A resets while B holds the ghost) re-creates the ghost in one propagation step, so the reset must be a barrier (`dist_cyc_epoch_fix`). States between epochs are not certified.
- **No resets.** Under `XUcR`, a quiescent state can differ from the `FedMachine` only by sitting at a larger fixed point (`quiet_ghost_only`). `DistributedCyclesExact.v` ([PR #64](https://github.com/blackwell-systems/normalization-confluence/pull/64); statements in [`coq/docs/distributed.md`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/docs/distributed.md#the-no-reset-model-on-monotone-cycles-exactly-distributedcyclesexactv)) makes the regime exact with no hypothesis left of the iff: `FlushR /\ DAgreeQ <-> XUcR /\ FlushR /\ NoGhostR` (`flush_agree_iff`), the same with `FairFlushR` (`fair_agree_iff`), and with `DConvQ` and `FMConv` added (`flush_fed_iff`, `fair_fed_iff`); each conjunct is necessary, by an instance where it alone fails (`ghost_exact` is `dist_cyc_ghost` as the `NoGhostR` instance). With gsm's per-target cyclic C1 and C2 over a set covering the reachable shared values, a no-reset deployment is certified exactly when it flushes and has no reachable ghost (`lens_noreset_iff`, `lens_noreset_fair_iff`). Both are characterized: a fair run reaches quiescence iff it reaches a sound state, `h <= F l h` (`fair_flush_sound_iff`); `NoGhostR` holds iff the start and every post-event state are ghost-free (`noghost_event_iff`), and under `SoundR` (every reachable state sound) `NoGhostR <-> LowR` (`noghost_soundr_iff`), with `LowR` checked per event (`lowr_post_iff`). The cheap sufficient routes are inflationary events, a per-event check (locals rise in an order the morphisms are monotone in, no shared value raised; they keep soundness, `infl_evsound`, and lowness, `evlow_lowr`, `infl_evlow`), and a unique fixed point for every locals assignment, a global check that rules out ghosts (`uniq_agree`; generalized to an invariant whose states have one-fixed-point locals or are low, `unique_or_low_noghost`), which still needs a flush argument (`flip_noflush`); otherwise `FlushR` and `NoGhostR` are a global reachability search. A clear event on an unpinned feedback loop (the flag cycle) cannot be certified without resets (`ghost_exact`). Convergence among quiescent interleavings alone, without agreement with the `FedMachine`, is also exact (normalization-confluence `DistributedConvergenceExact.v`, `conv_quiet_exact`, closing `REGIME-AUDIT.md` gap 14): runs converge exactly when every reachable state flushes to at most one quiescent state, events and flushes commute up to a common flush, and the flush-then-apply machine is order-independent, with the canonical state allowed to be a ghost. Agreement with the `FedMachine` is that convergence plus no ghost (`agree_conv_noghost`). gsm certifies agreement with the `FedMachine`, so the stronger property is the one it would check.

A deployment that runs the `FedMachine` itself (for example one `FedMachine` over a shared, totally ordered log) is unaffected, since `normalizeCyclic` performs the reset on every normalization.

### 11.5 Probabilistic and Timed Events

**Limitation**: Events are discrete and untimed.

**Extension possibilities**:
- Stochastic events with probability distributions
- Timed events with deadlines and timeouts
- Continuous-time compensation

**Challenge**: Verification becomes undecidable or requires approximation.

### 11.6 Non-Deterministic Repairs

**Limitation**: Repairs are deterministic functions.

**Extension**: Allow repairs to non-deterministically choose from multiple valid outcomes.

**Consequence**: Normal forms may not be unique, weakening convergence guarantee to "converge to equivalent states" rather than "identical states."

### 11.7 Partial Order Relations

**Limitation**: Invariants fire in declaration order (total order).

**Extension**: Allow partial order on invariants, firing all minimal violated invariants concurrently.

**Challenge**: Requires verifying that concurrent repairs don't interfere.

### 11.8 Keyed Collections (Symmetry)

**Implemented**: `NewCollection[K](over, template).Build()` verifies one item registry, the
template, with `Build`, and returns a machine that runs it at every key
([Getting started](getting-started.md#collections-one-template-every-key)). The theory is
normalization-confluence
[`SymmetryCutoff.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/SymmetryCutoff.v)
(axiom-free; the design notes are its `coq/docs/symmetry.md`).

**The model.** An item registry is lifted to a collection over n keys: states are one item per
key, the event "e at k" acts on item k only, repair acts on each item by the item's repair, and
the invariant is the conjunction of the item invariants. A registry is *independent and
identically governed* (`IdGov`) when an event or repair at k reads and writes only item k, the
rules are invariant under exchanging two keys, and the invariant is a conjunction of item
invariants. Every lift satisfies `IdGov` (`lift_idgov`), and for n ≥ 1 a registry satisfies it
iff it is the lift of its one-item restriction (`idgov_lift`). A collection is a lift by
construction: a template rule sees one item's state and no key, and every key runs the same
machine. So gsm needs no check of the hypotheses; there is nothing a user can write through the
API that violates them.

**What gsm runs.** Each key holds the template machine's state, and `Apply(s, k, e)` replaces item
k by the template's `Machine.Apply` of it: the lifted step `lst` of `SymmetryCutoff.v`, with the
template's governed step (event, then repair) as the item step. The guarantees map as follows.

| Report result, for a collection | Theorem |
|---|---|
| The collection's run at key k is the template's run of the events addressed to k | `run_proj` |
| Every interleaving of a delivery converges at every key, for any number of keys, iff it does for the template (exactly-once and at-least-once delivery) | `run_proj`, `alo_cutoff`, `alo_cutoff_uniform` |
| Events on different keys commute at every state, with no hypothesis | `cross_commute` |
| `NotIdempotent`: an event is idempotent at key k iff it is idempotent for the item at k; commutation and idempotence at reachable states reduce per item | `idem_reduces`, `alo_cutoff_exact` |
| `CausalOrderRequired`, declared pairs: every cross-key pair counts as declared, and the template's declared pairs commute at every key iff they commute for the template | `declared_cutoff` |
| WFC: the collection's repair has a potential iff the item's does | `wfc_cutoff` |

Each condition has cutoff 1: one item is the whole check, which is what the report line
`Verified by symmetry over <key> (items independent; cutoff 1)` states.

**CC1, CC2 and the cutoff.** `SymmetryCutoff.v` also treats the governance rewrite system of §7,
where repair may be delayed and then runs on every item at once. There, unique normal forms have
cutoff 1 given CC1 and CC2 together (`cc_lift_iff`, `un_cutoff`, `un_cutoff_global`, and for any
registry satisfying `IdGov`, `symmetry_sound`), while CC1 alone has cutoff 2 (`cc1_cutoff`, tight
by `cc1_cutoff_tight`): a repair pending on one item can be taken before or after an event on
another, and the two orders agree iff, on each item, repair then event equals the event alone
(`cross_item_reduces`), which the item's CC2 implies (`cc2_star`). gsm
checks CC1 and not CC2 (§6.5), so it does not claim the rewrite-system form. It does not need it:
gsm repairs eagerly, per key. An event at one key never runs repair on another key's item, so no
repair is pending across keys, and the cutoff-2 case cannot arise. From a valid collection the two
models coincide anyway: the governed collection step is the lift of the governed item step
(`gL_lst`, `run_bridge`, `alo_gov_cutoff`), and at valid states events on different keys commute
with no hypothesis (`cross_valid_commute`).

**Boundaries.** An aggregate rule (the invariant "total reserved across items is at most 1", with a
repair that cancels a reservation on every positive item) converges on one item and diverges on
two (`aggregate_diverges`, `agg_item_un`) and fails `IdGov` (`agg_not_idgov`); with the per-item
invariant the same events converge at every n (`itemwise_converges`). Items governed by different
rules can pass on one item and diverge together (`nonidentical_misleads`). Neither can be written
through a collection: every rule sees one item, and every key runs one machine. The tests
`TestCollection_NaiveAggregateRegistryFailsBuild` (the aggregate, written by hand as one two-item
registry, fails `Build`) and `TestCollection_AggregateCannotBeExpressed` pin both sides. The
federation conditions C1 and C2 also reduce to one item for a morphism that maps items pointwise
(`c1_cutoff`, `c2_cutoff`); gsm does not implement collections as federation components.

---

## 12. Relationship to the Paper

This library implements both the **single-registry governance model** (Section 3) and the **federated convergence model** (Section 8) of the paper:

- **Registry** = the machine definition (variables, invariants, compensation, events)
- **WFC** (§3) = well-founded measure on compensation depth
- **CC** (§3) = compensation commutativity (CC1 + CC2)
- **Convergence theorem** (§5) = WFC + CC ⟹ unique normal forms (via Newman's Lemma). Mechanized axiom-free for any well-founded repair potential, so the theorem does not need finite domains (only gsm's enumeration does), and with the exact converse: CC on the reachable states holds iff every order converges ([`GovernanceWF.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceWF.v), [`GovernanceConverse.v`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/GovernanceConverse.v))
- **Verification calculus** (§10) = footprint components + decomposable repair (`BuildCompositional`, in `compositional.go` and `footprint.go`)
- **Federation** (§8) = registry morphisms + the authority argument + the constructive normalizer ρ_Fed (`federation.go`: `Federation` / `FedMachine`)
- **Multi-source resolution** (§8) = resolution operators (`Resolve`)
- **Monotone cycles** (§8) = convergence on cyclic networks under monotone repair (`AllowMonotoneCycles`)
- **Compositionality** (§8) = sub-federations collapse to effective registries (`Embed`)

The paper proves: **if WFC and CC hold, all processors consuming the same events converge to the same valid state regardless of application order** - that this extends to a tree-shaped network of registries connected by validity-preserving morphisms, and further to any acyclic network whose multi-source targets carry a source-determined, validity-preserving resolution operator; that even *cyclic* networks converge when repair is monotone; and that verified sub-federations compose.

This library verifies: **does your machine satisfy WFC and CC?**

---

## Summary

The mathematical foundations of gsm rest on three key pillars:

1. **Abstract rewriting theory**: Compensation is a rewriting system; normal forms are valid states.

2. **Newman's Lemma**: Termination + local confluence → global confluence. WFC provides termination, CC provides local confluence.

3. **Finite enumeration**: Exhaustive verification is possible because each variable's domain is finite. `Build` enumerates the global product (capped at ~1M states), while `BuildCompositional` and federation verify per footprint component, so the machine's global state space may itself be astronomically large.

Together, these provide a **constructive proof** of convergence: if verification passes, convergence is mathematically guaranteed.

The trade-off is clear:
- **Gain**: Proven convergence, no runtime coordination, O(1) event application
- **Cost**: Finite variable domains, build-time verification overhead, and a ~1M-state cap on monolithic `Build` (lifted by compositional and federated verification)

For business logic state machines within these constraints, gsm provides convergence guarantees that are difficult or impossible to achieve with other approaches.

---

## References

### Foundational Papers

1. **Newman, M. H. A.** (1942). "On Theories with a Combinatorial Definition of 'Equivalence'." *Annals of Mathematics*.

2. **Baader, F., & Nipkow, T.** (1998). *Term Rewriting and All That*. Cambridge University Press.

3. **Terese** (2003). *Term Rewriting Systems*. Cambridge Tracts in Theoretical Computer Science.

### Confluence and Termination

4. **Huet, G.** (1980). "Confluent Reductions: Abstract Properties and Applications to Term Rewriting Systems." *Journal of the ACM*.

5. **Dershowitz, N.** (1987). "Termination of Rewriting." *Journal of Symbolic Computation*.

### Related Approaches

6. **Shapiro, M., et al.** (2011). "Conflict-Free Replicated Data Types." *SSS 2011*.

7. **Bailis, P., et al.** (2014). "Coordination Avoidance in Database Systems." *VLDB 2014*.

8. **Ellis, C. A., & Gibbs, S. J.** (1989). "Concurrency Control in Groupware Systems." *SIGMOD 1989*.

### This Work

9. **Blackwell, D.** (2026). "Normalization Confluence in Federated Registry Networks." Zenodo. [DOI: 10.5281/zenodo.18677400](https://doi.org/10.5281/zenodo.18677400)
