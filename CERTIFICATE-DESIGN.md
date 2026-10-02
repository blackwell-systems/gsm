# Effective-registry certificate (design note)

Status: **implemented (core + differential re-checker); one extension outstanding.** This note
specifies a serializable certificate for a verified (sub-)federation so that `Embed` can reuse it as
a black box: check only the boundary, skip re-verifying the subsystem's internals. It is additive.
Today `Build` re-checks the local conditions on the whole combined network, which is sound; the
certificate path is an opt-in optimization that does not change what gsm can verify, only what it
can safely skip.

Implemented (`certificate.go`): `Certificate`, `Federation.Certify`, `Federation.EmbedCertified`
(re-checks internal edges from the tables instead of their closures, and rebuilds each component
with `Build`, so component convergence is re-checked rather than trusted),
morphism/resolver table extraction, a digest that is tamper-complete over the declarations
(component rules, names and declared pairs, plus those tables; see "Versioning and trust policy"), and `Certificate.Verify`, a standalone differential re-checker that re-derives the federated
conditions from the tables rather than the producer's closures. Input ports are implemented
(`Certify(Port{...})`): a subsystem may declare free shared variables an outer morphism drives once
embedded, the inbound boundary morphism is verified at the seam (M1/R2), and the port declaration is
folded into the digest (the assume-guarantee / Theorem 2' case). Outstanding: the strongest trust
form (an axiom-free-Coq-extracted federation oracle, matching how `astchecker` re-checks
single-registry rules) waits on mechanizing the federation conditions in Coq.

## Problem

`Federation.Embed(sub)` brings a subsystem in as a unit, and `Build` then re-verifies the combined
network, including re-enumerating the subsystem's state space. For a subsystem reused in many places,
or a large network assembled from parts, that re-enumeration is the dominant cost and it is
redundant: the subsystem was already proven convergent. The compositional-collapse result (federated
paper, §8) says a verified sub-federation collapses to an effective registry and the flat network's
normal form equals the two-level computation, so in principle the internals never need to be looked
at again. This note makes that reuse concrete and states the exact condition under which it is sound.

Payoffs:
- **Verify once, reuse.** Cache a subsystem's certificate and drop it into larger networks without
  re-enumerating its internals.
- **Incremental re-verification.** Key the certificate by digest; on a change, re-verify only the
  touched subsystem plus the seam, not the whole network.
- **Independent parallel verification.** Footprint-disjoint components already verify independently;
  certificates make that reuse explicit across `Build` runs.
- **Third-party reuse.** A subsystem can be shipped as an interface plus a certificate that a
  consumer checks without seeing the internals.

## What a certificate is

An artifact produced by a successful `Build` of a (sub-)federation, assembled mostly from primitives
gsm already computes. Contents:

1. **Identity.** The policy digest of the subsystem: `PolicyDigest()` over the combined machine (the
   component registries' rules plus the edge/resolver declarations). `PolicyBytes()` is the
   canonical serialization the digest is taken over. Two subsystems with the same rules and wiring
   have the same digest; any rule or edge change changes it.
2. **Convergence verdict.** The `FedReport` the `Build` produced: per-component `WFC` and `CC`
   (with `PairsDisjoint` / `PairsBrute` / `FootprintChecked` provenance), `MaxRepairLen`, the
   component `StateCount` / `MaxComponentStates`, `Edges`, and the acyclic-or-monotone flag
   (whether `AllowMonotoneCycles` was in force). This is exactly the verdict `Build` already returns.
3. **Interface declaration.** The port split (see below): the shared variables the subsystem exposes,
   each marked an output port or an input port. Variables not declared as ports are internal and
   sealed.
4. **Parametric attestation.** For input ports, an attestation that the convergence verdict holds for
   every valid input-port valuation, not just one boundary (see "Parametric certificate").
5. **Resolver capability tags.** Each internal resolver tagged `Universal` (most-restrictive / AND)
   or `Section` (priority / OR), per "Resolver tags."
6. **Oracle evidence (optional, for third-party reuse).** Digests of the serialized machine AST
   (`WriteMachineAST`) and the convergence tables (`WriteConvergenceTables`), so a consumer can
   re-run the differential oracles (table oracle and rules oracle) and confirm the verdict
   independently rather than trusting the producer.

## Ports

The interface of a subsystem `E` is split into two kinds of shared variable:

- **Output port:** a shared variable that the subsystem controls (fixed by its own morphisms or
  roots) and that outer morphisms may read, i.e. `E` acts as a source to the outside on that
  variable.
- **Input port:** a shared variable that an outer morphism or resolver may write, i.e. `E` acts as a
  target from the outside on that variable. Inside the subsystem, an input port is a free parameter,
  not controlled by the subsystem's own morphisms.

A shared variable not declared as either port is **internal and sealed**: no outer morphism may read
or write it. Declaring ports is how a subsystem states its contract; sealing everything else is what
lets its certificate transfer unchanged.

## Parametric certificate

An output-port-only subsystem (no input ports) is certified as-is: nothing outside can perturb it, so
its verdict transfers directly.

An input port is written from outside, so the subsystem must be convergent for whatever valid value
arrives, not for one fixed boundary. Therefore a certificate that declares input ports must attest
convergence **over every valid input-port valuation**. gsm's `Build` already enumerates reachable
states exhaustively; declaring a variable an input port means treating it as unconstrained by the
subsystem and verifying convergence across its domain. The certificate records that the verdict is
parametric over the declared input ports. This is a rely-guarantee contract: the subsystem guarantees
convergence provided its inputs are valid, and the seam check (below) is what discharges the "inputs
are valid" assumption.

## Seam check (Embed from a certificate)

When `Build` embeds a subsystem via its certificate instead of re-verifying it, it checks only the
boundary:

- **(i) Ports only.** Every outer morphism touches the subsystem only at declared ports. An outer
  morphism that reads or writes an internal (sealed) variable is rejected.
- **(ii) Validity at input ports.** For every input port that gains an external source, re-verify the
  M1 / R2 condition at that port: the external write is validity-preserving for the receiving
  component, so the value delivered is a valid input-port valuation. This is the same per-edge check
  `verifyEdge` / `verifyResolved` already performs, applied only at the seam.
- **(iii) Acyclic or monotone across the boundary.** Run the graph-level acyclicity check (or, under
  `AllowMonotoneCycles`, the per-node monotonicity check) on the whole combined graph. A cycle can
  route through the subsystem's ports, so this check is global, but it is graph-level and per-node: it
  does not re-enumerate the subsystem's internal state space.
- **(iv) Digest match.** The embedded subsystem's `PolicyDigest()` equals the certificate's identity.

If all pass, `Build` uses the certificate's tables for the internal morphisms instead of re-verifying
their closures. (As built, it still rebuilds each component and re-checks its convergence; see
"Versioning and trust policy".) If any fail, it falls back to full re-verification (current behavior), so the
certificate path is never less sound than today, only faster when it applies.

## Resolver tags

The capability tag on each internal resolver records how it composes:

- **`Universal`** (most-restrictive / AND): composes freely. Its composition at a seam inherits the
  certificate with no extra obligation beyond the standard seam check.
- **`Section`** (priority / OR): converges (it satisfies R1/R2) but does not compose as a
  most-restrictive merge. Its use at a seam still requires the R2 re-check of step (ii), which the
  seam check already performs, so no additional machinery is needed; the tag is what tells a resolver
  library which primitives are safe to hand out as freely composable and which carry the per-seam
  obligation.

## Versioning and trust policy

There is a single development certificate version until 1.0 (the digest domain tag
`gsm-fedcert-v3`). It is not bumped per change, including verifier fixes. A certificate is
**validated by re-check, never trusted by digest**: the digest only binds the certificate to the
subsystem it describes. It covers each component's name, rules (`PolicyBytes`), and the names and
declared pairs the rules are addressed by (`PolicyNames`: variable names and kinds, enum labels,
event names, the Independent pairs), the morphism tables, the input ports and the cycle opt-in, so
it changes when any of those declarations changes. Every name it frames is quoted, so no name can
carry the framing of another, and the declared pairs are digested as a set. The certificate's own `Name`
is not covered: it labels messages only, and nothing is decided by it. A morphism or resolver closure is bound only
through its table, which records its images at one representative target: a closure that differs
only at other targets digests the same, and is caught at runtime by the FedMachine's image check.
The digest began binding names and pairs during this development version; certificates issued
before that no longer match and must be re-issued with `Certify` (the version tag is unchanged, per
this policy). Everything the certificate asserts is re-derived when it is used:

- `EmbedCertified` re-checks the internal morphisms from the tables (M1/R2, port freeness,
  acyclicity) and rebuilds every certified component with `Build`, which re-checks WFC and CC.
- `Certificate.Verify` does the same from the consumer's own copies of the component registries,
  after recomputing the digest from them. The copies are keyed by registry name, and each key must
  be its registry's name: the digest and the tables name components that way, so a mismatched key
  would re-check a table against a different registry than the digest covers.

The tables and ports address variables, and replay addresses events, by name. Every path that
builds, certifies, or exports therefore rejects a registry that declares two events, or two
variables, with the same name. `Build`, `BuildCompositional` and `Synthesize` also reject a
registry that a rule closure changes while they verify it, so a declaration cannot slip in after
their check. (`Synthesis.Machine` is the machine as synthesized: it is built from a snapshot taken
when `Synthesize` returns, so a later declaration does not reach it.) `Federation.Build` and
`Certify` verify the federation as it was when called: they work on a copy of the wiring, so a
morphism a closure adds while they run is neither verified nor in the machine or certificate, and
they reject a component changed while they run. `Certificate.Verify` runs no user code between
digesting and rebuilding (the digest requires combinator rules, and the tables replace the
morphism closures), so there is nothing there for a closure to change.

The recorded verdict (`Certificate.Report`) is informational. These re-checks are gsm's Go code:
they remove trust in stored results, not in the Go verifier. Component rebuilds go through `Build`, so the
in-process table oracle (generated from the proof) also re-certifies every component's tables; an
extracted federation oracle, for the morphisms, is planned separately. This is what makes a verifier fix
safe without a version bump: a certificate issued by an older, weaker verifier is re-checked by the
current one on load. For example, a v0.11.0 certificate for the naive pay/ship component (certified
then through Build's unchecked disjointness shortcut) still matches its digest, and both
`EmbedCertified` and `Verify` refuse it because the CC re-check finds the divergence
(`certificate_recheck_test.go`, with the v0.11.0 certificate as test data).

What re-checking costs: `EmbedCertified` builds each certified component's step tables anyway (the
runtime needs them), so the CC re-check adds two table lookups per state per event pair. What it
still saves is re-verifying the internal morphisms from their closures.

## Soundness condition

Reusing a certificate without re-verifying internals is sound exactly when the seam check passes and
the certificate is parametric over its declared input ports. This is the compositional-collapse
result of the federated paper (§8) together with its assume-guarantee refinement (the input/output
port contract). The check is entirely at the boundary: interface conditions plus a graph-level
cycle/monotonicity check, with no re-enumeration of the subsystem's state space.

## Proposed API shape (sketch)

Additive to the existing `Federation` / `FedMachine`:

<!-- gocheck: excerpt design sketch from before the API was built; see certificate.go for the real one -->
```go
// Certificate is the serializable verdict for a verified (sub-)federation.
type Certificate struct { /* identity, verdict, ports, parametric attestation, tags, oracle digests */ }

// Certify produces a certificate for a built subsystem, given its port declaration.
func (m *FedMachine) Certify(ports PortSpec) (*Certificate, error)

// EmbedCertified embeds a subsystem by its certificate: Build runs only the seam check.
func (f *Federation) EmbedCertified(cert *Certificate) *Federation
```

`PortSpec` declares each exposed shared variable as an input or output port; everything else is
sealed. `Build` on a federation that used `EmbedCertified` performs the seam check in place of the
subsystem's internal verification, and reports which subsystems were taken on certificate versus
re-verified.

## Non-goals

- Not a change to what gsm can verify: same WFC/CC/M1/R2/monotonicity contract, same oracles.
- Not a replacement for `Embed`: the default remains full re-verification, which stays correct.
- No new modeling vocabulary in the user-facing API beyond the port declaration and the certificate
  artifact.
