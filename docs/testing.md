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

Programs live under `conformance/testdata/conformance/` and are embedded by the `conformance` package. Each case is a directory:

```
conformance/testdata/conformance/basic_puts/
    main.rb
    stdout.txt
    status.txt     # optional, defaults to 0
    stderr.txt     # optional
    args.txt       # optional, one argument per line
```

Tests compile `main.rb` with the default `mrbc` and with `gomrbc`, execute
both `.mrb` files with mruby, and compare the results against the expected
files.

The corpus is intentionally shared with the pure-Go VM project so that both
implementations can be checked against the same behavior.

## Compiling mruby's own test tree

`cmd/compat` compares `go-mrbc` with `mrbc` on a directory of Ruby files. It
reports files that `mrbc` accepts but `go-mrbc` rejects, grouped by reason.

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/compat -fail mruby/test mruby/mrbgems
```

`-fail` makes the command exit non-zero when a compile gap is found. The
current snapshot is recorded in
[compatibility-report.md](compatibility-report.md). The check is compile-only;
runtime behavior is covered by the conformance suite and the runtime harness
below.

## Runtime parity over mruby's test tree

`cmd/runtimecompat` wraps each mruby test file with `test/assert.rb`, compiles
the wrapper with both `mrbc` and `go-mrbc`, runs both binaries, and compares
stdout, stderr, and exit status. Tests that run longer than the timeout are
compared by their partial output; when both implementations time out at the
same point, that counts as a match.

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/runtimecompat -fail mruby/test/t mruby/mrbgems
```

Use `-v` for per-file results and `-json report.json` for a machine-readable
report. `-fail` makes the command exit non-zero on compile or runtime
mismatches. The wrapper supplies `t_print` and appends a deterministic summary
line, so the test files can run under a plain `mruby` binary instead of
`mrbtest`.

For the primary 4.0.0 measurement, point both harnesses at a 4.0.0 worktree:

```sh
git -C mruby worktree add /tmp/mruby-4.0.0 4.0.0
go run ./cmd/compat -fail /tmp/mruby-4.0.0/test /tmp/mruby-4.0.0/mrbgems
go run ./cmd/runtimecompat -fail -assert /tmp/mruby-4.0.0/test/assert.rb \
  /tmp/mruby-4.0.0/test/t /tmp/mruby-4.0.0/mrbgems
```

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

`TestErrorCompatibility` additionally compares the CLI against `mrbc` for a
corpus of invalid programs. It builds `gomrbc`, runs both compilers with `-c`,
and checks that:

- both reject the program;
- both use the same exit status;
- both emit a non-empty diagnostic;
- the `gomrbc` diagnostic names the source file and includes a
  `line:column` location.

The exact wording is not compared, because Prism and mruby's parser produce
different messages.

## Randomized differential testing

`TestRandomizedDifferential` generates small programs within the supported
subset, compiles them with both compilers, runs both binaries on mruby, and
compares stdout, stderr, and exit status. The default is 50 programs with a
fixed seed; `testing.Short()` reduces that to 10, and CI runs 200.

The generator covers integer, bitwise, shift, float, and string expressions;
comparisons and logical operators; arrays, hashes, and ranges; methods with
keyword and splat parameters; blocks and `yield`; `if`/`elsif`/`else`;
`while`/`until`/`for` with `next`/`break`; multiple assignment; pattern
matching; `rescue`/`ensure`; and classes with instance variables. Division and
modulo use a nonzero literal divisor, and loops are bounded, so every
generated program terminates. It deliberately avoids uncaught type errors and
unbounded recursion.

Environment variables:

- `GOMRBC_FUZZ_ITERATIONS`: number of programs
- `GOMRBC_FUZZ_SEED`: random seed

On mismatch the test prints the seed, the generated source, and both results.

## Debug information tests

`mrbc/compile_test.go` compiles a failing program with `Options{DebugInfo: true}`
and checks that mruby reports the original `file:line`. The bytecode tests
round-trip `LVAR` and `DBG` sections and verify that `--remove-lv` suppresses
local variable names.

## Fuzzing

`TestRandomizedDifferential` provides the randomized differential corpus
described above. For longer runs, increase `GOMRBC_FUZZ_ITERATIONS` or vary
`GOMRBC_FUZZ_SEED`. The generator is deliberately restricted to constructs
with defined behavior; it is not a general Ruby fuzzer.

## Continuous integration

`.github/workflows/ci.yml` pins mruby to the **4.0.0 tag**:

- checks out the repository and sets up Go;
- clones and builds mruby 4.0.0 with `rake`;
- runs `go vet ./...` and `go test ./...`;
- runs `cmd/compat -fail` and `cmd/runtimecompat -fail` against the 4.0.0
  test tree.

The two harnesses exit non-zero on any compile gap or runtime mismatch. The
4.0.0 snapshot currently has 153/153 compile matches and 106/106 runtime
matches, so the job is strict rather than advisory.
