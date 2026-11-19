# Testing

The test strategy is built around the official mruby 4.0.0 implementation as
the oracle. A compiled program is only considered correct if it behaves the
same as the same program compiled by `mrbc`.

## Layers

1. **Encoder tests** verify instruction encoding, extended operands, jump
   offsets, pool entries, symbol tables, and RITE container layout.
2. **Frontend tests** verify that Prism nodes are lowered to the expected IR
   and that unsupported nodes produce located errors.
3. **Conformance tests** compile Ruby programs with both `mrbc` and `gomrbc`,
   run both binaries on mruby 4.0.0, and compare stdout, stderr, and exit
   status.
4. **Disassembly comparisons** are used during development to inspect the
   generated instruction sequence. Exact equality with `mrbc -v` is not
   required because `go-mrbc` does not optimize.

## Locating mruby

Tests look for `mruby` and `mrbc` on `PATH`. When using rbenv, pin the version:

```sh
export RBENV_VERSION=mruby-4.0.0
```

The `toolchain` package centralizes this lookup. Conformance tests are skipped
when the tools are unavailable, but continuous integration requires them.

## Conformance corpus

Programs live under `testdata/conformance/`. Each case is a directory:

```
testdata/conformance/basic_puts/
    main.rb
    stdout.txt
    status.txt     # optional, defaults to 0
    stderr.txt     # optional
    args.txt       # optional, one argument per line
```

Tests compile `main.rb` with `mrbc --no-optimize` and with `gomrbc`, execute
both `.mrb` files with mruby, and compare the results against the expected
files.

The corpus is intentionally shared with the pure-Go VM project so that both
implementations can be checked against the same behavior.

## Compiling mruby's own test tree

`cmd/compat` compares `go-mrbc` with `mrbc` on a directory of Ruby files. It
reports files that `mrbc` accepts but `go-mrbc` rejects, grouped by reason.

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/compat mruby/test mruby/mrbgems
```

The current snapshot is recorded in
[compatibility-report.md](compatibility-report.md). The check is compile-only;
runtime behavior is covered by the conformance suite.

## Running

```sh
go test ./...
go test ./... -run Conformance
make test
```

`make test` runs the full suite. `make conformance` runs only the end-to-end
comparisons.

## Error cases

Invalid or unsupported programs are covered by unit tests in
`mrbc/compile_test.go`. They assert that compilation fails with a message
containing the source location and reason, and that no output file is written
(the compile API returns an error before serialization).

## Debug information tests

`mrbc/compile_test.go` compiles a failing program with `Options{DebugInfo: true}`
and checks that mruby reports the original `file:line`. The bytecode tests
round-trip `LVAR` and `DBG` sections and verify that `--remove-lv` suppresses
local variable names.

## Fuzzing

Once the core compiler is stable, a small generator will produce random
programs within the supported subset. Each generated program is compiled by
both compilers and diffed at runtime. The generator is deliberately restricted
to constructs that have a defined expected behavior; it is not a Ruby fuzzer.

## Continuous integration

CI must:

- build the project with a supported Go version;
- install or build mruby 4.0.0;
- run unit tests and the full conformance suite;
- fail if any unsupported syntax is silently accepted.
