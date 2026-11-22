# Compatibility report

This page records snapshots of the compile-time and runtime parity checks
against mruby 4.0.0. Compile parity checks which files both compilers accept.
Runtime parity compiles the same files with both compilers and compares what
the resulting binaries print and return.

The primary measurement uses the **mruby 4.0.0 tag** and the **default
`mrbc`** as the reference compiler. A second measurement uses the local
development tree (`4.0.0-3011-gdbab56510`) to track syntax and behavior that is
newer than the target release.

## Compile parity: mruby 4.0.0 tag

- Date: 2026-11-20
- Target: mruby 4.0.0 (`831da26b9`)
- Scope: all `*.rb` files under `test/` and `mrbgems/`
- Files: 153

| Result | Count |
| --- | --- |
| Both compilers accept the file | 153 |
| `mrbc` accepts, `go-mrbc` rejects (gap) | 0 |
| `go-mrbc` accepts, `mrbc` rejects (superset) | 0 |
| Both reject | 0 |

## Runtime parity: mruby 4.0.0 tag

- Scope: test files under `test/t/` and `mrbgems/*/test/`
- Runnable files: 106
- Harness: `cmd/runtimecompat` with `-assert <4.0.0>/test/assert.rb`

| Result | Count |
| --- | --- |
| Identical stdout, stderr, and exit status | 106 |
| Different output | 0 |

Every runnable file compiles and behaves identically to the default `mrbc`
reference.

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
| Identical stdout, stderr, and exit status | 148 |
| Different output | 9 |
| Reference cannot compile | 1 |
| Both compilers cannot compile | 2 |

Three of the 148 identical files time out at the same point under both
compilers; they are compared by their partial output.

All 9 output differences are cases where the reference binary fails assertions
that `go-mrbc` passes. They come from optimized opcodes in default `mrbc` and
from tests that redefine core operators or inspect optimized behavior:

- `Integer#+`, `Float#+`, and similar operators redefined: `go-mrbc` uses
  `SEND`, so the redefinition is reached; optimized opcodes may bypass it.
- `next` inside a `rescue` inside a `while` in a block: `go-mrbc` continues
  the loop, while the reference binary returns from the block.
- A few tests that assert optimized-opcode or introspection behavior.

The mruby test suite expects the behavior `go-mrbc` produces in these cases.
Reproducing the reference output would mean deliberately reproducing a
limitation of optimized opcodes, so these are documented rather than chased.

## Reproducing

From the repository root, with the mruby 4.0.0 worktree at `$MRUBY_400`:

```sh
export RBENV_VERSION=mruby-4.0.0
go run ./cmd/compat -fail "$MRUBY_400/test" "$MRUBY_400/mrbgems"
go run ./cmd/runtimecompat -fail -assert "$MRUBY_400/test/assert.rb" \
  "$MRUBY_400/test/t" "$MRUBY_400/mrbgems"
```

For the development tree, use `mruby/test` and `mruby/mrbgems` instead.

Use `-v` for per-file results, or `-json report.json` to write a machine-readable
report. `-fail` makes the harness exit non-zero when a gap or runtime
difference is found.

## Notes

- The conformance corpus is embedded by the `conformance` package and also
  consumed by `go-mrbvm`, so compiler and VM run identical cases.
- The runtime harness prepends mruby's `test/assert.rb`, supplies `t_print`,
  and appends a deterministic summary line so the files can run under a plain
  `mruby` binary instead of `mrbtest`.
- Compile parity means that `go-mrbc` understands the same syntax rather than
  producing byte-identical output.
- Runtime feature coverage depends on the mrbgems linked into the target mruby
  build. For example, regular expression literals compile, but executing them
  requires an mruby build with `mruby-regexp`; big integer literals require
  `mruby-bigint`.
- The error-compatibility test compares exit status and the presence of a
  located diagnostic, not the exact wording: Prism and mruby's parser produce
  different messages.
- The reference 4.0.0 VM answers `Float::NAN == Float::NAN` with `true`
  because its `OP_EQ` identity shortcut runs before the NaN check. The
  development tree and CRuby answer `false`; go-mrbvm follows them. Shared
  conformance cases therefore avoid asserting NaN self-equality.
- `TestRandomizedDifferential` generates programs within the supported subset
  and compares both compiled binaries at runtime. It covers arithmetic
  (including bitwise and shift operators), floats, strings, arrays, hashes,
  ranges, control flow, blocks, multiple assignment, pattern matching,
  `rescue`/`ensure`, and classes. A fixed seed is used by default; CI runs 200
  programs.
