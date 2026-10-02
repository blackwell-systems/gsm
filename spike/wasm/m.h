/* OCaml 4.14 runtime configuration for wasm32-wasi with 64-bit OCaml values.
   Pointers are 32-bit, but value/intnat are 64-bit so OCaml int is 63-bit,
   exactly as on the native amd64/arm64 builds the checkers are proven for. */
#define ARCH_SIXTYFOUR 1
#define SIZEOF_INT 4
#define SIZEOF_LONG 4
#define SIZEOF_PTR 8
#define SIZEOF_SHORT 2
#define SIZEOF_LONGLONG 8
#define ARCH_INT64_TYPE long long
#define ARCH_UINT64_TYPE unsigned long long
#define ARCH_INT64_PRINTF_FORMAT "ll"
#define PROFINFO_WIDTH 0
#define CAML_SAFE_STRING 1
#define FLAT_FLOAT_ARRAY 1
#define SUPPORTS_ALIGNED_ATTRIBUTE 1
