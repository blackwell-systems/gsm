/* Entry point for an OCaml bytecode program embedded with ocamlc -output-obj.
   The runtime parameters are fixed here, not taken from the host environment,
   so every host runs the checker with the same limits: a 256 MiB (32 Mi words)
   bytecode stack, enough for the extracted code's non-tail recursion over
   2^20-element lists. */
#define CAML_INTERNALS
#include <stdlib.h>
#include <caml/callback.h>
#include <caml/sys.h>
int main(int argc, char **argv)
{
  (void)argc;
  setenv("OCAMLRUNPARAM", "l=32M", 1);
  unsetenv("CAMLRUNPARAM");
  caml_startup(argv);
  caml_do_exit(0);
  return 0;
}
