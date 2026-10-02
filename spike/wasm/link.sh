#!/usr/bin/env bash
# Link an OCaml bytecode program (ocamlc -output-obj C file) with the WASI runtime.
# Usage: link.sh <wasi-sdk> <runtime-outdir> <program_byte.c> <out.wasm>
set -euo pipefail
sdk="$1"; rt="$2"; byte="$3"; out="$4"; here="$(cd "$(dirname "$0")" && pwd)"
"$sdk/bin/clang" --target=wasm32-wasip1 "--sysroot=$sdk/share/wasi-sysroot" -O2 -fwrapv -fwrapv-pointer \
  -DCAML_NAME_SPACE -I"$rt/runtime" -Wno-int-to-pointer-cast -Wno-pointer-to-int-cast \
  "$byte" "$here/wasi_main.c" "$rt/libcamlrun.a" \
  -lm -lwasi-emulated-signal -lwasi-emulated-process-clocks -lwasi-emulated-getpid -lwasi-emulated-mman -o "$out"
