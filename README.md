# go-mrbc

A from-scratch Go implementation of mruby's `mrbc` bytecode compiler.

`go-mrbc` parses Ruby with [go-ruby-prism](https://github.com/danielgatis/go-ruby-prism)
and produces **RITE 04.00** bytecode that runs on the official **mruby 4.0.0**
virtual machine. Everything except the parser is implemented in pure Go.

The long-term goal is to use mruby as an embedded macro language for tools
written in Go. The runtime side is
[`go-mrbvm`](https://github.com/onohiroki/go-mrbvm), a pure-Go VM for the same
RITE 04.00 bytecode.

## The two repositories

```text
Ruby source ──▶ go-mrbc ──▶ RITE 04.00 ──▶ go-mrbvm
                                      └──▶ mruby 4.0.0 (reference)
```

- `go-mrbc` parses Ruby, lowers it, and writes bytecode. It has no dependency
  on the VM.
- `go-mrbvm` executes bytecode. It imports only `go-mrbc/bytecode`, the shared
  RITE model and opcode codec.

The conformance corpus lives here and is consumed by both projects. Release
order, version pinning, and the shared contract are described in
[docs/ecosystem.md](docs/ecosystem.md); tagging is described in
[docs/releasing.md](docs/releasing.md).

## Status

This project is under active development. The RITE writer, opcode codec, Prism
lowering, and the current macro-language milestones are implemented and tested
against mruby 4.0.0: scalars, control flow, methods, blocks, classes, modules,
inheritance, `super`, instance/class variables, constants, keyword arguments,
splats, lambdas, multiple assignment, exceptions, pattern matching, debug
information, and `raise`.

Against the **mruby 4.0.0 tag** with the **default `mrbc`** as the reference,
**153 of 153 files compile and 106 of 106 runnable files behave identically**.
Against the local development tree (3,011 commits after 4.0.0), 234 of 237
files compile and 148 of 160 runnable files agree; the remaining output
differences are cases where default `mrbc`'s optimized opcodes cause the
reference binary to fail assertions that `go-mrbc` passes. See
[Compatibility report](docs/compatibility-report.md). CI builds mruby 4.0.0
from the tag and runs both parity harnesses with `-fail`.

| Component | Status |
| --- | --- |
| RITE 04.00 writer / reader | Implemented |
| Opcode encoder / decoder | Implemented |
| Prism AST lowering | Implemented for the current subset |
| Scalar expressions and control flow | Implemented |
| Big integer literals | Implemented |
| Methods, blocks, and closures | Implemented |
| Classes, modules, and inheritance | Implemented |
| Keyword arguments, splats, forwarders | Implemented |
| Multiple assignment | Implemented |
| `begin` / `rescue` / `ensure` / `retry` | Implemented |
| `alias` / `undef` | Implemented |
| Pattern matching (array rest, find, alternation, hash) | Implemented |
| Debug information (`-g`, LVAR/DBG) | Implemented |
| Error compatibility and randomized differential tests | Implemented |
| Regex literals (runtime gem required) | Compiled |
| Peephole optimizer | Not planned initially |

See [Compatibility](docs/compatibility.md) for the exact supported subset and
known limitations.

## Goals

- Compile a practical subset of Ruby to bytecode accepted by mruby 4.0.0.
- Keep the compiler pipeline pure Go. The only external dependency is the
  Prism parser, which is shipped as WebAssembly and runs without cgo.
- Prioritize **behavioral compatibility** with `mrbc` over byte-for-byte
  identical output.
- Keep the bytecode format and opcode definitions reusable by the pure-Go
  `go-mrbvm` runtime.

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

## Install

The module is not yet tagged for `go install`. Build from a checkout for now:

```sh
go build -o gomrbc ./cmd/gomrbc
```

After the first tagged release:

```sh
go install github.com/onohiroki/go-mrbc/cmd/gomrbc@latest
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

Evaluator implementations pass the caller's local names and enable evaluator
semantics explicitly:

```go
irep, err := mrbc.Compile(ctx, source, mrbc.Options{
    FilePath:     "(eval)",
    LocalNames:   callerLocals,
    Eval:         true,
    EvalInMethod: true,
})
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
    -> go-mrbvm or mruby 4.0.0
```

See [docs/architecture.md](docs/architecture.md) for details.

## Documentation

- [Ecosystem and repository relationship](docs/ecosystem.md)
- [Ecosystem and repository relationship (Japanese)](docs/ecosystem.ja.md)
- [Releasing](docs/releasing.md)
- [Architecture](docs/architecture.md)
- [Bytecode format](docs/bytecode-format.md)
- [Compatibility](docs/compatibility.md)
- [Compatibility report](docs/compatibility-report.md)
- [Testing](docs/testing.md)
- [Roadmap](docs/roadmap.md)
- [go-mrbvm plan](docs/go-mrbvm-plan.md)
- [go-mrbvm plan (Japanese)](docs/go-mrbvm-plan.ja.md)
- [Third-party licenses](THIRD_PARTY_LICENSES.md)
- [Implementation plan (Japanese)](docs/implementation-plan.ja.md)
- [Contributing](CONTRIBUTING.md)

## Development

```sh
make build     # build ./gomrbc
make test      # unit tests and conformance tests
make conformance
make compat    # compile-parity check against mruby's test tree
make runtime   # runtime-parity check against mruby's test tree
```

Conformance tests require mruby 4.0.0 and `mrbc` on `PATH`; see
[Testing](docs/testing.md). The corpus itself lives in
`conformance/testdata/conformance/` and is embedded by the `conformance`
package so that `go-mrbvm` runs the same cases.

## License

The original source code and documentation in this repository are licensed
under the **MIT License**; see [LICENSE](LICENSE).

The compiler binary links third-party modules:

- [go-ruby-prism](https://github.com/danielgatis/go-ruby-prism) — MIT
- [Prism](https://github.com/ruby/prism) — MIT, embedded as `prism.wasm`
- [wazero](https://github.com/tetratelabs/wazero) — Apache-2.0

See [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md) and
[third_party/](third_party/) for the notices and full license texts. mruby is
referenced as the bytecode and VM specification; no mruby C code is linked.

`.mrb` files generated from user Ruby programs are not claimed by this
project's MIT License. Their licensing is determined by the input program and
the user's own distribution terms.
