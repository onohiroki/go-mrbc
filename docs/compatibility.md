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
| Integers (32/64-bit), `Rational` and `Imaginary` literals | Supported |
| Floats | Supported |
| Strings, symbols, string interpolation | Supported |
| Interpolated symbols (`:"a#{x}b"`) and xstrings | Supported |
| Regular expression literals, including interpolation | Compiled (runtime gem required) |
| Arrays, hashes, ranges (including beginless and endless) | Supported |
| Array and hash splats (`[1, *a]`, `{**h}`) | Supported |
| `%w` word arrays | Supported |
| Local variables and closures | Supported |
| Global variables, numbered references (`$1`) and backreferences (`$&`) | Supported |
| Instance and class variables | Supported |
| Constants (writes, reads, and `A::B` reads/writes) | Supported |
| Operator assignment (locals, globals, ivars, cvars, attributes, index) | Supported |
| `if` / `unless` / `elsif` / `while` / `until` | Supported |
| `case` / `when` | Supported |
| `&&`, `||`, `!` | Supported |
| Method calls, positional arguments, safe navigation (`&.`) | Supported |
| `def` with required, optional, post, rest, and keyword parameters | Supported |
| Destructured parameters (`def f((a, b))`) | Supported |
| Keyword arguments, splats, and their combinations | Supported |
| Argument forwarding (`...`) and anonymous block forwarding (`&`) | Supported |
| Numbered block parameters (`_1`, `_2`, ...) | Supported |
| Splat arguments and block arguments (`&blk`) | Supported |
| Blocks and `yield` (including splatted `yield` arguments) | Supported |
| `break` / `next` (loops, blocks, lambdas) | Supported |
| `return` with a single value or multiple values | Supported |
| `raise` | Supported |
| Classes, modules, inheritance, `super` (including from blocks) | Supported |
| Nested classes and `A::B` class definitions | Supported |
| Class methods, singleton methods, and `class << self` | Supported |
| `lambda` / `proc` | Supported |
| Multiple assignment (locals, globals, ivars, constants, index, attributes, splats) | Supported |
| Nested and destructuring multiple assignment (`(a, b), c = ...`) | Supported |
| `for` (including destructuring targets) | Supported |
| `begin` / `rescue` / `ensure` / `else` | Supported |
| `retry` / `redo` | Supported |
| `alias` / `undef` | Supported |
| Splat rescue class lists (`rescue *ERRORS`) | Supported |
| `defined?` | Supported |
| `__FILE__`, `__LINE__`, `__ENCODING__` | Supported |
| Debug information (`-g`, LVAR/DBG) | Supported |
| Pattern matching (value, variable, capture, array, hash including `**rest`, guard, pin) | Supported |

Everything listed as supported is exercised by the conformance suite. The
table is kept in sync with the implementation rather than the ambition.

## Known limitations

The current subset is intentionally conservative:

- `yield` is supported in a method body and in blocks nested inside that
  method. Yielding from class bodies or lambdas is rejected.
- The `it` implicit block parameter is rejected; numbered parameters (`_1`)
  are supported.
- Constant-path operator assignment (`A::X += 1`) is rejected; `A::X = 1`
  writes and `A::X` reads work.
- Pattern matching supports value, variable, capture, array, hash (including
  `**rest`), guard, and pinned patterns. Alternation patterns, find patterns,
  and array rest patterns (`[a, *b]`) are rejected.
- `alias` is limited to method names; global-variable aliases are rejected.
- `begin ... end while` / `begin ... end until` loops are rejected.
- `case/in` without a subject expression is rejected.
- Hash splats are rejected inside a positional hash argument to a method
  (`foo("k" => 1, **h)`).
- Dynamic constant names (`A.const_set(name, x)`) are rejected; constant paths
  must be literal.
- Regular expression literals compile, but executing them requires an mruby
  build with the `mruby-regexp` mrbgem. Matching operators (`=~`) are not
  lowered yet.

These restrictions are checked during compilation and produce
`file:line:column` errors.

## Unsupported syntax

Unsupported constructs produce a compile-time error with a file, line, and
column. They are never silently ignored.

Current explicit rejections include:

- the `it` implicit block parameter
- constant-path operator assignment (`A::X += 1`)
- alternation, find, and array-rest patterns
- `BEGIN` / `END` blocks
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
directly. Splat rescue class lists (`rescue *ERRORS`) are compiled by evaluating
the class list and testing it with `__case_eqq`.

### Pattern matching

`case` / `in` supports value patterns (which use `===`), variable bindings,
captures (`pattern => var`), array patterns, hash patterns, guards, and pinned
variables or expressions. Array patterns call `deconstruct` and `size` and then
index the result with `[]`; hash patterns call `deconstruct_keys` and use the
same `__pat_values` / `__except` helpers as mruby's compiler. Regexp captures in
patterns write the named local variables through `$~`. Unmatched patterns raise
`NoMatchingPatternError` through `MATCHERR` when there is no `else` clause.

### Regular expressions

Regular expression literals compile to `Regexp.compile(source, flags)`, matching
mruby 4.0's own compiler. This makes the bytecode portable, but the `Regexp`
class only exists when the target mruby build links `mruby-regexp`.

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
