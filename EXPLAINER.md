# The third way to make distributed state agree

Two replicas. The same events. They arrive in different orders. Do they end up in the same state?

If you have ever run a distributed system, you know the two textbook answers, and you know what each one costs you.

**Coordinate first.** Put a consensus protocol in front of every write. Raft, Paxos, a lock service. Now every replica sees the same order, so of course they agree. You paid for it in latency: a write is not done until a quorum says so, and a partition can stall you completely.

**Make the operations commute.** This is the CRDT answer. If `A then B` always equals `B then A`, order stops mattering and you can drop the coordination. Counters, sets, last-writer-wins registers all live here. It is a genuinely beautiful idea. The catch is the precondition: your operations have to commute. The moment a write can violate a business rule (ship an unpaid order, overdraw an account, approve a loan that is not funded) commutativity is not something you get to assume. Merging two "valid" replica states can produce a state that is not valid at all, and a CRDT has no notion of "not valid."

So the working engineer's mental model is: fast-but-limited (CRDTs) or expressive-but-slow (consensus). Pick your pain.

There is a third option, and it is not a compromise between those two. It is a different axis.

## Agree on the rules, not the order

The idea: instead of coordinating on *order*, agree ahead of time on *the rules* and on *how to repair a violation of them*. Then any order of events converges to the same valid state, with no coordination at write time.

Concretely you declare three things:

1. **Variables** with finite domains: a status enum, a paid flag, an inventory count.
2. **Invariants**: what "valid" means. "An order ships exactly when it is paid and a shipment was requested." Along with each invariant, a **repair**: if this is violated, here is how to fix it. ("Recompute the status from the facts.")
3. **Events**: the operations, which are allowed to violate invariants.

Here is the whole thing in [`gsm`](https://github.com/blackwell-systems/gsm), a Go library that implements this:

```go
r := gsm.NewRegistry("order_fulfillment")
paid := r.Bool("paid")
shipRequested := r.Bool("ship_requested")
status := r.Enum("status", "open", "shipped")

// Valid means: the status matches the facts. If violated, recompute it.
outcome := func(s gsm.State) string {
    if s.GetBool(paid) && s.GetBool(shipRequested) {
        return "shipped"
    }
    return "open"
}
r.Invariant("status_matches_facts").
    Watches(paid, shipRequested, status).
    Holds(func(s gsm.State) bool { return s.Get(status) == outcome(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(status, outcome(s)) }).
    Add()

r.Event("process_payment").Writes(paid).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(paid, true) }).Add()
r.Event("request_shipment").Writes(shipRequested).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(shipRequested, true) }).Add()

machine, _, err := r.Build() // verifies convergence, or refuses to build
```

Now the payoff. The events arrive backwards, the shipment request before the payment:

```go
s := machine.NewState()
s = machine.Apply(s, "request_shipment") // arrives first: still open, not paid
s = machine.Apply(s, "process_payment")  // arrives second: the repair derives "shipped"
// Every replica lands on the same valid state, whichever event it saw first.
```

Each event records a fact and never touches the status. The second one leaves the status stale, an invalid state (paid and requested, but still open). That is fine. The compensation repairs it, and because the repair obeys two algebraic properties, every ordering of these events lands on the same valid normal form. (The tempting first draft, "ship sets the status, and a repair un-ships an unpaid order", does not converge: ship-then-pay ends paid-but-not-shipped while pay-then-ship ends shipped. `Build` rejects it with exactly those two orders as the counterexample.) No consensus. No requirement that the operations commute. That underlying result has a name: **normalization confluence**.

## The part I care most about: you do not have to take my word for it

Anyone can write a library and claim it converges. The claim is worthless without a reason to believe it. Three things back this one up, and they are the reason I think it is worth your attention.

**It is verified at build time, not hoped for at runtime.** `Build` does not just wire up your callbacks. It enumerates the state space and checks that the two convergence properties actually hold for *your* rules. If they do not, it refuses to build and hands you a counterexample. Convergence is a compile-time property of your model, not a runtime prayer. (For models too large to enumerate whole, it verifies each independent component separately, so the cost scales with your biggest component, not the product of everything.)

**The convergence theorem is machine-checked, with no axioms.** The underlying math is proved in Coq/Rocq, and the proof is *axiom-free*: `Print Assumptions` reports "Closed under the global context," which is the proof assistant's way of saying nothing was assumed, everything was derived. CI gates on it. This is not "we wrote it on a whiteboard and it looked right." A machine checked every step, and rejects the build if a step goes missing.

**The library is checked against the proof, not just against itself.** This is the part that is genuinely unusual. A checker extracted directly from the Coq proof re-certifies the tables that the Go library produces, in process, before `Build` returns a machine, and a second extracted checker recomputes the result from the rules themselves when they are written in gsm's combinator vocabulary. So a bug in the Go convergence check cannot smuggle a non-convergent machine past you: the independent, proof-derived oracle would reject it. The trust does not rest on the Go code being correct. It rests on the math, and the Go code is held to the math.

That chain (build-time check, backed by an axiom-free proof, enforced by an extracted oracle) is what I mean by provenance of trust. Most "verified" claims stop at the first link. This one runs all the way down.

The assumptions that sit outside the proof are named, not buried. The guarantee is about orderings of the same events, each delivered once, so the build report lists every event that is not idempotent (deliver it twice and the result changes) and needs deduplication; the proof shows that for those events no amount of ordering cleverness absorbs a duplicate. If you declare only some event pairs independent, the report lists each undeclared pair that does not commute, because those must arrive in causal order.

## Even the hard case has a graceful answer

Some systems genuinely cannot converge on their own. The classic shape is a cycle: A mirrors B, and B is forced to disagree with A. As a loop that just oscillates, and `gsm` will tell you so rather than pretend:

```go
if _, _, err := fed.Build(); err != nil { /* rejected: non-monotone cycle */ }
d, _ := fed.DiagnoseCycle()   // d.Converges == false, and it tells you why
```

Instead of failing there, it computes a coordination plan: a set of connection points to put a coordination barrier on so the rest can run free. The set is always correct and never larger than the number of independent loops; the true minimum is NP-hard in general, so gsm does not promise it.

```go
plan := fed.CoordinationPlan()          // a small, correct set of points to coordinate
m, _, _ := fed.BuildCoordinated(plan)   // accept the network given that coordination
// now drive it and watch the rest converge with no further coordination
```

You start with something that cannot converge, get told an adjustment that works, make it, and watch it converge. That is the real shape of the guarantee: converge for free where you can, and get told precisely what it costs where you cannot.

## Where CRDTs fit

Here is the reframing that made this click for me. CRDTs are not a competitor to this. They are the **degenerate corner** of it: the special case where your operations already commute, so the required repair is empty. Everything a CRDT does, this framework does with a no-op compensation. Under causal delivery the fit is exact, and machine-checked: a governed system with no compensation converges under causal delivery precisely when it is an op-based CRDT ([`compensation_free_exact`](https://github.com/blackwell-systems/normalization-confluence/blob/main/coq/CausalReplay.v)). What you get by leaving that corner is the ability to handle operations that violate invariants (the entire world of real business rules) while keeping the coordination-free convergence that made CRDTs attractive in the first place.

`gsm` can even tell you *which* corner you are in: it will certify whether your machine falls in the commute-for-free fragment (use a CRDT, it is simpler) or genuinely needs compensation.

## If you want to go deeper

- The engine, with runnable examples: [github.com/blackwell-systems/gsm](https://github.com/blackwell-systems/gsm)
- The machine-checked proof (axiom-free, CI-gated): [the `coq` directory](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq)
- The papers: [*Normalization Confluence in Federated Registry Networks*](https://doi.org/10.5281/zenodo.18677400)

The one-line version: you do not have to choose between fast-and-limited and expressive-and-slow. If you can say what valid means and how to repair a violation, you can have coordination-free convergence over operations that break the rules, and a machine-checked proof that it holds.
