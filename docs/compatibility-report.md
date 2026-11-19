# Compatibility report

This page records a snapshot of the compiler parity check against mruby 4.0.0.
The check is compile-only: each Ruby file is compiled by both `mrbc` and
`go-mrbc`, and the results are compared. Runtime behavior is covered separately
by the conformance suite.

## Snapshot

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

## Reproducing

From the repository root:

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/compat mruby/test mruby/mrbgems
```

Use `-v` for per-file results, or `-json report.json` to write a machine-readable
report.

## Notes

- The test files are part of mruby's own test framework and are not executed by
  this check; they require `mrbtest`, assertions, and sometimes optional
  mrbgems.
- Compile parity means that `go-mrbc` understands the same syntax rather than
  producing byte-identical output.
- Runtime feature coverage depends on the mrbgems linked into the target mruby
  build. For example, regular expression literals compile, but executing them
  requires an mruby build with `mruby-regexp`.
