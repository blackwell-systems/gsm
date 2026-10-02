#!/usr/bin/env bash
# Build libcamlrun.a, the OCaml 4.14.2 bytecode runtime, for wasm32-wasip1.
# Usage: build-runtime.sh <ocaml-4.14.2-source> <wasi-sdk> <outdir>
# The configured headers (m.h, s.h, version.h, build_config.h) are checked in
# next to this script; runtime-wasi.patch holds the source changes WASI needs.
set -euo pipefail
src="$1"; sdk="$2"; out="$3"
here="$(cd "$(dirname "$0")" && pwd)"
rm -rf "$out"; mkdir -p "$out/runtime/caml"
cp "$src"/runtime/*.c "$out/runtime/"
cp "$src"/runtime/caml/*.h "$src"/runtime/caml/*.tbl "$out/runtime/caml/"
cp "$here/m.h" "$here/s.h" "$here/version.h" "$out/runtime/caml/"
cp "$here/build_config.h" "$here/wasi_stubs.c" "$out/runtime/"
( cd "$out" && patch -s -p1 < "$here/runtime-wasi.patch" )
cc=("$sdk/bin/clang" --target=wasm32-wasip1 "--sysroot=$sdk/share/wasi-sysroot")
flags=(-O2 -fno-strict-aliasing -fwrapv -fwrapv-pointer -D_FILE_OFFSET_BITS=64 -DCAML_NAME_SPACE -DCAMLDLLIMPORT=
  -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_GETPID -D_WASI_EMULATED_MMAN
  "-ffile-prefix-map=$out=/ocaml-wasi" -Wno-int-to-pointer-cast -Wno-pointer-to-int-cast)
objs=()
for f in interp misc stacks fix_code startup_aux startup_byt freelist major_gc \
  minor_gc memory alloc roots_byt globroots fail_byt signals \
  signals_byt printexc backtrace_byt backtrace compare ints eventlog \
  floats str array io extern intern hash sys meta parsing gc_ctrl md5 obj \
  lexing callback debugger weak compact finalise custom dynlink \
  afl unix bigarray memprof domain skiplist codefrag wasi_stubs; do
  "${cc[@]}" "${flags[@]}" -I"$out/runtime" -c "$out/runtime/$f.c" -o "$out/runtime/$f.o"
  objs+=("$out/runtime/$f.o")
done
"$sdk/bin/llvm-ar" rcsD "$out/libcamlrun.a" "${objs[@]}"
