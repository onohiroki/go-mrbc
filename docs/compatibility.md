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
| Local/global operator assignment (`+=`, `||=`, `&&=`) | Supported |
| Constants (read) | Supported |
| `if` / `unless` / `elsif` / `while` / `until` | Supported |
| `case` / `when` | Supported |
| `&&`, `||`, `!` | Supported |
| Method calls, positional arguments | Supported |
| `def` with required, optional, and rest parameters | Supported |
| Blocks and `yield` | Supported |
| `break` / `next` in `while` / `until` loops | Supported |
| `return` from methods (and `return` from blocks) | Supported |
| `raise` | Supported |
| Classes, modules, inheritance, `super` | Planned |
| Instance variables and constants (write) | Planned |
| Keyword arguments, splat arguments | Planned |
| `lambda` / `proc` | Planned |
| Multiple assignment | Planned |
| `for` | Planned |
| `begin` / `rescue` / `ensure` / `retry` | Planned (late) |
| Pattern matching (`case` / `in`) | Planned (late) |

Everything listed as supported is exercised by the conformance suite. The
table is kept in sync with the implementation rather than the ambition.

## Known limitations

The current subset is intentionally conservative:

- `break` and `next` are only implemented for `while` and `until` loops.
  Using them inside a block is rejected with a compile error rather than
  compiled with incorrect non-local control flow.
- `yield` is supported directly in a method body. Yielding from inside a
  nested block is rejected until the environment handling is implemented.
- `for` loops are not implemented yet.
- Block arguments can be declared (`def f(&b)`) and called (`b.call`), but
  forwarding a block with `g(&b)` is not supported at call sites.
- Keyword arguments, splat arguments, and multiple assignment are rejected.
- Operator assignment to targets other than locals and globals is rejected
  (for example `a[0] += 1` and `obj.attr ||= 1`).
- Methods with post parameters or keyword parameters are rejected.
- Classes, modules, and singleton methods are rejected.
- Lambda literals are rejected.
- `begin` / `rescue` / `ensure` are rejected.

These restrictions are checked during compilation and produce
`file:line:column` errors.

## Unsupported syntax

Unsupported constructs produce a compile-time error with a file, line, and
column. They are never silently ignored.

Current explicit rejections include:

- `for` loops (until block-scope emulation is implemented)
- `begin` / `rescue` / `ensure` (until the exception milestone)
- lambda literals and numbered block parameters
- safe navigation (`&.`) and other unimplemented node kinds
- pattern matching
- `alias`, `undef`, and singleton class definitions
- `defined?`
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

In the initial milestones, `raise` is available because it is an ordinary
method call. Rescuing exceptions requires catch-handler generation and is
deferred. Programs that use `begin` / `rescue` are rejected with a clear error
until then.

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
