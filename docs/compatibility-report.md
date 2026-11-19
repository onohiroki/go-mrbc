# Compatibility report

This page records snapshots of the compile-time and runtime parity checks
against mruby 4.0.0. Compile parity checks which files both compilers accept.
Runtime parity compiles the same files with both compilers and compares what
the resulting binaries print and return.

The primary measurement uses the **mruby 4.0.0 tag**. A second measurement
uses the local development tree (`4.0.0-3011-gdbab56510`) to track syntax that
is newer than the target release.

## Compile parity: mruby 4.0.0 tag

- Date: 2026-11-19
- Target: mruby 4.0.0 (`831da26b9`)
- Scope: all `*.rb` files under `test/` and `mrbgems/`
- Files: 153

| Result | Count |
| --- | --- |
| Both compilers accept the file | 152 |
| `mrbc` accepts, `go-mrbc` rejects (gap) | 1 |
| `go-mrbc` accepts, `mrbc` rejects (superset) | 0 |
| Both reject | 0 |

The single gap is `test/t/syntax.rb`, which uses the mruby-specific
`def f(&nil)` formal parameter. go-ruby-prism does not parse that syntax.

## Runtime parity: mruby 4.0.0 tag

- Scope: test files under `test/t/` and `mrbgems/*/test/`
- Runnable files: 106
- Harness: `cmd/runtimecompat` with `-assert <4.0.0>/test/assert.rb`

| Result | Count |
| --- | --- |
| Identical stdout, stderr, and exit status | 105 |
| `go-mrbc` cannot compile | 1 |

The compile failure is the same `&nil` file as above. Every file that both
compilers can compile behaves identically.

## Reference: development tree

The local development tree is 3,011 commits after the 4.0.0 tag. It contains
84 additional `.rb` files under `test/` and `mrbgems/`, including tests for
syntax added after the release.

### Compile parity

- Files: 237

| Result | Count |
| --- | --- |
| Both compilers accept the file | 234 |
| `mrbc` accepts, `go-mrbc` rejects (gap) | 0 |
| `go-mrbc` accepts, `mrbc` rejects (superset) | 1 |
| Both reject | 2 |

The superset is `mrbgems/mruby-regexp/test/regexp.rb`, which uses a regexp
literal in a hash pattern. The released 4.0.0 `mrbc` rejects that syntax;
`go-mrbc` accepts it. The two remaining files (`test/t/codegen.rb` and
`test/t/syntax.rb`) use syntax from after 4.0.0 and are rejected by both
compilers for different reasons, so they are not counted as gaps.

### Runtime parity

- Runnable files: 160

| Result | Count |
| --- | --- |
| Identical stdout, stderr, and exit status | 155 |
| Both compilers reject | 3 |
| Different output | 2 |

Four of the 155 identical files time out at the same point under both
compilers; they are compared by their partial output. The two differences are
cases where `go-mrbc` follows Ruby semantics more closely than
`mrbc --no-optimize`:

- `Integer#+` and friends redefined on `Integer` itself: operators are `SEND`
  in `go-mrbc`, so the redefinition is reached. `mrbc` emits `OP_ADD` and the
  VM does not dispatch.
- `next` inside a `rescue` inside a `while` loop in a block: `go-mrbc`
  continues the loop, while the reference binary returns from the block.

The mruby test suite expects the behavior `go-mrbc` produces in both cases.
Reproducing the reference output would mean deliberately reproducing a
limitation, so these are kept as documented differences.

## Reproducing

From the repository root, with the mruby 4.0.0 worktree at `$MRUBY_400`:

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/compat "$MRUBY_400/test" "$MRUBY_400/mrbgems"
go run ./cmd/runtimecompat -assert "$MRUBY_400/test/assert.rb" \
  "$MRUBY_400/test/t" "$MRUBY_400/mrbgems"
```

For the development tree, use `mruby/test` and `mruby/mrbgems` instead.

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
