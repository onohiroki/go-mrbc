# Compatibility report

This page records snapshots of the compile-time and runtime parity checks
against mruby 4.0.0. Compile parity checks which files both compilers accept.
Runtime parity compiles the same files with both compilers and compares what
the resulting binaries print and return.

## Compile parity

- Date: 2026-11-19
- Target: mruby 4.0.0
- Scope: all `*.rb` files under mruby's `test/` and `mrbgems/` directories
- Files: 237

| Result | Count |
| --- | --- |
| Both compilers accept the file | 234 |
| `mrbc` accepts, `go-mrbc` rejects (gap) | 0 |
| `go-mrbc` accepts, `mrbc` rejects (superset) | 0 |
| Both reject | 3 |

The three files rejected by both compilers use syntax that mruby's own parser
does not accept in this configuration. They are not `go-mrbc` gaps.

## Runtime parity

- Scope: test files under `mruby/test/t/` and `mruby/mrbgems/*/test/`
- Runnable files: 160
- Harness: `cmd/runtimecompat`

| Result | Count |
| --- | --- |
| Identical stdout, stderr, and exit status | 155 |
| Both compilers reject | 3 |
| Different output | 2 |

Four of the 155 identical files time out at the same point under both
compilers; they are compared by their partial output.

The two differences are cases where `go-mrbc` follows Ruby semantics more
closely than `mrbc --no-optimize`:

- `Integer#+` and friends redefined on `Integer` itself: operators are `SEND`
  in `go-mrbc`, so the redefinition is reached. `mrbc` emits `OP_ADD` and the
  VM does not dispatch.
- `next` inside a `rescue` inside a `while` loop in a block: `go-mrbc`
  continues the loop, while the reference binary returns from the block.

The mruby test suite expects the behavior `go-mrbc` produces in both cases.
Reproducing the reference output would mean deliberately reproducing a
limitation, so these are kept as documented differences.

The four timeouts (`mruby-bigint`, `mruby-complex`, `mruby-process`, and
`mruby-set`) hang in the same test under both compilers and are compared by
their partial output. The three rejected files are the same ones rejected by
both compilers in the compile-parity check.

## Reproducing

From the repository root:

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/compat mruby/test mruby/mrbgems
go run ./cmd/runtimecompat mruby/test/t mruby/mrbgems
```

Use `-v` for per-file results, or `-json report.json` to write a machine-readable
report.

## Notes

- The runtime harness prepends mruby's `test/assert.rb`, supplies `t_print`,
  and appends a deterministic summary line so the files can run under a plain
  `mruby` binary instead of `mrbtest`.
- Compile parity means that `go-mrbc` understands the same syntax rather than
  producing byte-identical output.
- Runtime feature coverage depends on the mrbgems linked into the target mruby
  build. For example, regular expression literals compile, but executing them
  requires an mruby build with `mruby-regexp`; big integer literals require
  `mruby-bigint`.
