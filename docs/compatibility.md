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
| Operator assignment (locals, globals, ivars, cvars, attributes, index) | Supported |
| `if` / `unless` / `elsif` / `while` / `until` | Supported |
| `case` / `when` | Supported |
| `&&`, `||`, `!` | Supported |
| Method calls, positional arguments | Supported |
| `def` with required, optional, post, rest, and keyword parameters | Supported |
| Keyword arguments, splats, and their combinations | Supported |
| Argument forwarding (`...`) and anonymous block forwarding (`&`) | Supported |
| Numbered block parameters (`_1`, `_2`, ...) | Supported |
| Splat arguments and block arguments (`&blk`) | Supported |
| Blocks and `yield` | Supported |
| `break` / `next` (loops, blocks, lambdas) | Supported |
| `return` from methods and blocks | Supported |
| `raise` | Supported |
| Classes, modules, inheritance, `super` (including from blocks) | Supported |
| Nested classes and `A::B` class definitions | Supported |
| Class methods, singleton methods, and `class << self` | Supported |
| `lambda` / `proc` | Supported |
| Multiple assignment (locals, globals, ivars, constants, index, attributes, splats) | Supported |
| `for` (including destructuring targets) | Supported |
| `begin` / `rescue` / `ensure` / `else` | Supported |
| `retry` / `redo` | Supported |
| `alias` / `undef` | Supported |
| Debug information (`-g`, LVAR/DBG) | Supported |
| Pattern matching (value, variable, capture, array, guard, pin) | Supported |

Everything listed as supported is exercised by the conformance suite. The
table is kept in sync with the implementation rather than the ambition.

## Known limitations

The current subset is intentionally conservative:

- `yield` is supported in a method body and in blocks nested inside that
  method. Yielding from class bodies or lambdas is rejected.
- The `it` implicit block parameter is rejected; numbered parameters (`_1`)
  are supported.
- Nested multiple assignment targets such as `(a, b), c = ...` are rejected.
- Constant-path writes and operator assignments (`A::X = 1`, `A::X += 1`)
  are rejected; `A::X` reads work.
- `defined?` is rejected; mruby 4.0 itself compiles it as a method call.
- Pattern matching supports value, variable, capture, array, guard, and pinned
  patterns. Hash patterns, alternation patterns, find patterns, and array
  rest patterns are rejected.
- Splat rescue class lists (`rescue *ERRORS`) are rejected.
- `alias` is limited to method aliases; global-variable aliases are rejected.

These restrictions are checked during compilation and produce
`file:line:column` errors.

## Unsupported syntax

Unsupported constructs produce a compile-time error with a file, line, and
column. They are never silently ignored.

Current explicit rejections include:

- the `it` implicit block parameter
- nested multiple assignment targets and constant-path writes/operators
- safe navigation (`&.`) and other unimplemented node kinds
- hash, alternation, find, and array-rest patterns
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

### Pattern matching

`case` / `in` supports value patterns (which use `===`), variable bindings,
captures (`pattern => var`), array patterns, guards, and pinned variables or
expressions. Array patterns call `deconstruct` and `size` and then index the
result with `[]`, matching mruby's behavior. Unmatched patterns raise
`NoMatchingPatternError` through `MATCHERR` when there is no `else` clause.

### Debug information

The compiler emits the LVAR section by default and the DBG section when `-g`
is requested. DBG uses mruby 4.0's packed line map, so runtime backtraces and
error messages include source file names and line numbers. `--remove-lv`
suppresses local variable names.

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
