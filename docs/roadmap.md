# Roadmap

The detailed, maintained plan is written in Japanese in
[implementation-plan.ja.md](implementation-plan.ja.md). This page is a short
English summary.

## Current progress

- [x] RITE 04.00 writer and reader
- [x] Opcode encoder/decoder with `EXT` support
- [x] Prism lowering and located errors
- [x] Scalar expressions, locals, closures, globals, constants
- [x] Conditionals, loops, `case`/`when`, `break`, `next`
- [x] Methods with required/optional/post/rest/keyword parameters
- [x] Argument forwarding, anonymous block forwarding, numbered parameters
- [x] Blocks, `yield`, and `return`
- [x] Arrays, hashes, ranges, string interpolation
- [x] Classes, modules, instance and class variables, constants
- [x] Inheritance, `super`, class and singleton methods, `class << self`
- [x] Keyword arguments, splats, block arguments, and their combinations
- [x] Lambdas and procs
- [x] Multiple assignment (including index/attribute and splat targets)
- [x] `for` loops (including destructuring)
- [x] `raise`, `begin` / `rescue` / `ensure`, `retry`, `redo`
- [x] `alias` / `undef`
- [x] Debug information (`-g`, LVAR/DBG)
- [x] Pattern matching (value, variable, capture, array, guard, pin)
- [x] Operator assignment on locals, globals, ivars, cvars, attributes, index
- [ ] Optimized opcodes

## M0 - Parser integration

- Add the go-ruby-prism parser.
- Lower Prism nodes into a small internal IR.
- Report Prism errors and warnings with file, line, and column.
- Reject unsupported syntax explicitly.
- Verify Prism's local-variable scope information against `mrbc -v`.

## M0.5 - RITE writer

- Implement the opcode table and instruction encoder, including `EXT1`/`EXT2`/
  `EXT3`.
- Implement the RITE 04.00 container, irep records, pools, and symbol tables.
- Execute a hand-written irep on mruby 4.0.0 as a smoke test.
- Add a Go disassembler for `--verbose`.

## M1 - Practical macro subset

- Scalars, strings, symbols, arrays, hashes, and ranges.
- Locals, globals, and constant reads.
- Conditionals, loops, `case`/`when`, `break`, `next`, and `return`.
- Methods with required, optional, and rest parameters.
- Blocks and `yield`.
- `raise` (without rescue).

## M2 - Object model

- Classes, modules, inheritance, and `super`.
- Instance variables, class variables, and constant writes.
- Keyword arguments, splat arguments, block parameters.
- Lambdas and procs.
- Multiple assignment.

Status: implemented for the supported subset. See
[Compatibility](compatibility.md#known-limitations) for the remaining
restrictions.

## M3 - Exceptions and control flow

- `begin` / `rescue` / `ensure` and catch handlers.
- Unwinding across ensures.
- `retry` and `redo`.
- `alias` / `undef`.
- Debug information and local variable sections.

Status: exceptions, unwinding, `retry`/`redo`, `alias`, `undef`, debug
information, and a practical pattern-matching subset are implemented.

## M4 - Expansion and optimization

- Optimized opcodes for arithmetic, comparison, and calls.
- Pattern matching extensions (hash, alternation, find, rest patterns).
- `defined?`.
- Performance improvements.

## Parallel project: pure-Go mruby VM

A pure-Go mruby VM is developed separately. It will consume the `bytecode`
package from this module for RITE loading and instruction decoding, and reuse
the conformance corpus. The compiler remains independent of the VM and always
uses mruby 4.0.0 as the reference implementation.
