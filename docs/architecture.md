# Architecture

`go-mrbc` compiles Ruby source to mruby 4.0.0 bytecode. The compiler is written
in Go; the parser is [Prism](https://github.com/ruby/prism) running as
WebAssembly through [go-ruby-prism](https://github.com/danielgatis/go-ruby-prism).

## Pipeline

```
Ruby source
    |
    v
go-ruby-prism (WASM via wazero)
    |  Prism AST (generated Go node structs)
    v
internal/frontend
    |  lowering: Prism AST -> small internal IR
    |  scope information from Prism (names and depths)
    v
internal/compiler
    |  scope/locals analysis, register allocation,
    |  argument specs, control flow, bytecode emission
    v
bytecode.Irep
    |
    v
bytecode.DumpBinary
    |
    v
.mrb (RITE 04.00)
    |
    v
mruby 4.0.0 VM
```

The parser can also be invoked directly with `--dump-ast`. The lowered IR can
be inspected with `--dump-ir`, and the generated instruction sequence with
`--verbose`.

## Why a separate IR?

Prism's AST is large and changes with upstream Ruby versions. Lowering it to a
small internal IR gives us:

- one place to normalize `unless`, `until`, modifier forms, `for`, operator
  assignment, and multiple assignment;
- a stable surface for code generation and golden tests;
- a clear list of supported constructs. Unknown Prism nodes are rejected with
  a located error rather than ignored.

The IR is private to this module. The only public contract for other tools is
the `bytecode` package.

## Packages

| Package | Responsibility |
| --- | --- |
| `cmd/gomrbc` | Command-line interface |
| `mrbc` | Public compile API (`Compile`, `CompileFile`, `Dump`) |
| `internal/frontend` | Prism AST lowering, IR, IR printer |
| `internal/compiler` | Scope analysis, register allocation, code generation |
| `bytecode` | RITE/opcode model, encoder, decoder, disassembler |
| `toolchain` | Locating `mruby`/`mrbc` for tests |

## Scopes and locals

Ruby's scoping rules are resolved by Prism. A `LocalVariableReadNode` or
`LocalVariableWriteNode` carries a `Depth` field:

- `depth == 0`: a local in the current scope;
- `depth == n`: a local in the n-th enclosing block scope.

mruby represents these as `GETUPVAR` / `SETUPVAR` with `(slot, level)`, where
`level` is the number of enclosing ireps to walk. The compiler maps Prism's
`depth` to mruby's `level` and assigns register slots per scope.

Each scope owns a local table:

- register `R0` is `self`;
- method and block parameters occupy a fixed layout determined by the VM's
  `OP_ENTER` semantics;
- other locals follow the parameters;
- `nlocals` is the size of the local slot range (including `R0`);
- `nregs` is the maximum register index used by temporaries plus one.

Prism's `locals` array is also used to predeclare variables that are assigned
later in the same scope, preserving Ruby's "assignment anywhere in the scope"
semantics.

### Keyword arguments

When a method, block, or lambda declares keyword parameters, the VM reserves a
hidden keyword dictionary register immediately after the positional
parameters, followed by the hidden block register. Named keyword locals are
allocated after those slots. The compiler emits `KARG`, `KEY_P`, and `KEYEND`
in the prologue to extract required keywords, apply optional keyword defaults,
and reject unknown keywords.

A call with keyword arguments places key/value pairs in consecutive registers
after the positional arguments and encodes their count in the high nibble of
the send operand. A keyword hash (`**hash`) is passed as a single hash register
using the reserved count 15.

### Class, module, and lambda scopes

Class and module bodies execute as separate ireps through `CLASS`/`MODULE`
followed by `EXEC`. `class << expr` first evaluates `expr`, obtains its
singleton class with `SCLASS`, and executes the body with the same `EXEC`
mechanism. Their `R0` is the class or module object, they do not have an
`OP_ENTER`, and they reserve no implicit block register. Class-body locals and
blocks inside the body use the same upvar mechanism as other scopes.

Lambdas are compiled as strict closure ireps created with `LAMBDA`. They
reserve a block register like methods, use `RETURN` for both explicit
`return` and `next`, and cannot yield to an enclosing method.

### Exceptions and unwinding

`begin` / `rescue` / `ensure` are compiled with mruby's catch-handler table.
Each handler records absolute byte offsets for the protected range and the
handler target. Ensure entries are registered before rescue entries for the
same construct, and nested constructs append their entries later; mruby
searches the table backwards, so the innermost handler wins.

Rescue handlers start with `EXCEPT`, test exception classes with `RESCUE`, and
re-raise unmatched exceptions with `RAISEIF`. Ensure handlers save the pending
exception with `EXCEPT`, run the ensure body, and re-raise with `RAISEIF`.

Control flow that leaves a protected region (`break`, `next`, `retry`,
`redo`) is emitted with `JMPUW`, which unwinds ensure handlers on the way.
`return` and non-local block returns rely on the VM's own return unwinding.

## Register machine

mruby bytecode is a register machine. Expression evaluation uses a stack of
temporary registers starting after the locals. The compiler tracks:

- `cursp`: the first free temporary register;
- `maxsp`: the high-water mark, used for `nregs`;
- the destination register of each expression.

Calls reserve consecutive registers for the receiver, positional arguments,
keyword arguments, and the block argument. The exact layout follows mruby's
`OP_SEND` family and is verified against `mrbc -v` output.

## Correctness first, optimization later

The first backend emits straightforward bytecode:

- arithmetic and comparison operators are compiled as ordinary method calls
  (`SEND`), not as specialized opcodes;
- no peephole optimizer is run;
- debug and local-variable sections are omitted initially.

This keeps the code generator small and easy to verify. The generated code is
still valid mruby bytecode; specialized opcodes can be added later without
changing semantics.

## Relationship to a pure-Go VM

The bytecode format is the contract between this compiler and a future pure-Go
mruby VM:

- `go-mrbc` exposes RITE reading/writing and opcode definitions in the
  `bytecode` package;
- the VM imports that package for its loader and decoder;
- the compiler never imports the VM;
- conformance programs and expected outputs are kept in a form the VM project
  can reuse.

The only shared definitions are the RITE 04.00 container, the mruby 4.0.0
instruction set, and the value/pool encodings. Internal compiler IR is not part
of the contract.
