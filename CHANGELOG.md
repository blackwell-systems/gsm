# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.7.0] - 2026-09-24

### Added
- **Combinator rule vocabulary** (`DeclInvariant`, `DeclEvent`, `DeclEventGuarded`, and the `V`/`Lit`/`Add`/`Sub`/`Le`/`Lt`/`Eq`/`And`/`Or`/`Not`/`Set`/`Do` combinators): express invariants and events as data built from a fixed, gsm-owned vocabulary instead of arbitrary Go closures. Still reads as Go (`Le(V(a), Lit(3))`, `Set(a, Add(V(a), Lit(1)))`) but produces an expression tree gsm can both evaluate and analyze. Footprints are derived from the tree (the variables it reads and writes), so combinator rules are footprint-conformant by construction: no `Watches`/`Writes` needed, and nothing to check because the declaration is the footprint. This is the additive, closure-free surface that makes rules inspectable and serializable (the precondition for a verified verifier and portable policies); the closure API is unchanged. Prototype.
- **Ergonomic surface over the combinator primitives** (`sugar.go`): read-as-English helpers (`AtMost`, `AtLeast`, `Below`, `Above`, `Is`, `IsNot`, `InRange`; `SetTo`, `Inc`, `Dec`, `IncBy`, `DecBy`, `Raise`, `Lower`) and fluent builders (`r.Rule(name).Require(pred).RepairWith(xform).Add()`, `r.On(name).Does(xform).OnlyIf(guard).Add()`) that **lower to the same combinator AST** the analyzable core is built from. The layering is deliberate and load-bearing: the friendly layer is for humans, the primitive layer is what is serialized, tested, and handed to the machine-checked checker. `sugar_test.go` pins it: a machine written with the sugar serializes (`WriteMachineAST`) to byte-identical AST as the same machine written with raw combinators, so the sugar can never smuggle in semantics the verified oracle does not see.
- **Differential testing against verified oracles** (`Machine.WriteConvergenceTables`, `Registry.WriteMachineAST`): re-certify a built machine, independently of this Go code, using checkers extracted from an axiom-free Coq proof (`normalization-confluence/coq/extraction`). Two angles: the **table oracle** consumes emitted step tables (per-event step functions commute and stay in range; valid states remapped to a compact index; `oracle_test.go`, `GSM_CONVERGENCE_CHECKER`); the **rules oracle** consumes the serialized combinator rules and recomputes convergence straight from the expression trees, so it trusts neither gsm's enumeration nor its normalization (`astoracle_test.go`, `GSM_AST_CHECKER`). A bug in gsm's own verification cannot make a non-convergent machine pass either oracle. `WriteMachineAST` serializes the fragment the Coq model covers (variables with any nonnegative minimum; comparison/`and`/`or`/`not` predicates; `Set`/`Add`/`Sub` transforms; events with an optional guard) and refuses anything outside it, so a passing cross-check always compares like semantics.
- **Footprint-local verification** (`Registry.BuildCompositional`): certify WFC and CC per footprint component instead of over the whole state space. `Build` is capped by global enumeration (20 bits); `BuildCompositional` partitions variables into footprint-connected components and verifies each over its own (small) subspace, so a machine with many independent small invariants certifies even when its global state space is astronomically large. Cross-component event pairs commute by footprint disjointness (the mechanized `disjoint_events_commute` result); shared-footprint pairs are brute-forced within their component. Returns a lazy `Machine` that computes `Apply`/`Normalize` at runtime from the rules (no global tables, so `Export` is unavailable). Preconditions: every invariant declares a footprint, every event declares its writes, the zero state is valid, and no single component exceeds the enumeration budget.
- **Policy as a verifiable artifact** (`Registry.PolicyBytes`, `Registry.PolicyDigest`): a stable, domain-separated SHA-256 over the canonical `WriteMachineAST` serialization, so a combinator policy can be named and anchored independently of who built it. The digested bytes are exactly the verified AST oracle's input, so the anchored digest and the re-checked artifact cannot diverge, and a rule written with the sugar surface digests identically to the equivalent primitive combinators (tested). This is the stable interchange contract an external audit layer (for example an agent SDK's transparency log) pins to.

### Changed
- The convergence theorem is now **machine-checked** (axiom-free Coq/Rocq; see the `normalization-confluence` `coq/` artifact and the `proof: machine-checked` badge). No library behavior change.

## [0.6.0] - 2026-09-23

Compensation synthesis grows up: it now scales (backtracking + forward-checking instead of brute force), takes a preference to steer the repair it chooses, can return the provably minimum-cost repair, and surfaces a concrete witness when convergence is impossible. Breaking: two `Synthesis` fields were renamed.

### Added
- **Optimal synthesis** (`Optimal` option): branch-and-bound for the provably minimum-cost convergent repair, rather than the first ordering-biased one. Uses the accumulated cost plus an admissible lower bound to prune branches that can't beat the best found. When the search completes (`Exhaustive`) the result is guaranteed optimal; if the budget is hit first it's the best-so-far. `Synthesis.Cost` reports the representative repair's total cost.
- **Preference-guided synthesis** (`Registry.SynthesizeWith`, `Prefer`): supply a `cost(from, to)` over repairs and synthesis returns the least-costly convergent one it finds (an ordering bias — tries lower-cost targets first). Encodes a domain policy ("prefer hold over cancel", "toward a safe state", "avoid destructive repairs") so the synthesized repair is the one you *want*, while gsm keeps the convergence guarantee. It only chooses among convergent repairs. `Synthesize()` is now `SynthesizeWith()` with the default policy below.
- **Least-invasive repair by default**: with no preference, synthesis orders each invalid state's candidate repairs by how few variables they change (nearest valid state first), so the representative compensation is the minimal, likely-sensible one — e.g. "clamp the violating variable" rather than an arbitrary reset. Directly mitigates the convergent-≠-desirable caveat.
- **Impossibility witness** (`Synthesis.Witness`): when no compensation converges, synthesis surfaces a concrete critical pair — two events that from a valid state reach *distinct already-valid states* — that no repair can reconcile. Makes "redesign the events" actionable instead of a bare verdict.

### Changed
- **Compensation synthesis now scales via backtracking + forward-checking** (was brute-force enumeration over `|valid|^|invalid|`). Same completeness — it finds a convergent compensation iff one exists, and proves impossibility by exhaustion — but it prunes the assignment tree, handling spaces far beyond the old `2^20` cap (a test solves an `8^8 ≈ 16.7M`-assignment problem in ~10 nodes). Still worst-case exponential (CC synthesis is NP-hard); a search budget now distinguishes **provably impossible** (exhaustive) from **undetermined** (budget hit — a SAT/SMT encoding would settle those). `Synthesis` fields changed: `Alternatives`/`Searched` → `Exhaustive`/`Nodes`.

## [0.5.0] - 2026-09-23

Compensation synthesis: gsm can now *generate* a convergent compensation, not just verify one — or prove none exists. Additive over v0.4.x.

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
