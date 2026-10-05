# Getting Started

This is the tutorial: how to write rules that converge, use a built machine, read and write
state, write rules as data with the combinator vocabulary, and declare which events are
independent. It assumes you have read the [order fulfillment example](../README.md#example-order-fulfillment)
in the README and installed gsm ([Installation](../README.md#installation)).

For why any of this converges, see [Concepts](concepts.md). For what `Build` checks and what its
report means, see [Verification](verification.md).

## Contents

- [Events record facts; invariants derive outcomes](#events-record-facts-invariants-derive-outcomes)
- [Why not guard the shipment on payment?](#why-not-guard-the-shipment-on-payment)
- [Using machines](#using-machines)
- [Reading state](#reading-state)
- [Writing state](#writing-state)
- [Rules must return a state of their own machine](#rules-must-return-a-state-of-their-own-machine)
- [Declarative rules (combinators)](#declarative-rules-combinators)
- [Independence declarations](#independence-declarations)
- [Next steps](#next-steps)

---

## Events record facts; invariants derive outcomes

A registry declares three things: **variables** with finite domains, **invariants** (what valid
means, each with a repair), and **events** (the operations, which may violate invariants). `Build`
verifies that every ordering of the events converges, and returns a `Machine`, or an error and a
report with a counterexample. [Concepts](concepts.md#core-definitions) explains each part.

The README's order machine shows the shape to copy. Each event writes one variable and reads
nothing else, so nothing an event does depends on what arrived before it. The outcome (the order's
status) is never set by an event: an invariant recomputes it from the facts, and compensation
applies it after every event.

## Why not guard the shipment on payment?

The natural first draft ships only when the order is already paid:

<!-- gocheck: check registry -->
```go
r.Event("ship").Writes(status).
    Guard(func(s gsm.State) bool { return s.GetBool(paid) }). // reads another event's write
    Apply(func(s gsm.State) gsm.State { return s.Set(status, "shipped") }).
    Add()
```

A replica that sees `ship` before `pay` drops the shipment (the guard is false), and one that sees `pay` first ships, so the two never agree. `Build` rejects that machine with the two orderings as a counterexample. A guard that reads a variable another event writes is the most common way to lose convergence; record the request as a fact instead and let an invariant decide. ([Concepts](concepts.md#cc-compensation-commutativity) traces both orders.)

## Using machines

<!-- gocheck: check machine -->
```go
// Create initial state (all variables at min/first value)
s := machine.NewState()

// Apply events (returns new state, original unchanged)
s = machine.Apply(s, "increment")
s = machine.Apply(s, "increment")

// Check validity
if machine.IsValid(s) {
    fmt.Println("State satisfies all invariants")
}

// Manually normalize (usually not needed - Apply does this)
s = machine.Normalize(s)

// Get event list
events := machine.Events() // ["increment", "enable", "disable"]
```

`Apply` is one table lookup: the compensation was precomputed by `Build`
([how](verification.md#how-build-verifies)).

## Reading state

<!-- gocheck: check machine -->
```go
// Enum variables
status := s.Get(statusVar)           // returns string

// Bool variables
enabled := s.GetBool(enabledVar)     // returns bool

// Int variables
count := s.GetInt(countVar)          // returns int (adjusted for min offset)
```

## Writing state

<!-- gocheck: check machine -->
```go
// Enum (panics if value not in declared set)
s = s.Set(statusVar, "active")

// Bool
s = s.SetBool(enabledVar, true)

// Int (silently clamped to declared range - SetInt(countVar, 999) on [0,100] becomes 100)
s = s.SetInt(countVar, 42)
```

`State.TrySet` is the non-panicking form of `Set`: it returns an error for a label the enum does
not declare. A clamped write is reported in `Report.Saturations` ([Reference](reference.md#report)).

## Rules must return a state of their own machine

An event effect, a repair, a morphism `Map` and a `Resolver` must return a state with the machine's variable schema and every variable within its declared range. Deriving the result from the input with `Set`/`SetBool`/`SetInt` always does. Membership is by value: a `State` from another machine with an identical variable declaration list is accepted. `Build`, `BuildCompositional`, `Synthesize` and `Federation.Build` reject a rule that returns anything else (a state of a different machine, an out-of-range value) on the states they run it on, with an error naming the rule, the input state and the result (`Report.DomainViolation`). A lazy `BuildCompositional` machine and a `FedMachine` run closures at `Apply` time, on states the build may not have enumerated, so they check each result there and panic on a bad one.

## Declarative rules (combinators)

Rules can also be written from a fixed, gsm-owned vocabulary instead of Go closures. It still reads as Go, but produces an expression tree gsm can both evaluate and analyze:

<!-- gocheck: check registry -->
```go
a := r.Int("a", 0, 5)
r.DeclInvariant("a_cap", Le(V(a), Lit(3)), Do(Set(a, Lit(3)))) // holds when a<=3; repair sets a=3
r.DeclEvent("inc_a", Do(Set(a, Add(V(a), Lit(1)))))            // a := a + 1
```

The footprint is **derived** from the tree: an invariant's footprint is every variable its predicate and repair mention, and an event's write set is the variables it assigns. Nothing is declared by hand, so nothing can be mis-declared, and `BuildCompositional` checks an event's reads (its guard and the expressions it assigns) against its write set exactly, from the tree. Because the rules are data (not opaque closures), they are inspectable and serializable, the precondition for a verified verifier and portable policies. Declarations copy the transforms they are given, and `And`/`Or` copy their predicate lists, so changing the caller's slice afterwards does not change a declared rule. The closure API (`Holds`/`Repair`/`Apply`) is unchanged; use whichever fits. Closure rules stay fully supported; they just cannot be serialized or independently re-certified from their rules.

### Rule expression layers

These combinators are the **analyzable core**: primitives we serialize (`Registry.WriteMachineAST`) and hand to the machine-checked oracle. On top of them sits an ergonomic layer that lowers to the exact same AST, so it adds nothing the verifier must learn:

<!-- gocheck: check registry -->
```go
r.Rule("a_cap").Require(AtMost(a, 3)).RepairWith(SetTo(a, 3)).Add()
r.On("inc_a").Does(Inc(a)).Add()
```

`AtMost`/`Inc`/`SetTo` and the `Rule`/`On` builders desugar to `Le(V(a),Lit(3))` / `Do(Set(a,Add(V(a),Lit(1))))` etc. The layering is deliberate: write for people at the top, but the *primitive* layer is what gets serialized, tested, and handed to the verified oracle. A load-bearing test (`sugar_test.go`, `TestSugar_LowersToPrimitives`) pins that the friendly and primitive spellings serialize to byte-identical rules, so no behavior can hide in the sugar that the analyzable core (and the proof-derived checker) would not see. Write for humans at the top; test, serialize, and prove at the primitive bottom.

What the rules oracle checks, and which rules it can serialize, is in
[Verification](verification.md#the-rules-oracle-and-its-fragment). Exporting a policy and its
digests is in [Reference](reference.md#policy-as-a-portable-artifact).

## Independence declarations

By default, gsm checks **all event pairs** for commutativity. For large systems, you can optimize by declaring which pairs are independent:

<!-- gocheck: check registry -->
```go
// Calling Independent() automatically switches to declared-only mode
r.Independent("deposit", "send_notification")
r.Independent("withdraw", "send_notification")
```

`OnlyDeclaredPairs()` makes the switch explicit (it is automatic on the first `Independent` call):

<!-- gocheck: check registry -->
```go
r.OnlyDeclaredPairs()
r.Independent("deposit", "notify")  // These two can happen in either order
r.Independent("withdraw", "notify")
// Other pairs: checked, and reported if they need causal delivery
```

**Independent events** can arrive in either order (they're not causally related). Declared pairs are certified: `Build` fails if one does not commute.

Use this when you know some events are causally ordered (e.g., `pay` always before `ship`). That knowledge is an obligation on the runtime: **every undeclared pair must be delivered in causal order**, the same fixed order at every replica. `Build` still checks the undeclared pairs and lists each one that does not commute in `Report.CausalOrderRequired`; [Deployment](deployment.md#causal-order-for-undeclared-pairs) shows the report and what to do about a listed pair.

`Build` checks every declared pair exactly, whatever the pair's footprints. Two events that write different variables can still fail to commute: a guard or effect may read a variable the other event writes (see [Why not guard the shipment on payment?](#why-not-guard-the-shipment-on-payment)). Only `BuildCompositional` skips pairs by footprint, and only after it has checked what each event reads (see [Compositional Verification](verification.md#compositional-verification)).

## Next steps

- [Concepts](concepts.md): why WFC and CC make every ordering converge.
- [Verification](verification.md): reading the report, the oracles, `BuildCompositional` for large machines, and synthesizing the compensation instead of writing it.
- [Federation](federation.md): connecting registries across organizational boundaries.
- [Deployment](deployment.md): what your transport must guarantee (duplicates, causal order, distributed nodes).
- [Reference](reference.md): report fields, errors, build options, the export format.
