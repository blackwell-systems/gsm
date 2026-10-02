# Pinned extracted checkers

The `oracles` CI job builds the two checkers extracted from the Coq proof
(`checker`, the table oracle, and `astchecker`, the rules oracle) and runs gsm's
oracle tests and the differential cross-check against them. Those tests are
required there: the job sets `GSM_REQUIRE_ORACLES=1`, so a missing checker fails
instead of skipping.

- `pins.env` names the proof repository, the exact commit, and the Rocq image by
  digest. The job checks out that commit and runs its
  `coq/extraction/ci-build.sh` in that image.
- `SHA256SUMS` holds the expected hashes of the extracted OCaml
  (`checker_core.ml`, `checker_core.mli`) and of both binaries. The job fails if
  the rebuild does not reproduce them byte for byte. The proof repository's
  `oracles` workflow prints the same hashes for every commit.

To move to a new proof commit, update `NC_COMMIT` and `SHA256SUMS` together, from
that commit's `oracles` run summary.

To run the cross-check locally, build the checkers (`make` in
`normalization-confluence/coq/extraction`, or the CI script with Docker), then:

```
GSM_REQUIRE_ORACLES=1 \
GSM_CONVERGENCE_CHECKER=/path/to/checker \
GSM_AST_CHECKER=/path/to/astchecker \
  go test -count=1 .
```

`GSM_ORACLE_REPORT=<file>` writes the differential summary to a file.
