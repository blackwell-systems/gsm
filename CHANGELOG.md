# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Compensation synthesis** (`Registry.Synthesize`): instead of verifying a compensation you wrote, gsm can now *generate* one. Given the invariants' validity predicates and the events (Repair omitted), it searches for a normal-form map on invalid states that satisfies CC — returning a representative convergent compensation as a ready-to-use `Machine` plus an inspectable repair map (`Synthesis.Repairs`), reporting how many alternatives exist, or **proving that no compensation can make the registry converge** (the invariants + events must be redesigned). Convergent ≠ desirable: the synthesized repair only makes orderings agree, so inspect it and judge acceptability. Brute-force over `|valid|^|invalid|`, bounded by an internal cap (a SAT/SMT encoding would lift the ceiling).
- Invariants may now be declared **without a `Repair`** (validity predicate only) for use with `Synthesize`; `Build` still requires a Repair and now returns a clear error (rather than panicking) when one is missing.

## [0.4.2] - 2026-09-23

Documentation and CI hygiene; no code or API change.

### Changed
- **README de-staled**: fixed a contradiction (Limitations claimed cyclic networks are rejected while the body documents `AllowMonotoneCycles`), added the missing `Embed`/compositionality section, removed a fabricated `Checked in:` line from the verification-report example (`Report.String` never prints it), and corrected the "Relationship to the Paper" cross-references (WFC/CC are axioms in §3, convergence is §5, the verification calculus is §10; added multi-source, monotone-cycle, and compositionality rows).

### Fixed
- CI lint (`errcheck`): `TestFederation_ReportAndAccessors` discarded `Build`'s error into `_`; flattened it to check the error. Test-only.

## [0.4.1] - 2026-09-23

### Changed
- **Relicensed from MIT to Apache License 2.0.** Adds an explicit patent grant (relevant for a library implementing novel, published algorithms) and a `NOTICE` file citing the underlying papers. Copyright held by Dayna Blackwell, Blackwell Systems. Still fully permissive; no code or API change; the papers remain CC-BY-4.0.

## [0.4.0] - 2026-09-23

Federation beyond trees: monotone cyclic networks (`AllowMonotoneCycles`) and compositional construction (`Embed`). Additive over v0.3.0.

### Added
- **Compositionality** (`Federation.Embed`): compose a sub-federation into a larger one — its component registries, internal morphisms, and resolvers are brought in as a unit, so a subsystem can be defined and verified independently (its own `Build`) and reused. Realizes the paper's compositional-collapse result: the composed federation runs as the flat convergent machine (a `FedState` holds one `State` per component, so no product state space is materialized), and `Build` re-checks the local conditions on the combined network. Cycles introduced across an embed boundary follow the usual rules (rejected unless `AllowMonotoneCycles` and monotone).
- **Monotone cycles** (`Federation.AllowMonotoneCycles`): cyclic morphism networks are now supported when repair is monotone. By default the network must be acyclic; with this opt-in, `Build` instead requires every morphism/resolver to be **monotone** with respect to the componentwise order on variable values (verified per-node by finite enumeration), and `Normalize` computes the federated normal form by **Kleene iteration to the least fixed point** (reset shared components to ⊥, iterate repair to a fixed point) rather than a one-shot topological pass. This is the paper's *Monotone Convergence Despite Cycles* theorem (Knaster–Tarski + chaotic iteration): on ordered shared domains a monotone repair operator converges order-independently even on arbitrary cyclic graphs. Non-monotone cyclic networks (e.g. the negation counterexample) are rejected, as are cycles without the opt-in. State-based CRDTs are the compensation-free special case of this regime.

## [0.3.0] - 2026-09-23

Multi-source federation: the tree restriction is lifted to any acyclic network. A target with several sources declares a resolution operator that deterministically merges them — convergence guaranteed by the paper's *Federated Convergence with Resolution* theorem, whose preconditions gsm verifies exhaustively at build time. Additive over v0.2.0; single-source (tree) federations are unchanged.

### Added
- **Multi-source federation via resolvers** (`Resolver`, `Federation.Resolve`): a federation target may now have more than one source (an acyclic DAG, not just a tree). Such a target declares a `Resolver(dst, sources)` that deterministically merges its sources' states into its shared component (priority, AND/OR, most-restrictive, etc.) — a merge no single-authority morphism can express. Convergence is the paper's *Federated Convergence with Resolution* theorem (Section 8), which holds whenever the resolver is source-determined (R1) and validity-preserving (R2) — the multi-source generalization of the single-source M1 condition, with single-source authority as the special case. gsm certifies exactly those hypotheses: `Build` **exhaustively verifies** (over every reachable combination of valid source states) that the resolver writes only shared variables, satisfies R1, and satisfies R2 — the same verify-the-preconditions contract gsm applies to single-registry WFC/CC and tree-federation M1. A multi-source target without a resolver — or a resolver that violates R1/R2 (reads local state, can produce an invalid target, or writes non-shared variables) — is rejected. Single-source (tree) federations are unchanged.

## [0.2.0] - 2026-09-23

Federated registry networks: gsm now composes multiple registries connected by directed morphisms and proves the whole network converges (Section 8 of the paper), in addition to the single-registry model. Additive — no breaking changes to the single-registry API.

### Fixed
- **Export() file permissions**: Changed from 0644 (world-readable) to 0600 (owner-only)
- **State space overflow**: Added overflow guard before multiplication in Build() to prevent silent int overflow on large variable domains
- **Var ownership validation**: getRaw/setRaw now panic with a clear message if a Var from a different Machine is used on a State, preventing silent data corruption

### Added
- **Federated registries** (`Federation`, `MorphismBuilder`, `FedMachine`, `FedState`): compose multiple component registries connected by directed registry morphisms encoding cross-registry constraints, per §8 of *Normalization Confluence in Federated Registry Networks*. `FedMachine` applies the constructive two-phase normalizer ρ_Fed (Corollary 8.10) — normalize each component, then propagate shared components through morphisms in topological order — without ever materializing the product state space. Exposes `NewState`/`Of`/`Apply`/`Normalize`/`IsValid`. Directed morphisms give coordination-free conflict resolution via the authority argument (§8.3): a source registry deterministically fixes its targets' shared components.
- **Federated build-time verification**: `Federation.Build` refuses any network the theory proves cannot converge — each component must satisfy WFC + CC; the network must be a tree/forest (no cycles, Prop 8.13; at most one incoming morphism per registry — multi-source is rejected per Remark 8.15); component names must be distinct; and every morphism must satisfy M1 validity-preservation-under-overwrite (Prop 8.14, verified by finite enumeration over valid states) with a `Map` that writes only its declared `Shared()` variables. A `FedMachine` exists only if federated convergence is guaranteed — the federated analogue of gsm's single-registry contract.
- `FedMachine.ApplyNamed(state, registry, event)` and `FedMachine.Registries()`: name-keyed application and component enumeration, for event-sourced replay where a durable log holds `(registry, event)` strings rather than live `*Registry` handles. `ApplyNamed` returns an error (rather than panicking) on an unknown registry or event so a stale/corrupt log fails gracefully on reconstruction.
- **Worked federation example** (`ExampleFederation`): a runnable, godoc-rendered manufacturer→supplier catalog federation showing shared-vs-local state, the authority argument (a supplier delist is ignored while the manufacturer stays authoritative), and morphism propagation on publish. Verified by the test suite.
- **Partial synchronization** (`Projection`, `FedMachine.SharedProjection`, `FedMachine.Component`, `Machine.MergeProjection`): the distributed form of the constructive normal form (Corollary 8.10). Each node runs only its own component `Machine`, applies local events, and exchanges small shared-component messages (`ϕ_ij(σ_i)`) along tree edges — no node holds the full federated state. A target merges its parent's projection with `MergeProjection` (validity preserved by M1, no re-normalization needed). `Build` now also verifies **source-determinacy** — a morphism's shared image must depend only on the source, not the target's local state — so projections are well-defined; a `Map` that reads the target's local component is rejected. Proven equivalent to the centralized `FedMachine` by test.
- `State.TrySet()` — error-returning alternative to `Set()` for use with user input or external values
- Clearer panic message in `Set()` showing variable name and invalid value

### Tests
- **Confluence property tests**: an all-independent machine now proves the core convergence guarantee at full strength — every one of the 720 permutations of a 6-event multiset reaches an identical normal form (Theorem 5.4), plus 500 randomized-multiset shuffles and a 2000-step "normal form is always valid" (WFC) walk. Exercises both CC-verification paths (footprint-disjoint and brute-force).
- **Federation scale tests**: a 10-registry chain (a root flag propagates through all ten levels in one ρ_Fed) and a 6-registry branching tree (fan-out plus two-level depth) confirm federations handle many components and arbitrary tree shapes — no product state space is built.
- **Validation / guardrail tests**: panic paths (unknown event, invalid enum value, foreign `Var`, enum with <2 values, `Int` max<min, invariant/event builders missing their functions, `Independent` on an unknown event) and behavioral edges (`TrySet` valid/error, `SetInt` clamping, negative-min offset, guarded-out no-op, oversized-state-space build rejection, name accessors). Coverage 89.7% → 96.3%; suite is race-clean.

### Changed
- **Docs repositioned for federation**: gsm is no longer described as single-registry-only. The package doc comment (godoc landing text) now covers federation; README gains a "Federated Registries" section (morphisms, the authority argument, the extended build-time contract) plus intro/when-to-use/relationship-to-paper updates; THEORY.md §11.4 now records federation as implemented (Section 8), with multi-source as the remaining boundary. Corrected stale "Section 7 / not yet implemented" references (federation is Section 8 in the published paper).
- README: Added "Act like UDP, receive like TCP" tagline and CRDT positioning paragraph
- README: Trimmed quick example for scannability
- README: Documented `Set()` panic and `SetInt()` clamping behavior in Writing State section

## [0.1.5] - 2026-02-20

### Changed
- Fixed variable naming consistency in README code examples (b → r)
- Removed duplicate comment in Independence Declarations section

## [0.1.4] - 2026-02-20

### Changed
- Removed "The Problem" section from README
- Removed summary section from CONCEPTS.md

## [0.1.3] - 2026-02-20

### Changed
- **BREAKING**: Renamed `Builder` to `Registry` and `NewBuilder()` to `NewRegistry()`
  - The registry is the central authority that holds invariants, compensation rules, and events
  - This naming better reflects the conceptual model: the registry governs convergent state
  - Update your code: `gsm.NewBuilder(name)` → `gsm.NewRegistry(name)`
  - File renamed: `builder.go` → `registry.go`
- **API simplification**: `Independent()` now automatically switches to declared-only mode
  - No need to call `OnlyDeclaredPairs()` first
  - `OnlyDeclaredPairs()` still exists for explicitness but is no longer required
  - Old: `r.OnlyDeclaredPairs(); r.Independent("e1", "e2")`
  - New: `r.Independent("e1", "e2")` (auto-switches)
- Rewrote problem section in README for clarity

### Added
- CONCEPTS.md (541 lines) - foundational concepts, definitions, and glossary
- THEORY.md (836 lines) - mathematical foundations and proofs
- Strategic code comments for bitpacking operations and footprint calculus
- Cross-references between documentation files

## [0.1.2] - 2026-02-19

### Changed
- **BREAKING**: Renamed `InvariantBuilder.Over()` to `Watches()` for better clarity
- **BREAKING**: Renamed `InvariantBuilder.Check()` to `Holds()` to align with formal methods terminology
- **BREAKING**: Renamed `Builder.DeclaredIndependence()` to `OnlyDeclaredPairs()` for clearer semantics
- Expanded "CC" abbreviation to "Compensation Commutativity" in all user-facing messages and reports
- Updated all code examples and documentation to use new API

### Added
- New "Core Concepts" section in README explaining invariants, compensation, events, and independence
- Detailed explanations of WFC (Well-Founded Compensation) and CC requirements
- Examples showing footprint usage and event declaration patterns

## [0.1.1] - 2026-02-19

### Added
- CODEOWNERS file for automatic review assignments on pull requests

### Changed
- Improved documentation formatting

## [0.1.0] - 2026-02-18

### Added
- Initial release of governed state machines library
- Builder API for defining state machines with fluent interface
- State variable types: Bool, Enum, Int with finite domains
- Invariant declaration with footprint tracking and repair functions
- Event declaration with write sets, guards, and effect functions
- Build-time WFC verification via exhaustive state-space enumeration
- Build-time CC verification (CC1 and CC2) with counterexample generation
- Footprint-based optimization for disjoint event pairs
- Immutable Machine type with precomputed lookup tables
- O(1) runtime event application via Step table
- State normalization and validity checking
- Bitpacked state representation (uint64) for efficient table indexing
- Comprehensive verification reports with WFC depth and CC pair statistics
- Independence declarations for restricting CC checks to relevant pairs
- JSON export format for portable multi-language runtime support
- Full test suite covering WFC, CC, compensation, and failures
- Documentation with usage examples, API reference, and design rationale

[Unreleased]: https://github.com/blackwell-systems/gsm/compare/v0.1.5...HEAD
[0.1.5]: https://github.com/blackwell-systems/gsm/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/blackwell-systems/gsm/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/blackwell-systems/gsm/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/blackwell-systems/gsm/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/blackwell-systems/gsm/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/blackwell-systems/gsm/releases/tag/v0.1.0
