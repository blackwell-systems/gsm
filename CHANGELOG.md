# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/blackwell-systems/gsm/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/blackwell-systems/gsm/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/blackwell-systems/gsm/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/blackwell-systems/gsm/releases/tag/v0.1.0
