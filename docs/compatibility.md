# Compatibility

## Target

- Bytecode format: **RITE 04.00**
- VM: **mruby 4.0.0**
- Parser: Prism (Ruby 3.3/3.4 syntax) through go-ruby-prism
- Reference compiler: `mrbc` from mruby 4.0.0

The acceptance criterion is **behavioral compatibility**: a program compiled by
`go-mrbc` must produce the same observable behavior as the same program
compiled by `mrbc` when both run on mruby 4.0.0. Output, raised errors, and
exit status are compared.

Byte-for-byte identical `.mrb` output is explicitly not a goal. `go-mrbc` does
not initially run a peephole optimizer and compiles many operators as ordinary
method calls, so its bytecode is larger than `mrbc`'s. This does not affect
behavior.

## Supported today

The following table describes the implementation target of the current
milestones. Features are enabled as their compiler support lands.

| Feature | Status |
| --- | --- |
| `nil`, `true`, `false` | Supported |
| Integers (32/64-bit) | Supported |
| Floats | Supported |
| Strings, symbols, string interpolation | Supported |
| Arrays, hashes, ranges | Supported |
| `%w` word arrays | Supported |
| Local variables and closures | Supported |
| Global variables | Supported |
| Instance and class variables | Supported |
| Constants (simple writes, reads, and `A::B` reads) | Supported |
| Operator assignment (`+=`, `||=`, `&&=` on locals, globals, ivars, cvars) | Supported |
| `if` / `unless` / `elsif` / `while` / `until` | Supported |
| `case` / `when` | Supported |
| `&&`, `||`, `!` | Supported |
| Method calls, positional arguments | Supported |
| `def` with required, optional, and rest parameters | Supported |
| Keyword parameters and keyword arguments | Supported |
| Splat arguments and block arguments (`&blk`) | Supported |
| Blocks and `yield` | Supported |
| `break` / `next` (loops, blocks, lambdas) | Supported |
| `return` from methods and blocks | Supported |
| `raise` | Supported |
| Classes, modules, inheritance, `super` | Supported |
| Nested classes and `A::B` class definitions | Supported |
| Class methods, singleton methods, and `class << self` | Supported |
| `lambda` / `proc` | Supported |
| Multiple assignment (including splat targets) | Supported |
| `for` | Supported |
| `begin` / `rescue` / `ensure` / `else` | Supported |
| `retry` / `redo` | Supported |
| `alias` / `undef` | Supported |
| Pattern matching (`case` / `in`) | Planned |

Everything listed as supported is exercised by the conformance suite. The
table is kept in sync with the implementation rather than the ambition.

## Known limitations

The current subset is intentionally conservative:

- `yield` is supported in a method body and in blocks nested inside that
  method. Yielding from class bodies or lambdas is rejected.
- `for` loops support a single local, global, instance-variable, or constant
  target. Destructuring targets (`for a, b in ...`) are rejected.
- Anonymous block forwarding (`def f(&); g(&); end`) is rejected.
- Keyword arguments combined with positional splats in the same call are
  rejected (`f(*a, k: 1)`).
- Keyword splats combined with explicit keyword pairs are rejected
  (`f(**h, k: 1)`).
- Explicit keyword arguments to `super` are rejected, as is forwarding from a
  method that declares both named keywords and `**rest`.
- Multiple assignment supports local, global, instance-variable, constant, and
  splat targets. Index/attribute targets (`a[0], b = ...`) are rejected.
- Nested constant writes (`A::X = 1`) are rejected; `A::X` reads work.
- Operator assignment to attributes and index expressions is rejected
  (for example `a[0] += 1` and `obj.attr ||= 1`).
- Methods with post parameters are rejected.
- `defined?` is rejected; mruby 4.0 itself compiles it as a method call.
- Splat rescue class lists (`rescue *ERRORS`) are rejected.
- `alias` is limited to method aliases; global-variable aliases are rejected.
- Debug information is not emitted, so runtime error line numbers are absent.

These restrictions are checked during compilation and produce
`file:line:column` errors.

## Unsupported syntax

Unsupported constructs produce a compile-time error with a file, line, and
column. They are never silently ignored.

Current explicit rejections include:

- numbered block parameters and anonymous block forwarding
- safe navigation (`&.`) and other unimplemented node kinds
- pattern matching
- `defined?`
- splat rescue class lists and global-variable aliases
- `BEGIN` / `END` blocks
- regular expressions, until runtime support is chosen
- `eval`, `load`, and `require` (host responsibilities)
- encoding and magic comments
- fibers, threads, refinements, and `TracePoint`

## Semantic notes

### Operators

Arithmetic, comparison, indexing, and unary operators are compiled as method
calls. For example, `a + b` becomes a `SEND` of `:+`. This matches Ruby's
method-dispatch semantics and avoids relying on specialized opcodes before
their behavior is verified. Optimized opcodes may be added later.

### Exceptions

`begin` / `rescue` / `ensure` and the `rescue` modifier are supported,
including multiple rescue clauses, `else`, and rescue-variable binding.
`retry` is supported inside rescue clauses, and `redo` is supported in loops,
blocks, lambdas, and `for` bodies. Ensure handlers run when control leaves the
protected region through `return`, `break`, or `next` because the compiler
emits `JMPUW` for loop jumps and relies on the VM's return unwinding.

The compiler emits mruby's catch-handler table (rescue and ensure entries)
directly. Splat rescue class lists are not implemented yet.

### Debug information

The first releases do not emit the `DBG` or `LVAR` sections. Backtraces still
work, but source line numbers and local variable names may be missing from
runtime errors. This is a known limitation, not a semantic difference in
successful programs.

### Missing standard library

The compiler does not control which mrbgems are linked into the mruby VM. A
method that is unavailable in the target VM will raise `NoMethodError` even if
the script compiles successfully. Users embedding the bytecode should define
and document their runtime profile (core plus selected mrbgems) as part of
their tool, not as part of this compiler.

## Versioning

The compiler rejects or avoids features that require a different bytecode
format. The RITE version and opcode table are pinned to mruby 4.0.0. Support
for mruby 3.x is not planned.
