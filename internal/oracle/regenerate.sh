#!/usr/bin/env bash
# Regenerate oracle_gen.go from the proof repository, as CI does, and check it
# against PROVENANCE. Needs git, docker and go.
#   internal/oracle/regenerate.sh [<proof commit>]   (default: PROVENANCE's NC_COMMIT)
# With a commit other than the pinned one it writes oracle_gen.go and prints the
# new hashes for PROVENANCE instead of checking them.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
# PROVENANCE's KEY=value lines (keys may contain digits: JSON_SHA256).
eval "$(grep -E '^[A-Z0-9_]+=' "$here/PROVENANCE" | sed -E 's/^([A-Z0-9_]+)=(.*)$/\1="\2"/')"
commit="${1:-$NC_COMMIT}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git clone -q "https://github.com/$NC_REPO" "$work/nc"
git -C "$work/nc" checkout -q "$commit"
bash "$work/nc/coq/goextract/ci-extract.sh" "$ROCQ_IMAGE"
json="$work/nc/coq/goextract/oracle_core.json"
# shellcheck disable=SC2086
(cd "$work/nc/coq/goextract" && go run ./gogen $GOGEN_ARGS -o "$work/oracle_gen.go" oracle_core.json)
json_sum="$(sha256sum "$json" | cut -d' ' -f1)"
go_sum="$(sha256sum "$work/oracle_gen.go" | cut -d' ' -f1)"
echo "JSON_SHA256=$json_sum"
echo "GO_SHA256=$go_sum"
if [ "$commit" != "$NC_COMMIT" ]; then
  cp "$work/oracle_gen.go" "$here/oracle_gen.go"
  echo "wrote oracle_gen.go from $commit; set NC_COMMIT=$commit and the two hashes above in PROVENANCE"
  exit 0
fi
status=0
[ "$json_sum" = "$JSON_SHA256" ] || { echo "FAIL: the extracted JSON is not the pinned JSON"; status=1; }
[ "$go_sum" = "$GO_SHA256" ] || { echo "FAIL: the generated Go is not the pinned Go"; status=1; }
cmp -s "$work/oracle_gen.go" "$here/oracle_gen.go" || { echo "FAIL: oracle_gen.go differs from the regenerated Go"; diff "$work/oracle_gen.go" "$here/oracle_gen.go" | head -40; status=1; }
[ "$status" = 0 ] && echo "oracle_gen.go is the pinned proof commit's generated oracle, byte for byte"
exit $status
