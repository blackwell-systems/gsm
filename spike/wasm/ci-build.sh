#!/usr/bin/env bash
# SPIKE: build the native checkers and the wasm modules inside the pinned Rocq
# image, twice from scratch (outputs a/ and b/) so the job can check that the
# wasm build is reproducible.
# Usage: ci-build.sh <image> <nc-pinned-checkout> <nc-spike-checkout> <toolchain-dir> <out-dir>
#   toolchain-dir holds ocaml-4.14.2/ (source) and wasi-sdk/ (x86_64-linux).
set -euo pipefail
image="$1"; nc="$2"; ncs="$3"; tc="$4"; out="$5"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$out"
chmod -R a+rwX "$nc" "$ncs" "$tc" "$out"
docker run --rm --platform linux/amd64 -v "$nc:/work/nc" -v "$ncs:/work/ncs" -v "$tc:/work/tc" \
  -v "$out:/work/out" -v "$here:/work/wasm:ro" "$image" bash -euo pipefail -c '
  eval "$(opam env 2>/dev/null)" || true
  if ! command -v coqc >/dev/null 2>&1; then
    mkdir -p "$HOME/.local/bin"
    printf "#!/bin/sh\nexec %s compile \"\$@\"\n" "$(command -v rocq)" > "$HOME/.local/bin/coqc"
    chmod +x "$HOME/.local/bin/coqc"
    export PATH="$HOME/.local/bin:$PATH"
  fi
  coqc --version; ocamlc -version
  (cd /work/nc/coq/extraction && make >/dev/null)
  (cd /work/ncs/coq/extraction && make >/dev/null)
  mkdir -p /work/out/native /tmp/fast
  cp /work/nc/coq/extraction/checker /work/nc/coq/extraction/astchecker /work/ncs/coq/extraction/checker_fast /work/out/native/
  cp /work/ncs/coq/extraction/fast_core.ml /work/ncs/coq/extraction/fast_core.mli /work/ncs/coq/extraction/main_fast.ml /tmp/fast/
  for o in a b; do
    WORK_DIR=/tmp bash /work/wasm/build.sh /work/tc/ocaml-4.14.2 /work/tc/wasi-sdk /work/out/$o \
      /work/nc/coq/extraction /tmp/fast 2>&1 | grep -v -e "warning:" -e "^ " -e "warnings generated" -e "note:" || true
  done
  sha256sum /work/nc/coq/extraction/checker_core.ml /work/ncs/coq/extraction/fast_core.ml /work/out/native/*
'
