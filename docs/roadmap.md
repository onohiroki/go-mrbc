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
- [x] Pattern matching (value, variable, capture, array, rest, find, alternation, hash, guard, pin)
- [x] Operator assignment on locals, globals, ivars, cvars, attributes, index
- [x] Safe navigation, beginless/endless ranges, array/hash splats
- [x] Interpolated symbols and xstrings, backreferences, `defined?`
- [x] Nested multiple assignment and destructured parameters
- [x] Big integer literals
- [x] Constant-path operator assignment (`A::X += 1`, `A::X ||= 1`)
- [x] M5 compile-parity harness over mruby's test tree
- [x] M6 runtime-parity harness over mruby's test tree
- [x] M7 remaining pattern and control-flow syntax
- [x] M8 error compatibility, randomized differential tests, and 4.0.0 CI
- [x] `super`, `yield`, and `...` forwarding inside lambdas
- [x] Optimized opcodes

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

Status: `defined?` and hash patterns are implemented. Alternation, find, and
array-rest patterns remain. Optimized opcodes are implemented; see
[operator-opcodes.ja.md](operator-opcodes.ja.md) for the contract and the
VM-side fallback.

## M5 - Compatibility harness

- Compile mruby's own test tree with both `mrbc` and `go-mrbc`.
- Report files accepted by `mrbc` and rejected by `go-mrbc`.
- Use the failures as the priority list for syntax support.

Status: `cmd/compat` is implemented and run against `mruby/test` and
`mruby/mrbgems`. The 2026-11-19 snapshot reports 237 files, 234 accepted by
both, 0 gaps, and 3 files rejected by both compilers. See
[compatibility-report.md](compatibility-report.md).

The harness compares compilation only. Runtime behavior is covered by the
conformance suite, including cases for modern syntax and destructuring.

## M6 - Runtime parity

- Wrap mruby's test files with `test/assert.rb` so they run under plain
  `mruby`.
- Compile each wrapper with both `mrbc` and `go-mrbc`, run both, and compare
  stdout, stderr, and exit status.
- Treat files where both implementations time out at the same point as a
  match, comparing partial output.
- Fix the compiler defects surfaced by the run.

Status: `cmd/runtimecompat` is implemented. The snapshot of the development
tree covers 160 runnable files and is tracked in the compatibility report; the
primary 4.0.0 measurement is exact.

The run also surfaced and fixed several compiler defects:

- class and module bodies now return their last expression;
- `class ::C` uses `OCLASS` as its outer scope;
- attribute assignment expressions evaluate to the assigned value;
- `-0.0` and `0.0` no longer share a pool entry;
- big integer literals are emitted as `BIGINT` pool entries;
- unnamed rest parameters and named block parameters use mruby's `LVAR`
  layout, fixing `Proc#parameters`;
- `break`/`next` inside a loop in a block target the loop, not the block;
- constant `||=`/`&&=` treats an undefined constant as false;
- constant-path operator assignment is supported for literal paths.

See [compatibility-report.md](compatibility-report.md) for the full snapshot.

## M7 - Remaining language syntax

- Alternation, find, and array-rest patterns.
- `begin ... end while` / `until` (do-while semantics).
- `when *values` in `case`.
- Keyword arguments to `yield`.
- Packed argument arrays for calls with more than 14 positional arguments.
- Rightward pattern matching (`value => pattern`) and the `in` predicate form.
- Parsing and compiling regexp and interpolated-string patterns.

Status: implemented. Against the mruby 4.0.0 tag with default `mrbc` as the
reference, **153 of 153 files compile and 106 of 106 runnable files behave
identically**.

A for-loop scope bug surfaced by the development tree was also fixed: a block
nested in a `for` body can now capture both its own parameters and the for
body's locals. The runtime oracle switched from `mrbc --no-optimize` to the
default `mrbc`, which surfaced and fixed keyword-hash classification, `redo`
unwinding through `ensure`, argument forwarding with leading arguments, and
`super` in top-level blocks. Lambdas now inherit the enclosing method context,
so `super`, `yield`, and `...` forwarding work inside a lambda.

See [compatibility-report.md](compatibility-report.md) for the full snapshot.

## M8 - Verification hardening

- Compare CLI exit status and diagnostic shape for invalid programs.
- Add a seeded randomized differential test that generates programs within
  the supported subset and compares both binaries at runtime.
- Pin mruby to the 4.0.0 tag in CI and make both parity harnesses strict
  (`-fail`).

Status: implemented. `TestErrorCompatibility` checks exit status and located
diagnostics (not exact wording). `TestRandomizedDifferential` runs 50 seeded
programs by default and 200 in CI. `.github/workflows/ci.yml` builds mruby
4.0.0 and runs `go vet`, `go test`, `cmd/compat -fail`, and
`cmd/runtimecompat -fail`.

## Parallel project: go-mrbvm

The pure-Go mruby VM is developed separately as
[`go-mrbvm`](https://github.com/onohiroki/go-mrbvm). It consumes the
`bytecode` package from this module for RITE loading and instruction decoding,
and reuses the conformance corpus. The compiler remains independent of the VM
and always uses mruby 4.0.0 as the reference implementation.

The initial VM plan is kept here so both projects share a single contract:
[go-mrbvm plan](go-mrbvm-plan.md).
