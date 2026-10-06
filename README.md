# gsm - Governed State Machines

[![Blackwell Systems™](https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg)](https://github.com/blackwell-systems)
[![Go Reference](https://pkg.go.dev/badge/github.com/blackwell-systems/gsm.svg)](https://pkg.go.dev/github.com/blackwell-systems/gsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/blackwell-systems/gsm)](https://goreportcard.com/report/github.com/blackwell-systems/gsm)
[![CI](https://github.com/blackwell-systems/gsm/actions/workflows/test.yml/badge.svg)](https://github.com/blackwell-systems/gsm/actions/workflows/test.yml)
[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.18677400.svg)](https://doi.org/10.5281/zenodo.18677400)
[![proof: machine-checked](https://github.com/blackwell-systems/normalization-confluence/actions/workflows/verify.yml/badge.svg)](https://github.com/blackwell-systems/normalization-confluence/tree/main/coq)

**Send in any order. Converge on the rules.**

gsm is the checker for [normalization confluence](https://github.com/blackwell-systems/normalization-confluence): an exact regime map of governed concurrent state. In every regime there is a machine-checked exact condition, a hardness result showing no efficient one exists, or a gap stated in the open ([regime audit](https://github.com/blackwell-systems/normalization-confluence/blob/main/REGIME-AUDIT.md)), and gsm checks the practical ones.

`gsm` is a Go library for **convergence by compensation**: events may arrive out of order and break business rules, and every replica still converges to the same valid state, because repair is well-founded and commutes with events. Convergence is **verified at build time**: `Build` enumerates the state space and checks that every ordering converges, then returns a machine that applies an event with one table lookup, or refuses with a counterexample. A machine too large to enumerate whole is checked per footprint component instead, exactly, when its rules are combinators (`BuildCompositional` also takes closures). The check is **checked again by the proof**: a checker extracted from the axiom-free Coq/Rocq development re-certifies the machine's tables in-process, and the build fails closed if it does not, so a bug in gsm's Go verification cannot hand you a machine that does not converge.

## Example: Order Fulfillment

Payment and shipment requests arrive at different replicas in different orders. Every replica must end in the same state, and no order may ship unpaid:

<!-- gocheck: run -->
```go
r := gsm.NewRegistry("order_fulfillment")

// Facts: each event records one fact and reads nothing else.
paid := r.Bool("paid")
shipRequested := r.Bool("ship_requested")
cancelled := r.Bool("cancelled")

// Outcome: derived from the facts by compensation, never written by an event.
status := r.Enum("status", "open", "shipped", "cancelled")

// The business rule: what the status must be, given the facts.
outcome := func(s gsm.State) string {
    switch {
    case s.GetBool(cancelled):
        return "cancelled" // cancellation wins, in every order
    case s.GetBool(paid) && s.GetBool(shipRequested):
        return "shipped" // ship only once paid
    default:
        return "open"
    }
}

r.Invariant("status_matches_facts").
    Watches(paid, shipRequested, cancelled, status).
    Holds(func(s gsm.State) bool { return s.Get(status) == outcome(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(status, outcome(s)) }).
    Add()

r.Event("process_payment").Writes(paid).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(paid, true) }).Add()
r.Event("request_shipment").Writes(shipRequested).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(shipRequested, true) }).Add()
r.Event("cancel_order").Writes(cancelled).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(cancelled, true) }).Add()

machine, report, err := r.Build() // verifies that every ordering converges
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}

// Replica A sees the shipment request first; the order stays open until payment.
a := machine.NewState()
a = machine.Apply(a, "request_shipment") // status=open
a = machine.Apply(a, "process_payment")  // status=shipped: compensation derives it

// Replica B sees the same events in the other order.
b := machine.NewState()
b = machine.Apply(b, "process_payment")
b = machine.Apply(b, "request_shipment")

fmt.Println(a.Get(status), b.Get(status), a.ID() == b.ID()) // shipped shipped true
```

The shape to copy: **events record facts; invariants derive outcomes.** Each event writes one variable and reads nothing else, so nothing an event does depends on what arrived before it. The order's status is never set by an event: the invariant recomputes it from the facts, and compensation applies it after every event. (The natural first draft, a `ship` event guarded on payment, does not converge; [Getting started](docs/getting-started.md#why-not-guard-the-shipment-on-payment) shows why.)

`report` says what `Build` verified and what certified it:

```
Machine: order_fulfillment
  Variables: 4
  States: 24
  Events: 3

  WFC: PASS (max repair depth: 1)
  CC (Compensation Commutativity): PASS (3 pairs: 0 disjoint, 3 brute-force)

  Convergence: GUARANTEED
  Checked globally: the whole machine has 24 states, within Build's enumeration limit of 1,048,576, and the global check keeps step tables and both whole-machine oracles
  Assurance: tables certified by the verified table oracle
  Rules oracle: not run: not a combinator machine (...)
```

WFC: repair terminates from every state. CC: every pair of events reaches the same normal form in either order, from every valid state. Assurance: the extracted table oracle certified the tables too. Every line is explained in [Verification](docs/verification.md#verification-report).

## What Build guarantees

| Setting | What converges | What `Build` checks | Details |
|---|---|---|---|
| Single registry | Every ordering of the events, from a valid state or `NewState`, reaches one normal form | WFC and CC over the whole state space, re-certified by the extracted oracles | [Verification](docs/verification.md) |
| Registry too large to enumerate, combinator rules | The single-registry guarantee, for the whole machine | Each footprint component over its own subspace (footprints include reads; pairs in different components need no check), exact for the whole machine, each component's tables re-certified by the extracted oracle | [Verification](docs/verification.md#compositional-verification) |
| Keyed collection | The single-registry guarantee at every key, for any number of keys; events on different keys commute | The template (one item) with `Build`: by symmetry one item is the whole check (cutoff 1) | [Getting started](docs/getting-started.md#collections-one-template-every-key) |
| Integer variables, compared and copied (`Abstract`) | The single-registry guarantee for every value in the declared ranges, however wide | The representative states only (C, the n values above each constant and below the least): rules must only compare and copy values and declared constants, which `Build` checks from the combinator trees | [Verification](docs/verification.md#abstraction-check-relationships-not-values) |
| Acyclic federation | The whole network: a unique federated normal form, and every interleaving of independent events | Validity preservation per morphism (M1, or R1/R2 for a resolver), plus the event-order checks C1 and C2 | [Federation](docs/federation.md#when-it-just-works-no-loops) |
| Monotone cycles | The least fixed point, reached in any order | Opt-in `AllowMonotoneCycles`: monotonicity of every morphism and resolver, plus C1 and C2 | [Federation](docs/federation.md#escape-hatch-2-monotone-cycles) |
| Coordinated cycles | The rest of the network, coordination-free; the normal form is unique given the plan's authority root | `CoordinationPlan` names the edges to coordinate; `BuildCoordinated` verifies the acyclic residual (not the coordination itself) | [Federation](docs/federation.md#escape-hatch-3-coordinate-the-obstruction) |
| Projection deployments | Nodes that each run one registry and merge projections, once propagation completes | XU, reported in `FedReport.ProjectionSafe` and required with `RequireProjectionSafe`; acyclic, single-source targets only | [Deployment](docs/deployment.md#projection-deployments) |

Every row assumes each event is delivered once and, if you declared only some pairs `Independent`, that each undeclared pair that does not commute arrives in causal order. `Build` names the events and pairs this applies to ([Deployment](docs/deployment.md#delivery)).

## When to Use gsm

**Use gsm when:**
- Events arrive out of order and can violate invariants, so you need compensation (repair), not just commutativity
- Each variable's domain is finite (the *global* state space may be astronomically large: the per-component check of `Build` and `BuildCompositional` scales with the largest footprint component, not the product of all domains)
- You want convergence proved, not tested, before the machine runs
- Several registries share constraints across organizational boundaries (federated registry networks)
- You want gsm to **synthesize** the compensation from your invariants and events (or prove none converges)

**Don't use gsm when:**
- Operations already commute: a CRDT is simpler (and is provably the compensation-free special case of what gsm does; gsm can certify whether your machine falls in that fragment)
- Operations preserve invariants in all orderings (use invariant confluence)
- A variable needs a truly unbounded domain (arbitrary strings, lists), or the machine is one large tightly-coupled footprint that neither `Build` nor `BuildCompositional` can enumerate
- Real-time latency requirements conflict with build-time verification cost

## Installation

```bash
go get github.com/blackwell-systems/gsm
```

Verification is Go-only; a built machine exports to JSON and runs in any language ([Reference](docs/reference.md#multi-language-runtime)).

## Documentation

<a id="core-concepts"></a><a id="api-overview"></a><a id="federated-registries"></a><a id="how-it-works"></a><a id="verification-report"></a><a id="what-the-extracted-oracles-check"></a><a id="compositional-verification"></a><a id="compensation-synthesis"></a><a id="performance"></a><a id="limitations"></a><a id="multi-language-support"></a><a id="relationship-to-the-paper"></a><a id="testing"></a>

| Page | Read it for |
|---|---|
| [Getting started](docs/getting-started.md) | The tutorial: using machines, reading and writing state, combinator rules, independence declarations, keyed collections |
| [Concepts](docs/concepts.md) | Why it works: invariants, compensation, events, WFC and CC, where CRDTs fit, a glossary |
| [Federation](docs/federation.md) | Many registries: morphisms, resolvers, C1 and C2, cycles, coordination, certificates |
| [Deployment](docs/deployment.md) | What your runtime must provide: delivery, causal order, shared logs, projection deployments, reset epochs |
| [Verification](docs/verification.md) | The report, the extracted oracles, assurance levels, compositional verification, abstraction, synthesis, performance, limitations |
| [Reference](docs/reference.md) | `Report` and `FedReport` fields, errors, build options, the export format, the test suite |
| [Theory](docs/theory.md) | Formal definitions and proofs, the paper's sections mapped to the code |
| [Roadmap](docs/ROADMAP.md) | What is planned next: verifying realistic domains, checking a change before deploying it, a regime report |
| [Design notes](docs/design/) | [Architecture](docs/design/ARCHITECTURE.md), [certificates](docs/design/CERTIFICATE-DESIGN.md), [holonomy-minimal coordination](docs/design/HOLONOMY-COORDINATION-DESIGN.md) |
| [CHANGELOG](CHANGELOG.md) | What changed in each release |

API documentation: [pkg.go.dev/github.com/blackwell-systems/gsm](https://pkg.go.dev/github.com/blackwell-systems/gsm).

## Citation

```bibtex
@techreport{blackwell2026nc,
  author = {Blackwell, Dayna},
  title = {Normalization Confluence in Federated Registry Networks},
  year = {2026},
  publisher = {Zenodo},
  doi = {10.5281/zenodo.18677400},
  url = {https://doi.org/10.5281/zenodo.18677400}
}
```

## License

Apache License 2.0 - see [LICENSE](LICENSE) and [NOTICE](NOTICE). The accompanying papers are licensed separately under CC-BY-4.0.

## Related Tools

- **[normalization-confluence](https://github.com/blackwell-systems/normalization-confluence)** - The theory, the mechanized proof, and the extracted checkers gsm runs
- **[nccheck](https://github.com/blackwell-systems/nccheck)** - YAML-based verifier for registry specs (reference implementation from the paper)
- **gsm** (this library) - Go library for building verified convergent state machines
