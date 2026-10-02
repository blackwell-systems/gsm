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
- `not-built`: `Build` fails before its convergence check (an invariant left
  without a repair for `BuildOrSynthesize`), so there is no machine to check; the
  machine the program uses instead is listed separately.

The step fails if a checker rejects a machine listed as accepted, a checker
disagrees with `Build`, a program makes a machine `machines.txt` does not list,
or a listed machine is not made. `TestGateCatalogListsEveryExample` (in every
`go test` run) fails if an `Example` function or run block is not in
`machines.txt`.

How it works: built with `-tags gsmgate` and run with `GSM_GATE_DIR=<dir>`, any
program records every machine it makes (each `Build` result, accepted or not,
each synthesized machine and each compositional machine, which fails the gate
because it has no global tables) as the checkers' inputs in that directory
(`gate.go`). `internal/cmd/gsmgate` then runs both checkers on the records and
compares the verdicts with the catalog (`internal/gate`). Without the tag none of
this is compiled. A project that uses gsm can gate its own machines the same way:
build its programs with the tag against a gsm commit that has it, then run

```
go run ./internal/cmd/gsmgate -dumps <dir> -catalog <file> \
  -table-checker <checker> -rules-checker <astchecker> [-scan <repo root>]
```

from that gsm checkout, with one subdirectory of `<dir>` per program. `-scan`
also fails if a directory of the repository creates a gsm registry or federation
outside the catalog's programs, or a document shows one without an `@doc` line.

What the gate certifies: each listed machine (a registry, including each
component of a federation) has the property `Build` checks, decided by the
checkers extracted from the proof. It does not check a federation's morphisms or
its acyclicity, which no extracted checker covers.

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
