# Pinned extracted checkers

The `oracles` CI job builds the two checkers extracted from the Coq proof
(`checker`, the table oracle, and `astchecker`, the rules oracle) and runs gsm's
oracle tests and the differential cross-check against them. Both checkers decide
the property `Build` checks, so the cross-check fails on any disagreement except
a rules-oracle refusal outside its arithmetic fragment. `GSM_DIFF_RANDOM=<n>`
sets the number of random combinator machines (default 600 with the oracles). Those tests are
required there: the job sets `GSM_REQUIRE_ORACLES=1`, so a missing checker fails
instead of skipping.

- `pins.env` names the proof repository, the exact commit, and the Rocq image by
  digest. The job checks out that commit and runs its
  `coq/extraction/ci-build.sh` in that image.
- `pins.env` also names the two checker binaries (`TABLE_CHECKER`,
  `RULES_CHECKER`) inside `coq/extraction`. A faster checker proven equal to the
  current one replaces it by changing that name, `NC_COMMIT` and `SHA256SUMS`;
  nothing else refers to the binary.
- `SHA256SUMS` holds the expected hashes of the extracted OCaml
  (`checker_core.ml`, `checker_core.mli`) and of both binaries. The job fails if
  the rebuild does not reproduce them byte for byte, or if a checker the pins name
  is not listed there. The proof repository's `oracles` workflow prints the same
  hashes for every commit.

To move to a new proof commit, update `NC_COMMIT` and `SHA256SUMS` together, from
that commit's `oracles` run summary.

## Example-machine gate

The job's "Example-machine gate" step checks every machine gsm's examples make:
each `Example` function and each `gocheck: run` block of the checked docs (the
README flagship). `machines.txt` lists every such machine and its expected
verdict:

- `certified`: `Build` accepts it and both checkers verify it.
- `certified-tables`: `Build` accepts it and the table checker verifies it. Its
  rules are closures, which `WriteMachineAST` cannot export, so the rules checker
  does not run. Every example machine with events or invariants is of this kind
  today.
- `rejected`: `Build` rejects it for WFC or CC, and every checker that can run
  rejects it for the same reason.
- `synthesized`: a synthesized repair, which the table checker verifies.
- `not-built`: `Build` rejects the registry as written and neither its rules
  nor its tables can be exported (an invariant left without a repair for
  `BuildOrSynthesize`), so no checker can run on it; the machine the program uses
  instead is listed separately.

The step fails if a checker rejects a machine listed as accepted, refuses its
input or crashes (is killed by a signal), a checker disagrees with `Build`, a
program makes a machine `machines.txt` does not list, or a listed machine is not
made. `TestGateCatalogListsEveryExample` (in every `go test` run) fails if an
`Example` function or run block is not in `machines.txt`. An example that makes
no machine is listed `@none <program> <reason>`, and the gate fails if it makes
one.

How it works: built with `-tags gsmgate` and run with `GSM_GATE_DIR=<dir>`, any
program records every machine it makes (each `Build` result, accepted or not,
each synthesized machine and each compositional machine, which fails the gate
because it has no global tables) as the checkers' inputs in that directory
(`gate.go`). `internal/cmd/gsmgate` then runs both checkers on the records and
compares the verdicts with the catalog (`internal/gate`). Without the tag none of
this is compiled. A project that uses gsm can gate its own machines the same way:
build its programs (main packages) with the tag against a gsm commit that has it,
run each once, with no arguments, recording into its own subdirectory of `<dir>`
named by its directory, then run, from `internal/cmd/gsmgate` of that gsm
checkout (its own module, since it needs `golang.org/x/tools`):

```
go run . -dumps <dir> -catalog <file> \
  -table-checker <checker> -rules-checker <astchecker> \
  [-rerun <dir2>] [-scan <repo root>]
```

`-scan` loads every Go module under the repository root, hidden, `testdata`,
`vendor` and `node_modules` directories included, with its full import graph
(test code excluded), and type-checks the packages its rules need. It fails if:

- a main package depends on gsm, directly or through any module (one in a hidden
  directory or outside the repository included), and is not a catalog program;
- a package refers to a gsm function or method that makes a machine
  (`NewRegistry`, `NewFederation`, a `Build`, `BuildOrSynthesize`,
  `BuildCoordinated`, `BuildCompositional`, `Synthesize`, `Certify` or
  `Synthesis.Machine`, called or taken as a value, under any import name) and is
  not a catalog program, or a catalog program is not a main package;
- a catalog program, or any package of the repository it imports, reads an input
  that could make another run differ: flags, `os.Args`, the environment (`os`,
  `syscall`), `os.Stdin`, files (`os.ReadFile`, `Open`, `OpenFile`, `ReadDir`,
  `DirFS`, `Getwd`) or the platform (`runtime.GOOS`, `GOARCH`), or has a `.go`
  file a build constraint excludes;
- a non-test Go file imports gsm but is in no loaded package;
- a directory is a symbolic link (the scan does not follow it);
- a document (`.md`, `.mdx`, `.markdown`, `.rst`, `.adoc`, `.txt`, `.html`)
  shows a gsm machine without an `@doc` line.

The clock, randomness and the host name are not in that list: bide's core reads
them for timestamps, keys and lease owners. `-rerun <dir2>`, the records of a
second run of the same programs, catches a machine that depends on them: the
gate fails if a program makes different machines on the two runs. There is no
exemption for a package that makes machines.

What the gate certifies: each listed machine (a registry, including each
component of a federation) has the property `Build` checks, decided by the
checkers extracted from the proof. Federation-level checks (morphisms,
resolvers, acyclicity, the monotone-cycle check) are done by `Build`, not by an
extracted checker.

To run it locally, with the checkers built:

```
GSM_CONVERGENCE_CHECKER=/path/to/checker GSM_AST_CHECKER=/path/to/astchecker \
  go test -tags gsmgate -run TestExampleMachinesGate -count=1 -v .
```

To run the cross-check locally, build the checkers (`make` in
`normalization-confluence/coq/extraction`, or the CI script with Docker), then:

```
GSM_REQUIRE_ORACLES=1 \
GSM_CONVERGENCE_CHECKER=/path/to/checker \
GSM_AST_CHECKER=/path/to/astchecker \
  go test -count=1 .
```

`GSM_ORACLE_REPORT=<file>` writes the differential summary to a file.
