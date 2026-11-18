# go-mrbc

A from-scratch Go implementation of mruby's `mrbc` bytecode compiler.

`go-mrbc` parses Ruby with [go-ruby-prism](https://github.com/danielgatis/go-ruby-prism)
and produces **RITE 04.00** bytecode that runs on the official **mruby 4.0.0**
virtual machine. Everything except the parser is implemented in pure Go.

The long-term goal is to use mruby as an embedded macro language for tools
written in Go. A pure-Go mruby VM is being developed as a separate, parallel
project and is not part of this repository.

## Status

This project is under active development. The RITE writer, opcode codec, Prism
lowering, and the current macro-language milestones are implemented and tested
against mruby 4.0.0: scalars, control flow, methods, blocks, classes, modules,
inheritance, `super`, instance/class variables, constants, keyword arguments,
splats, lambdas, multiple assignment, exceptions, pattern matching, debug
information, and `raise`.

| Component | Status |
| --- | --- |
| RITE 04.00 writer / reader | Implemented |
| Opcode encoder / decoder | Implemented |
| Prism AST lowering | Implemented for the current subset |
| Scalar expressions and control flow | Implemented |
| Methods, blocks, and closures | Implemented |
| Classes, modules, and inheritance | Implemented |
| Keyword arguments, splats, lambdas | Implemented |
| Multiple assignment | Implemented |
| `begin` / `rescue` / `ensure` / `retry` | Implemented |
| `alias` / `undef` | Implemented |
| Pattern matching (`case` / `in`, subset) | Implemented |
| Debug information (`-g`, LVAR/DBG) | Implemented |
| Peephole optimizer | Not planned initially |

See [Compatibility](docs/compatibility.md) for the exact supported subset and
known limitations.

## Goals

- Compile a practical subset of Ruby to bytecode accepted by mruby 4.0.0.
- Keep the compiler pipeline pure Go. The only external dependency is the
  Prism parser, which is shipped as WebAssembly and runs without cgo.
- Prioritize **behavioral compatibility** with `mrbc` over byte-for-byte
  identical output.
- Keep the bytecode format and opcode definitions reusable by a future
  pure-Go mruby VM.

## Non-goals

- Reimplementing the mruby VM in this repository.
- Bytecode compatibility with mruby 3.x (the target is mruby 4.0.0 only).
- Implementing every Ruby feature. Unsupported syntax is rejected with a
  clear, located compile error instead of being silently miscompiled.
- Compile-time `eval` / `load` / `require` support.

## Requirements

- Go 1.24 or newer
- mruby 4.0.0 (`mrbc` and `mruby`) for the differential test suite
  (optional for building the compiler itself)

The test suite looks for `mruby` and `mrbc` on `PATH`. With rbenv:

```sh
export RBENV_VERSION=mruby-4.0.0
```

## Build

```sh
go build -o gomrbc ./cmd/gomrbc
```

## Usage

```sh
# Compile a script to bytecode
./gomrbc -o hello.mrb examples/hello.rb

# Run it with the official mruby VM
RBENV_VERSION=mruby-4.0.0 mruby hello.mrb

# Syntax check only
./gomrbc -c examples/hello.rb

# Show the code generated for a script
./gomrbc -v -o hello.mrb examples/hello.rb
```

```ruby
# examples/hello.rb
def greet(name)
  puts "hello, #{name}"
end

%w[alice bob].each do |name|
  greet(name)
end
```

## CLI

```
gomrbc [options] file.rb

  -o FILE        write bytecode to FILE (default: <source>.mrb)
  -c             check syntax only
  -g, --debug    emit debug information (source lines)
  -v, --verbose  print the generated instruction sequence
  --dump-ast     print the Prism AST as JSON
  --dump-ir      print the lowered intermediate representation
  --remove-lv    omit local variable information
  --version      print version information
```

## Library

The compiler is also available as a Go package.

```go
import (
    "context"

    "github.com/onohiroki/go-mrbc/mrbc"
)

irep, err := mrbc.Compile(ctx, source, mrbc.Options{FilePath: "macro.rb"})
if err != nil {
    // Compile errors carry file/line/column information.
}

binary, err := mrbc.Dump(irep)
```

The `bytecode` package contains the shared RITE format model, opcode
definitions, and encoder/decoder. It is intentionally independent of the
parser and compiler so that other tools, including a pure-Go VM, can import
it directly.

## Architecture

```
Ruby source
    -> go-ruby-prism (WebAssembly)
    -> Prism AST
    -> frontend lowering
    -> internal IR
    -> compiler (scopes, registers, bytecode)
    -> bytecode.Irep
    -> RITE writer
    -> .mrb
    -> mruby 4.0.0 VM
```

See [docs/architecture.md](docs/architecture.md) for details.

## Documentation

- [Architecture](docs/architecture.md)
- [Bytecode format](docs/bytecode-format.md)
- [Compatibility](docs/compatibility.md)
- [Testing](docs/testing.md)
- [Roadmap](docs/roadmap.md)
- [Implementation plan (Japanese)](docs/implementation-plan.ja.md)

## Development

```sh
make build     # build ./gomrbc
make test      # unit tests and conformance tests
make conformance
```

Conformance tests require mruby 4.0.0 and `mrbc` on `PATH`; see
[Testing](docs/testing.md).

## License

MIT. See [LICENSE](LICENSE). mruby, Prism, and go-ruby-prism are distributed
under their own MIT licenses; see [NOTICE](NOTICE).
