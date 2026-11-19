# Compatibility

## Target

- Bytecode format: **RITE 04.00**
- VM: **mruby 4.0.0**
- Parser: Prism (Ruby 3.3/3.4 syntax) through go-ruby-prism
- Reference compiler: default `mrbc` from mruby 4.0.0

The acceptance criterion is **behavioral compatibility**: a program compiled by
`go-mrbc` must produce the same observable behavior as the same program
compiled by `mrbc` when both run on mruby 4.0.0. Output, raised errors, and
exit status are compared.

Byte-for-byte identical `.mrb` output is explicitly not a goal. `go-mrbc` does
not run a peephole optimizer and compiles many operators as ordinary method
calls, so its bytecode is larger than `mrbc`'s. Default `mrbc` emits optimized
opcodes. In the rare case where a core operator such as `Integer#+` is
redefined, the `SEND` form used by `go-mrbc` reaches the redefinition while the
optimized opcode may not. Such differences are documented as intentional.

## Supported today

The following table describes the implementation target of the current
milestones. Features are enabled as their compiler support lands.

| Feature | Status |
| --- | --- |
| `nil`, `true`, `false` | Supported |
| Integers (32/64-bit and big integers), `Rational` and `Imaginary` literals | Supported |
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
| Constants (writes, reads, `A::B` reads/writes, and operator assignment) | Supported |
| Operator assignment (locals, globals, ivars, cvars, attributes, index) | Supported |
| `if` / `unless` / `elsif` / `while` / `until` (including `begin ... end` forms) | Supported |
| `case` / `when` (including `when *values`) | Supported |
| `&&`, `||`, `!` | Supported |
| Method calls, positional arguments, safe navigation (`&.`) | Supported |
| Calls with more than 14 positional arguments | Supported (packed array) |
| Braceless hashes in argument lists | Supported (keyword hash semantics) |
| `def` with required, optional, post, rest, and keyword parameters | Supported |
| mruby `&nil` formal parameter (rejects blocks) | Supported |
| Destructured parameters (`def f((a, b))`) | Supported |
| Keyword arguments, splats, and their combinations | Supported |
| Argument forwarding (`...`) and anonymous block forwarding (`&`) | Supported |
| Numbered block parameters (`_1`, `_2`, ...) | Supported |
| Splat arguments and block arguments (`&blk`) | Supported |
| Blocks and `yield` (including splatted and keyword arguments) | Supported |
| `break` / `next` (loops, blocks, lambdas) | Supported |
| `redo` in loops, blocks, and rescue clauses | Supported |
| `return` with a single value or multiple values | Supported |
| `raise` | Supported |
| Rightward pattern matching (`value => pattern`) | Supported |
| Pattern-matching predicate (`value in pattern`) | Supported extension |
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
| Pattern matching (value, variable, capture, array, array rest, find, alternation, hash with `**rest`, guard, pin) | Supported |

Everything listed as supported is exercised by the conformance suite. The
table is kept in sync with the implementation rather than the ambition.

## Known limitations

The current subset is intentionally conservative:

- `yield` is supported in a method body and in blocks nested inside that
  method. Yielding from class bodies or lambdas is rejected.
- `super` is supported in methods and blocks, including top-level blocks
  (where it raises at runtime), but not inside a lambda defined in a method.
- The `it` implicit block parameter is rejected; numbered parameters (`_1`)
  are supported. mruby 4.0's own compiler lowers `it` to a method call that is
  undefined at runtime, so accepting it would not make the program work.
- `alias` is limited to method names; global-variable aliases are rejected.
  mruby 4.0's own parser rejects global-variable aliases as well.
- `case/in` without a subject expression is rejected. mruby 4.0's parser does
  not accept it either.
- Dynamic constant names (`A.const_set(name, x)`) are rejected; constant paths
  must be literal when they are assignment targets.
- Regular expression literals compile, but executing them requires an mruby
  build with the `mruby-regexp` mrbgem.
- Big integer literals compile, but running them requires an mruby build with
  the `mruby-bigint` mrbgem.

These restrictions are checked during compilation and produce
`file:line:column` errors.

## Unsupported syntax

Unsupported constructs produce a compile-time error with a file, line, and
column. They are never silently ignored.

Current explicit rejections include:

- the `it` implicit block parameter
- dynamic constant-path operator assignment (`expr::CONST += 1`)
- `BEGIN` / `END` blocks
- `eval`, `load`, and `require` (host responsibilities)
- encoding and magic comments
- fibers, threads, refinements, and `TracePoint`
- `super` inside a lambda defined in a method

## Runtime parity

`cmd/runtimecompat` runs mruby's own test files through both compilers, using
the default `mrbc` as the reference. The primary measurement uses the **mruby
4.0.0 tag**. The 2026-11-20 snapshot covers 106 runnable files:

| Result | Count |
| --- | --- |
| Identical stdout/stderr/exit status | 106 |

For reference, the same check against the local mruby development tree
(`4.0.0-3011-gdbab56510`, 160 runnable files) reports 147 matches (including 3
that time out at the same point), 9 output differences, 1 file the reference
cannot compile, 2 files neither compiler can compile, and 1 one-sided timeout.

All 9 output differences are cases where the reference binary fails assertions
that `go-mrbc` passes. They come from optimized opcodes in default `mrbc` and
from tests that redefine core operators or inspect optimized behavior. The
mruby test suite itself expects the behavior `go-mrbc` produces. Examples:

- `Integer#+` and `Float#+` redefined: `go-mrbc` uses `SEND`, so the
  redefinition is reached; optimized `OP_ADD` may bypass it.
- `next` inside `rescue` inside a `while` in a block: `go-mrbc` continues the
  loop; the reference binary returns from the block instead.

The development-tree results are tracked separately because those tests
include syntax added after the 4.0.0 release.

## Semantic notes

### Operators

Arithmetic, comparison, indexing, and unary operators are compiled as method
calls. For example, `a + b` becomes a `SEND` of `:+`. This matches Ruby's
method-dispatch semantics and avoids relying on specialized opcodes before
their behavior is verified. Optimized opcodes may be added later.

### Class and module expressions

A `class`, `module`, or `class << obj` expression evaluates to the value of its
body, matching mruby's `EXEC` result. An empty body evaluates to `nil`.
`class ::C` defines the class at the top level by using `OCLASS` as its outer
scope.

### Constant assignment

Writes through a constant path (`A::X = v`, `A::X += v`, `A::X ||= v`) are
supported when the path is built from literal constants. mruby's `mrbc`
rejects ordinary constant reassignment at compile time; `go-mrbc` does not
enforce that policy, so this is a deliberate superset.

### mruby `&nil`

`def f(&nil)` is mruby-specific syntax that Prism does not accept. The
compiler rewrites the four-byte `&nil` sequence to a same-length sentinel
before parsing and marks the parameter as a no-block parameter. The generated
`ENTER` sets mruby's `noblock` aspec bit, so the VM raises `ArgumentError` when
a block is passed, matching mruby.

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
captures (`pattern => var`), array patterns (including rest and posts),
find patterns, alternation patterns, hash patterns, guards, and pinned
variables or expressions. Array patterns call `deconstruct` and `size` and then
index the result with `[]`; hash patterns call `deconstruct_keys` and use the
same `__pat_values` / `__except` helpers as mruby's compiler. Find patterns
search for the required subsequence and bind the left and right splats.
Regexp captures in patterns write the named local variables through `$~`.
Unmatched patterns raise `NoMatchingPatternError` through `MATCHERR` when there
is no `else` clause.

The rightward form (`value => pattern`) returns the value and raises on a
mismatch. The predicate form (`value in pattern`) returns a boolean and is
compiled as an extension; mruby 4.0's parser rejects that form, so it is not
part of the reference-compatible subset.

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
