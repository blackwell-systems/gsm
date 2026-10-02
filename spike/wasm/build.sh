#!/usr/bin/env bash
# SPIKE: build the extracted checkers as wasm32-wasip1 modules: OCaml bytecode
# (ocamlc -output-obj) linked with the OCaml 4.14.2 bytecode runtime built by
# wasi-sdk. Run where `ocamlc` is OCaml 4.14.2 (the pinned Rocq image).
# Usage: build.sh <ocaml-4.14.2-source> <wasi-sdk> <outdir> <extraction-dir>...
# Each extraction dir holds the extracted *_core.ml(i) and the front ends
# (main.ml, ast_main.ml, main_fast.ml); every front end found is built.
set -euo pipefail
src="$1"; sdk="$2"; out="$3"; shift 3
here="$(cd "$(dirname "$0")" && pwd)"
ver="$(ocamlc -version)"
[ "$ver" = 4.14.2 ] || { echo "ocamlc is $ver, need 4.14.2" >&2; exit 1; }
mkdir -p "$out"
work="$(mktemp -d "${WORK_DIR:-${TMPDIR:-/tmp}}/wasmbuild.XXXXXX")"
bash "$here/build-runtime.sh" "$src" "$sdk" "$work/rt"
for dir in "$@"; do
  for spec in checker:checker_core:main astchecker:checker_core:ast_main checker_fast:fast_core:main_fast; do
    IFS=: read -r name core front <<<"$spec"
    [ -f "$dir/$front.ml" ] && [ -f "$dir/$core.ml" ] || continue
    b="$work/$name"; mkdir -p "$b"
    cp "$dir/$core.ml" "$dir/$core.mli" "$dir/$front.ml" "$b/"
    ( cd "$b" && ocamlc -output-obj -o "${name}_byte.c" "$core.mli" "$core.ml" "$front.ml" std_exit.cmo )
    bash "$here/link.sh" "$sdk" "$work/rt" "$b/${name}_byte.c" "$out/$name.wasm"
  done
done
( cd "$out" && sha256sum *.wasm 2>/dev/null || shasum -a 256 *.wasm )
rm -rf "$work"
