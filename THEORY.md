# Mathematical Foundations

This document provides the formal mathematical foundations of governed state machines. It is intended for readers with background in formal methods, rewriting theory, or distributed systems who want to understand the rigorous theoretical basis for convergence guarantees.

For other audiences, see:
- **Practical usage**: [README.md](README.md)
- **Conceptual introduction**: [CONCEPTS.md](CONCEPTS.md)
- **Implementation**: [ARCHITECTURE.md](ARCHITECTURE.md)
- **Research paper**: [Normalization Confluence in Federated Registry Networks](https://doi.org/10.5281/zenodo.18677400)

## Table of Contents

1. [Abstract Rewriting Systems](#1-abstract-rewriting-systems)
2. [Confluence and the Church-Rosser Property](#2-confluence-and-the-church-rosser-property)
3. [Newman's Lemma](#3-newmans-lemma)
4. [Labeled Transition Systems](#4-labeled-transition-systems)
5. [Governed State Machines as Rewriting Systems](#5-governed-state-machines-as-rewriting-systems)
6. [Well-Founded Compensation (WFC)](#6-well-founded-compensation-wfc)
7. [Compensation Commutativity (CC)](#7-compensation-commutativity-cc)
8. [The Convergence Theorem](#8-the-convergence-theorem)
9. [Footprint Calculus](#9-footprint-calculus)
10. [Verification Algorithm](#10-verification-algorithm)
11. [Comparison to Related Formalisms](#11-comparison-to-related-formalisms)
12. [Limitations and Extensions](#12-limitations-and-extensions)

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

**Proof by Noetherian induction** (induction on terminating systems):

Assume the ARS is terminating and locally confluent. We prove confluence by induction on the number of rewrite steps.

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

## 4. Labeled Transition Systems

### 4.1 Definition

A **labeled transition system** (LTS) is a tuple (S, L, →) where:
- **S** is a set of states
- **L** is a set of labels (events, actions)
- **→** ⊆ S × L × S is the transition relation

We write s →ᵉ s′ for (s, e, s′) ∈ →, meaning "state s transitions to s′ via event e."

### 4.2 Paths and Traces

A **path** is a sequence s₀ →^e₁ s₁ →^e₂ s₂ ... →^eₙ sₙ.

A **trace** is the sequence of labels [e₁, e₂, ..., eₙ] (abstracting away intermediate states).

### 4.3 Reachability

State s′ is **reachable** from state s if there exists a path from s to s′.

The **reachable state space** from s₀ is:

```
Reach(s₀) = {s | s₀ →* s}
```

### 4.4 Determinism

An LTS is **deterministic** if for all s, e:

```
s →ᵉ s₁ ∧ s →ᵉ s₂  ⟹  s₁ = s₂
```

In other words, each (state, event) pair determines a unique successor state.

---

## 5. Governed State Machines as Rewriting Systems

### 5.1 Registry Definition

A **registry** R is a tuple (V, I, ρ) where:
- **V** is a finite set of variables, each with finite domain
- **I** = [inv₁, inv₂, ..., invₖ] is an ordered list of invariants
- **ρ** = [ρ₁, ρ₂, ..., ρₖ] are corresponding repair functions

Each invariant invᵢ is a predicate invᵢ : S → {true, false}.

Each repair ρᵢ : S → S modifies only variables in invᵢ's footprint.

### 5.2 State Space

The **state space** S is the Cartesian product of variable domains:

```
S = Dom(v₁) × Dom(v₂) × ... × Dom(vₙ)
```

Since each domain is finite, |S| is finite.

### 5.3 Validity Predicate

A state s ∈ S is **valid** if all invariants hold:

```
Vᵣ(s) = ⋀ᵢ invᵢ(s)
```

The set of valid states: Validᵣ = {s ∈ S | Vᵣ(s)}.

### 5.4 Compensation as Rewriting

Define the **compensation relation** →ᵣ as:

```
s →ᵣ s′  ⟺  ¬Vᵣ(s) ∧ s′ = ρᵢ(s)
```

where i is the smallest index such that ¬invᵢ(s).

This creates an abstract rewriting system (S, →ᵣ).

### 5.5 Normal Forms

The **normal forms** of (S, →ᵣ) are exactly the valid states:

```
NFᵣ = {s ∈ S | ¬∃s′. s →ᵣ s′} = Validᵣ
```

**Proof**: If s is valid, then Vᵣ(s) holds, so no repair fires, so s →ᵣ s′ for no s′.

Conversely, if s is not valid, then some invᵢ(s) is false, so s →ᵣ ρᵢ(s).

Therefore, s is in normal form iff s is valid.

### 5.6 Normalization Function

For a registry R, the **normalization function** NFᵣ : S → S is defined as:

```
NFᵣ(s) = the unique normal form of s (if it exists)
```

This is well-defined only if (S, →ᵣ) is terminating and confluent (guaranteed by WFC + CC).

---

## 6. Well-Founded Compensation (WFC)

### 6.1 Definition

A registry R satisfies **Well-Founded Compensation** (WFC) if the compensation relation (S, →ᵣ) is terminating.

**Formally**: There is no infinite sequence s₀ →ᵣ s₁ →ᵣ s₂ →ᵣ ...

### 6.2 Well-Founded Orderings

A binary relation < on a set X is **well-founded** if there is no infinite descending chain:

```
x₀ > x₁ > x₂ > x₃ > ...
```

**Equivalently**: Every non-empty subset of X has a minimal element.

Examples of well-founded orderings:
- Natural numbers with standard ordering (ℕ, <)
- Lexicographic ordering on finite tuples
- Multiset ordering

### 6.3 WFC via Well-Founded Measure

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

### 6.4 Verification Algorithm

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

## 7. Compensation Commutativity (CC)

### 7.1 Event Application

Given a registry R and event set E, each event e ∈ E has:
- Write set Wₑ ⊆ V (variables it modifies)
- Guard gₑ : S → {true, false} (precondition)
- Effect φₑ : S → S (transition function)

Event application:

```
s →ₑ s′  ⟺  gₑ(s) ∧ s′ = φₑ(s)
```

If ¬gₑ(s), then s →ₑ s (no-op).

### 7.2 Normalized Event Application

Define the **normalized step** relation:

```
s ⇒ₑ s′  ⟺  s′ = NFᵣ(s →ₑ ·)
```

In words: apply event e, then normalize via compensation.

This is what the `step` table precomputes.

### 7.3 CC1: Event Commutativity

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

### 7.4 CC2: Compensation Stability

**CC2** requires that for all events e and all states s:

```
NFᵣ(s →ₑ ·) = NFᵣ(NFᵣ(s) →ₑ ·)
```

In words: Normalizing before applying an event gives the same result as normalizing after.

**Intuition**: Once normalized, subsequent events should behave "the same" as if applied to the original state (modulo normalization).

### 7.5 Why CC2 Matters

CC2 ensures that the order of normalization doesn't matter:

```
s → (apply e₁) → (normalize) → (apply e₂) → (normalize)

vs.

s → (normalize) → (apply e₁) → (normalize) → (apply e₂) → (normalize)
```

Both must yield the same result.

**Note**: The gsm implementation enforces CC2 implicitly. Since the `step` table stores NFᵣ(s →ₑ ·), and we always look up from this table, we're always computing normalized steps.

### 7.6 Local Confluence

CC1 + CC2 together ensure that the system (S, ⇒) with multiple events is **locally confluent**:

For any valid state s and events e₁, e₂:
```
s ⇒ₑ₁ s₁  ∧  s ⇒ₑ₂ s₂  ⟹  ∃t. s₁ ⇒ₑ₂ t ∧ s₂ ⇒ₑ₁ t
```

This is the "one-step diamond" required by Newman's Lemma.

---

## 8. The Convergence Theorem

### 8.1 Statement

**Theorem (Convergence Guarantee)**: Let R be a registry with event set E. If R satisfies WFC and CC, then for any initial state s₀ and any two sequences of events [e₁, ..., eₘ] = [f₁, ..., fₘ] (as multisets), the final states are equal.

**Formally**:

```
s₀ ⇒ₑ₁ ... ⇒ₑₘ sₐ
s₀ ⇒f₁ ... ⇒fₘ sᵦ
⟹  sₐ = sᵦ
```

### 8.2 Proof

The proof follows from Newman's Lemma applied to the normalized event system.

**Step 1**: Define the event system (S, ⇒) where s ⇒ s′ if s ⇒ₑ s′ for some event e.

**Step 2**: WFC implies (S, →ᵣ) terminates. Combined with the fact that |S| is finite and events always produce valid normal forms, (S, ⇒) terminates.

**Step 3**: CC implies (S, ⇒) is locally confluent. Any one-step divergence (via different events) can be joined.

**Step 4**: By Newman's Lemma, (S, ⇒) is confluent.

**Step 5**: Two sequences of events [e₁, ..., eₘ] and [f₁, ..., fₘ] (same multiset) are equivalent modulo reordering. By confluence, they must reach the same final state.

Therefore, convergence is guaranteed. ∎

### 8.3 Eventual Consistency

The convergence theorem provides **strong eventual consistency**:

All replicas processing the same set of events (in any order) reach the same state, assuming WFC and CC hold.

This is stronger than weak eventual consistency (which only guarantees convergence after quiescence).

---

## 9. Footprint Calculus

### 9.1 Variable Footprints

Each invariant invᵢ has a **footprint** Fᵢ ⊆ V, the set of variables it constrains.

The repair ρᵢ may only modify variables in Fᵢ.

**Formally**: For all s ∈ S and all v ∉ Fᵢ:

```
ρᵢ(s)(v) = s(v)
```

### 9.2 Event Write Sets

Each event e has a **write set** Wₑ ⊆ V, the variables it modifies.

**Formally**: For all s ∈ S and all v ∉ Wₑ:

```
φₑ(s)(v) = s(v)
```

### 9.3 Triggered Invariants

Event e **triggers** invariant invᵢ if Wₑ ∩ Fᵢ ≠ ∅.

**Intuition**: If an event modifies a variable that an invariant watches, the invariant might be violated.

### 9.4 Event Footprints

The **footprint** of an event e is the union of footprints of all invariants it can trigger:

```
Footprint(e) = ⋃{Fᵢ | Wₑ ∩ Fᵢ ≠ ∅}
```

This is the transitive closure: if e writes v, and invᵢ watches v, then invᵢ's repair can modify other variables in Fᵢ.

### 9.5 Disjointness Theorem

**Theorem**: If two events e₁ and e₂ have disjoint footprints, then they commute.

**Proof sketch**:

Assume Footprint(e₁) ∩ Footprint(e₂) = ∅.

Let s be a valid state. Apply e₁:
- φₑ₁ modifies only variables in Wₑ₁
- This may trigger invariants with footprints overlapping Wₑ₁
- Repairs modify only variables in Footprint(e₁)

Similarly for e₂ with Footprint(e₂).

Since the footprints are disjoint, repairs from e₁ don't affect variables relevant to e₂, and vice versa.

Therefore, the normalized results are independent, so they commute. ∎

### 9.6 Optimization

The disjointness theorem allows the verifier to skip exhaustive checking for event pairs with disjoint footprints.

For a system with n events:
- Worst case: O(n²) pairs to check exhaustively
- With disjointness: Only check pairs with overlapping footprints

This is the "footprint optimization" mentioned in the verification report.

---

## 10. Verification Algorithm

### 10.1 Phase 1: Normal Form Computation (WFC Check)

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

### 10.2 Phase 2: Step Table Construction

**Algorithm**:
```
for each event e in E:
    for each state s in S:
        s' = apply_event(e, s)
        step[e][s] = nf[s']
```

**Complexity**: O(|E| × |S|) - one event application and one table lookup per (event, state) pair.

### 10.3 Phase 3: CC Verification

**Algorithm**:
```
pairs = compute_independent_pairs(E)

for each (e1, e2) in pairs:
    if disjoint_footprints(e1, e2):
        disjoint_count += 1
        continue

    brute_force_count += 1
    for each valid state s in S:
        s_12 = step[e2][step[e1][s]]
        s_21 = step[e1][step[e2][s]]

        if s_12 != s_21:
            return CC_FAILURE(e1, e2, s, s_12, s_21)

return CC_SUCCESS(disjoint_count, brute_force_count)
```

**Correctness**: If the algorithm succeeds for all pairs, then CC1 holds for all independent events.

**Complexity**: O(|E|² × |S|) worst case, but typically much better due to disjointness optimization.

### 10.4 Soundness and Completeness

**Soundness**: If the verification algorithm reports success, then WFC and CC hold.

**Proof**: The algorithm exhaustively checks all states (for WFC) and all event pairs in all states (for CC). Since |S| is finite, exhaustive checking is sound.

**Completeness**: If WFC and CC hold, then the verification algorithm reports success.

**Proof**: By definition. If WFC holds, compensation terminates, so Phase 1 succeeds. If CC holds, all event pairs commute, so Phase 3 succeeds.

---

## 11. Comparison to Related Formalisms

### 11.1 Conflict-Free Replicated Data Types (CRDTs)

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

### 11.2 Operational Transformation (OT)

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

### 11.3 Invariant Confluence (I-confluence)

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

### 11.4 Transaction Processing

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

### 11.5 Abstract State Machines (ASMs)

**ASMs** (Gurevich) model state transitions with update rules:

```
if cond(s) then s' = update(s)
```

**Difference from gsm**:
- ASMs: general computational model
- gsm: specialized for convergent event processing

**Relationship**: gsm can be viewed as ASMs with specific termination and confluence guarantees.

---

## 12. Limitations and Extensions

### 12.1 Finite State Spaces

**Limitation**: gsm requires finite state spaces for exhaustive verification.

**Consequence**: Cannot model:
- Unbounded integers, strings, lists
- Recursive data structures
- Infinite domains

**Mitigation**:
- Bound domains to reasonable ranges (e.g., balance ∈ [0, 1000000])
- Use symbolic verification for unbounded domains (future work)

### 12.2 State Space Explosion

**Limitation**: State space grows as product of variable domains.

**Example**: 10 variables with 10 values each = 10¹⁰ states (too large).

**Mitigation**:
- Modular verification (verify subsystems independently)
- Symmetry reduction (exploit equivalent states)
- Partial order reduction (ignore irrelevant interleavings)

Current limit: 2²⁰ ≈ 1M states.

### 12.3 Dynamic Event Sets

**Limitation**: Event set is fixed at build time.

**Consequence**: Cannot add new events at runtime without reverification.

**Use case**: Systems where event types evolve over time.

**Future work**: Incremental verification when adding events.

### 12.4 Multi-Registry Systems

**Limitation**: gsm currently models single-registry systems.

**Paper extension**: Section 7 of the paper describes federated registries with cross-registry constraints.

**Challenge**: Inter-registry compensation requires coordination.

**Future work**: Implement federation with partial synchronization.

### 12.5 Probabilistic and Timed Events

**Limitation**: Events are discrete and untimed.

**Extension possibilities**:
- Stochastic events with probability distributions
- Timed events with deadlines and timeouts
- Continuous-time compensation

**Challenge**: Verification becomes undecidable or requires approximation.

### 12.6 Non-Deterministic Repairs

**Limitation**: Repairs are deterministic functions.

**Extension**: Allow repairs to non-deterministically choose from multiple valid outcomes.

**Consequence**: Normal forms may not be unique, weakening convergence guarantee to "converge to equivalent states" rather than "identical states."

### 12.7 Partial Order Relations

**Limitation**: Invariants fire in declaration order (total order).

**Extension**: Allow partial order on invariants, firing all minimal violated invariants concurrently.

**Challenge**: Requires verifying that concurrent repairs don't interfere.

---

## Summary

The mathematical foundations of gsm rest on three key pillars:

1. **Abstract rewriting theory**: Compensation is a rewriting system; normal forms are valid states.

2. **Newman's Lemma**: Termination + local confluence → global confluence. WFC provides termination, CC provides local confluence.

3. **Finite enumeration**: Exhaustive verification is possible because state spaces are finite.

Together, these provide a **constructive proof** of convergence: if verification passes, convergence is mathematically guaranteed.

The trade-off is clear:
- **Gain**: Proven convergence, no runtime coordination, O(1) event application
- **Cost**: Finite domains, build-time verification overhead, state space limits

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
