/* Definitions the 4.14 bytecode runtime needs when built without sockets
   (and so without the debugger), as on WASI preview 1. */
#define CAML_INTERNALS
#include <stdlib.h>
#include "caml/mlvalues.h"
#include "caml/instruct.h"
#include "caml/misc.h"

/* interp.c references this for the BREAK instruction, which only the
   debugger inserts; the debugger is never active here. */
opcode_t caml_debugger_saved_instruction(code_t pc)
{
  (void)pc;
  caml_fatal_error("debugger not supported on WASI");
}
