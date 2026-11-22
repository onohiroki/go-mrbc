# 演算子 opcode の実装計画

最終更新: 2026-11-22  
対象: `go-mrbc`（基準コミット `1951b5d`）  
関連: [implementation-plan.ja.md](implementation-plan.ja.md)

## 1. 目的

`go-mrbc` は以前、`+` や `<` などの演算子をすべて `SEND` で出力していた。
mruby 4.0.0 の公式 `mrbc` は `OP_ADD` / `OP_ADDI` / `OP_ADDILV` / `OP_LT`
などの専用命令を出力しており、これが go-mrbvm と C mruby の性能差の
主要因になっていた。

go-mrbvm 側は専用 opcode をすでに高速実装しているため、go-mrbc が公式と
同じ opcode を出せば、そのまま性能差が縮まる。この文書は、その
仕様・手順・検証方法と実装結果をまとめたものである。Phase 1〜5 は
実装済み（§8 参照）。

### ゴール（すべて達成）

- `x + 1` が `OP_ADDI`
- `x + y` が `OP_ADD`
- `x += 1` が `OP_ADDILV`
- `x < y` が `OP_LT`
- `a[i]` / `a[i] = v` が `OP_GETIDX` / `OP_SETIDX`
- 公式 `mrbc` と go-mrbc の命令数がほぼ一致する

### 期待効果（go-mrbvm での実測、2026-11-22）

| ワークロード | 公式 mrbc | go-mrbc（現状） | 目標 |
| --- | ---: | ---: | ---: |
| 加算ループ | 1.15x | 2.11x | 1.2x 前後 |
| fib(22) | 1.59x | 1.84x | 1.6x 前後 |
| mixed.rb | 約 1.04x | 約 1.33x | 1.05〜1.1x |

数値は「go-mrbvm の時間 ÷ C mruby の時間」。1.0 が同等。

---

## 2. 実測した差分

### 2.1 `x += 1` / `x - 2`（メソッド内ローカル）

```ruby
def bump(n)
  x = n
  x += 1
  x - 2
end
```

公式 `mrbc`（`mruby -v -v`）:

```text
000 ENTER        1:0:0:0:0:0:0:0
004 MOVE         R3 R1          ; x = n
007 ADDILV       R3 R4 1        ; x += 1
011 MOVE         R4 R3
014 SUBI         R4 2           ; x - 2
017 RETURN       R4
```

go-mrbc（`gomrbc -v`）:

```text
0000 ENTER
0004 MOVE         6 1
0007 MOVE         3 6
0010 MOVE         5 6
0013 MOVE         7 3
0016 LOADI_1      8
0018 SEND         R7 :+ n=1
0022 MOVE         6 7
0025 MOVE         3 6
0028 MOVE         5 6
0031 MOVE         5 3
0034 LOADI_2      6
0036 SEND         R5 :- n=1
0040 MOVE         4 5
0043 RETURN       4
```

公式 8 命令 / go-mrbc 17 命令。

### 2.2 比較と乗算

```ruby
if a < b
  p a * 3
end
```

| 公式 | go-mrbc |
| --- | --- |
| `MOVE, MOVE, LT` | `MOVE, MOVE, SEND :<` |
| `MOVE, LOADI_3, MUL` | `MOVE, LOADI_3, SEND :*` |

### 2.3 インデックス操作

```ruby
a[0] += 5
h[:x] += 1
```

公式は `GETIDX0` / `GETIDX` / `ADDI` / `SETIDX` を使う。go-mrbc は
`SEND :[]` / `SEND :+` / `SEND :[]=` で、15 命令 vs 25 命令。

---

## 3. 公式 mruby 4.0 の生成規則

参照実装はリポジトリ内の `mruby/` にある。

### 3.1 opcode の意味

`mruby/include/mruby/ops.h`:

```text
OPCODE(ADD,    B)    /* R[a] = R[a]+R[a+1] */
OPCODE(ADDI,   BB)   /* R[a] = R[a]+mrb_int(b) */
OPCODE(SUB,    B)    /* R[a] = R[a]-R[a+1] */
OPCODE(SUBI,   BB)   /* R[a] = R[a]-mrb_int(b) */
OPCODE(ADDILV, BBB)  /* R[a] = R[a]+mrb_int(c); R[b],R[b+1] for method call */
OPCODE(SUBILV, BBB)  /* R[a] = R[a]-mrb_int(c); R[b],R[b+1] for method call */
OPCODE(MUL,    B)    /* R[a] = R[a]*R[a+1] */
OPCODE(DIV,    B)    /* R[a] = R[a]/R[a+1] */
OPCODE(EQ,     B)
OPCODE(LT,     B)
OPCODE(LE,     B)
OPCODE(GT,     B)
OPCODE(GE,     B)
```

- `a` がレシーバかつ結果、`a+1` が引数
- `ADDI` / `SUBI` の即値は **8bit unsigned（0〜255）**
- `ADDILV` / `SUBILV` は `a` がローカル、`b` がフォールバック用作業レジスタ、
  `c` が即値
- レジスタ operand は 8bit。`base > 255` のときは SEND にフォールバックする

### 3.2 生成箇所

`mruby/mrbgems/mruby-compiler/src/codegen.c`:

| 行（目安） | 内容 |
| --- | --- |
| 2845 付近 | `NODE_CALL` で `+ - * / < <= > >= ==` を専用 opcode に変換 |
| 1355 付近 | `gen_addsub`。直前の整数リテラルを `ADDI`/`SUBI` に畳み込む |
| 1033 付近 | `MOVE temp, local; ADDI temp, imm; MOVE local, temp` を `ADDILV`/`SUBILV` に融合 |
| 960〜1050 | `gen_move` の MOVE peephole |

`mruby/src/vm.c`:

- `OP_MATHI` / `OP_MATHILV` / `OP_CMP` は Integer / Float の高速パスを持ち、
  非整数・演算子再定義時は `L_SEND_SYM` に合流する
- つまりコンパイラは「とりあえず専用 opcode を出し、VM がフォールバックする」
  設計でよい

---

## 4. 実装計画

以下は実装時の計画である。Phase 1〜5 は実装済みで、結果は §8 にまとめた。

### 対象ファイル

- `internal/compiler/expr.go`
  - `compileCall`（1378 行付近）
  - `compileCallPrepared`（1397 行付近）
  - `case *frontend.LocalWrite:`（64 行付近）
- `internal/compiler/scope.go`
  - `emit`（277 行付近）、`emitMove`（281 行付近）、`alloc`（261 行付近）
- `internal/compiler/asm.go`
  - `emit`（37 行付近、バイト列に追記）
- `internal/frontend/lower.go`
  - `LocalVariableOperatorWriteNode`（282 行付近）
  - `x += 1` は `LocalWrite(Call(LocalRead(x), "+", IntegerLiteral))` に
    lower 済み
- `bytecode/opcodes_gen.go`
  - `OpADD` / `OpADDI` / `OpADDILV` は定義済み。使っていないだけ

※ 行番号は基準コミット時点のもの。関数名を優先して探すこと。

---

### Phase 1: 演算子 SEND → 専用 opcode

`compileCallPrepared` の `SEND` emit の直前で分岐する。

**条件（すべて満たすときだけ）**

- `!e.ImplicitSelf()`（明示レシーバ）
- `len(e.Args) == 1`
- `len(e.Keywords) == 0`
- `e.Block == nil && e.BlockArg == nil`
- positional splat なし（`packedArgs == false`）
- `e.Name` が `+ - * / == < <= > >=` のいずれか
- `base <= 255`

**マッピング**

| Ruby | opcode |
| --- | --- |
| `+` | `OpADD` |
| `-` | `OpSUB` |
| `*` | `OpMUL` |
| `/` | `OpDIV` |
| `==` | `OpEQ` |
| `<` | `OpLT` |
| `<=` | `OpLE` |
| `>` | `OpGT` |
| `>=` | `OpGE` |

**コードイメージ**

```go
// base はレシーバ、base+1 が引数
if op, ok := arithOpcode(e.Name); ok && canUseArithOp(e, n, packedArgs, hasBlock) {
    if op == bytecode.OpADD || op == bytecode.OpSUB {
        if lit, ok := integerLiteral(e.Args[0]); ok && lit >= 0 && lit <= 255 {
            imm := bytecode.OpADDI
            if op == bytecode.OpSUB {
                imm = bytecode.OpSUBI
            }
            // リテラルは副作用がないのでロード自体を省略してよい
            s.emit(imm, uint32(base), uint32(lit))
            s.emitMove(dst, base)
            return
        }
    }
    s.emit(op, uint32(base))
    s.emitMove(dst, base)
    return
}
```

- 引数のコンパイルは現状どおり `base+1` に行う
- `ADDI` / `SUBI` のときだけリテラルのロードを省略する
- **負値・256 以上は畳み込まない**

---

### Phase 2: ローカル複合代入 → `ADDILV` / `SUBILV`

`expr.go` の `case *frontend.LocalWrite:` の先頭で、lower 済みの形を
認識する。

**条件**

- `s.effectiveDepth(e.Depth) == 0`
- `e.Value` が `*frontend.Call`
- `call.Receiver` が `*frontend.LocalRead` で同じ名前、`depth == 0`
- `call.Name` が `"+"` または `"-"`
- `call.Args` が 1 個の `*frontend.IntegerLiteral` で 0〜255
- block / block arg / keywords / splat なし

**コードイメージ**

```go
case *frontend.LocalWrite:
    if add, ok := e.Value.(*frontend.Call); ok && s.effectiveDepth(e.Depth) == 0 {
        if read, ok := add.Receiver.(*frontend.LocalRead); ok &&
            read.Name == e.Name && s.effectiveDepth(read.Depth) == 0 &&
            add.Block == nil && add.BlockArg == nil &&
            len(add.Keywords) == 0 && len(add.Args) == 1 {
            if lit, ok := add.Args[0].(*frontend.IntegerLiteral); ok &&
                lit.Value >= 0 && lit.Value <= 255 &&
                (add.Name == "+" || add.Name == "-") {
                slot := s.locals[e.Name]
                temp := s.alloc(1) // フォールバック用作業レジスタ
                op := bytecode.OpADDILV
                if add.Name == "-" {
                    op = bytecode.OpSUBILV
                }
                s.emit(op, uint32(slot), uint32(temp), uint32(lit.Value))
                s.emitMove(dst, slot)
                return
            }
        }
    }
    // 既存処理
```

`temp` は go-mrbvm 側では未使用だが、公式と同じ operand を埋める。

---

### Phase 3: MOVE peephole

Phase 1〜2 だけでも命令数は大きく減るが、公式との残差は `MOVE`。

公式 `gen_move` の主な規則:

1. `MOVE dst, src` で `dst == src` → 削除（`emitMove` は実装済み）
2. 直前が `MOVE a, src` で `a == src` → 宛先を `dst` に付け替え
3. 直前が定数ロードで `a == src` かつ `a` が一時レジスタ →
   ロード先を `dst` に付け替え
   - 対象: `LOADNIL` / `LOADSELF` / `TRUE` / `FALSE` / `LOADI_*` /
     `LOADI8/16/32` / `LOADL` / `LOADSYM` / `GETGV` / `GETIV` /
     `GETCONST` / `STRING` / `LAMBDA` / `BLOCK` など
4. 直前が `ARRAY` で条件を満たす → `ARRAY` / `ARRAY2` の宛先を付け替え

**実装方法は 2 案**

- **A. バイト列 peephole（公式に忠実）**
  - `scope.emit` で直前命令の開始オフセットを記録
  - `rewindLast()` で `asm.code` を切り戻して再 emit
  - ラベルが直前命令に張られている場合は無効化
  - プール・シンボルは残っても意味的に無害
- **B. AST レベルで宛先直接コンパイル（安全・簡易）**
  - `LocalWrite` で値がリテラルや `LocalRead` のとき、一時レジスタを
    経由せず `slot` に直接コンパイル
  - 例: `x = 0` → `LOADI_0 slot`
  - まずは B を推奨

---

### Phase 4（任意）: インデックス操作

- `a[i]` → `GETIDX` / `GETIDX0`
- `a[i] = v` → `SETIDX`
- 配列リテラル → `ARRAY` / `ARRAY2`
- `*a` → `ARYCAT` / `ARYPUSH` / `ARYSPLAT`

go-mrbvm は `OpGETIDX` / `OpGETIDX0` / `OpSETIDX` / `OpAREF` / `OpASET`
を実装済み。Phase 1〜3 の効果を測ってからでよい。

### Phase 5（任意）: `SEND0` / `SSEND0`

引数 0 個・キーワード 0 個・ブロックなしの send は、公式は
`SEND0` / `SSEND0` を使う。go-mrbvm も実装済み。

---

## 5. 落とし穴チェックリスト

| 項目 | 注意 |
| --- | --- |
| 非整数レシーバ | opcode は VM が SEND にフォールバックする。型判定は不要 |
| ユーザー定義演算子 | 公式 VM は `bop_redefined` で判定。go-mrbvm は整数高速パスを優先するが、C mruby 4.0.0 でも同じ結果になることを確認済み |
| 即値の範囲 | `ADDI` / `SUBI` / `ADDILV` / `SUBILV` は 0〜255 の非負のみ |
| レジスタ番号 | `OP_ADD` 系は 8bit operand。`base > 255` は SEND のまま |
| block / keyword / splat | 1 つでもあれば SEND のまま |
| 暗黙 self | `e.ImplicitSelf()` は対象外 |
| setter | `foo=` は対象外。`==` / `<=` / `>=` と混同しない |
| 評価順序 | Phase 2 は同じローカルへの読み書き + リテラルに限定。`x += f()` は融合しない |
| ラベル境界 | Phase 3-A で rewind する場合、直前にラベルがあれば最適化しない |
| 行情報 | `markLine` は既存のまま。リテラルのロード省略で行エントリは壊れない |
| 除算 | `OP_DIV` の 0 除算は VM が例外にする |
| オーバーフロー | 公式は bigint 昇格、go-mrbvm は現状 int64 でラップ（既存挙動）。SEND 経路と同じなので悪化しない |

---

## 6. go-mrbvm 側の受け入れ状況

以下は実装済み・高速化済み。追加実装は不要。

- `OpADD` / `OpSUB` / `OpMUL` / `OpDIV` / `OpEQ` / `OpLT` / `OpLE` /
  `OpGT` / `OpGE`
  - 整数同士は命令内で直接計算
  - 非整数・0 除算・ユーザー定義は `s.call` にフォールバック
- `OpADDI` / `OpSUBI`
- `OpADDILV` / `OpSUBILV`（`b` operand は現状未使用）
- `OpGETIDX` / `OpGETIDX0` / `OpSETIDX` / `OpAREF` / `OpASET`
- ジャンプ先・演算子 ID のデコード時事前計算
- バックトレース行は「呼び出し時・例外時」だけ計算

---

## 7. 検証手順

### 7.1 go-mrbc 側

```sh
cd /Users/hiroki/MyProjects/go-mrbc
go test ./...
```

- `mrbc/conformance_test.go`: 公式 `mrbc` と go-mrbc の両方でコンパイルし、
  `mruby` で実行して stdout 一致
- `mrbc/compile_test.go:TestCompileRunsOnMruby`

### 7.2 opcode の存在テスト

```go
func TestOperatorOpcodes(t *testing.T) {
    // コンパイル結果を bytecode.Decode し、以下の opcode を含むことを確認
    // x += 1 → OpADDILV
    // x + 1  → OpADDI
    // x + y  → OpADD
    // x < y  → OpLT
    // x * 3  → OpMUL
}
```

命令数の回帰テストも追加する。

```ruby
def bump(n)
  x = n
  x += 1
  x - 2
end
```

公式は 8 命令前後。go-mrbc が 17 → 10 前後になれば成功。

### 7.3 ディスアセンブル比較

```sh
# 公式
mruby -v -v /tmp/sample.rb

# go-mrbc
../go-mrbc/gomrbc -v -o /dev/null /tmp/sample.rb
```

### 7.4 意味論テスト

- `x += 1` / `x -= 1`
  - メソッド内、引数、ブロック内、クロージャ内
- `a + 1` / `a - 1` / `a + b` / `a < b` / `a == b`
- Float、BigInt、String、配列、ユーザー定義 `+`
- `1 / 0` の例外
- `obj.value += 1`（メソッド経由。融合しないこと）

### 7.5 go-mrbvm での統合確認

```sh
cd /Users/hiroki/MyProjects/go-mrbvm

go test ./...
go build -o bin/vmcompat ./cmd/vmcompat
./bin/vmcompat -corpus

go build -o bin/mrbtest ./cmd/mrbtest
./bin/mrbtest -manifest testdata/official.txt

# go-mrbc バイトコード
./bin/vmbench -n 20 -compare benchmark/mixed.rb

# 公式 mrbc バイトコード（基準）
./bin/vmbench -n 20 -mrbc -compare benchmark/mixed.rb
```

`bin/vmbench` が無い場合は `go build -o bin/vmbench ./cmd/vmbench` で作成する。

---

## 8. 作業チェックリスト

新しいセッションでは、まずこのファイルと
[implementation-plan.ja.md](implementation-plan.ja.md) の §6.3 / §8 を読む。

- [x] Phase 1: 演算子 → `OP_ADD` / `OP_SUB` / `OP_MUL` / `OP_DIV` /
      `OP_EQ` / `OP_LT` / `OP_LE` / `OP_GT` / `OP_GE`
  - [x] `compileCallPrepared` に条件分岐を追加
  - [x] opcode 存在テスト
  - [x] `go test ./...`
  - [x] `vmbench` で加算ループ・mixed を計測
- [x] Phase 2: 即値 → `OP_ADDI` / `OP_SUBI`
  - [x] 0〜255 の制限を守る
  - [x] 負値・256 以上が SEND のままであることをテスト
- [x] Phase 3: `x += 1` → `OP_ADDILV` / `OP_SUBILV`
  - [x] クロージャ内・引数・ブロック内のテスト
  - [x] `obj.x += 1` が融合しないことをテスト
- [x] Phase 4: MOVE peephole（任意）
  - [x] まず AST レベルの宛先直接コンパイル
        （単純な代入、文の結果破棄、call の dst 再利用）
  - [x] バイト列 rewind は不要だったため未実装
- [x] Phase 5: `GETIDX` / `SETIDX` / `SEND0`（任意）
  - [x] `GETIDX0`（`a[0]`）、`SEND0` / `SSEND0` も実装
  - [x] `a[i] = v` の式の値が代入値になることを修正

### 実装結果（2026-11-22 実測）

| 項目 | 変更前 | 変更後 | 公式 mrbc（同時計測） |
| --- | ---: | ---: | ---: |
| `bump` のメソッド irep | 17 命令 | 6 命令 | 6 命令 |
| 加算ループ（200,000 回） | 2.06x | 1.1〜1.3x | 1.2x 前後 |
| mixed.rb | 1.34x | 1.09〜1.12x | 1.10〜1.14x |

`vmbench` の比率は C mruby のプロセス起動やマシン負荷で大きく振れる。
同じ実行内で公式 mrbc のバイトコードと交互に比べると、go-mrbvm の
実行時間は次のとおりで、公式と同等かわずかに速い。

- 加算ループ 150 iterations: go-mrbc 1.046〜1.078s / 公式 mrbc 1.106〜1.189s
- mixed.rb 20 iterations: go-mrbc 627〜643ms / 公式 mrbc 637〜663ms

`fib(22)` の `irep` は公式と同じ命令列（`SUBI` + `SSEND` + `ADD`）になり、
加算ループも 1 反復あたりの命令数は公式と一致した。残る差はトップレベル
の `LOADNIL` / `RETURN` とレジスタ番号程度である。

生成される opcode:

- `+ - * / == < <= > >=` → `OP_ADD` 〜 `OP_GE`
- `x + 1` / `x - 2`（0〜255 の整数リテラル）→ `OP_ADDI` / `OP_SUBI`
  - 負の即値は畳み込まない。参照 `mrbc` 4.0.0 は `q + -5` を `SUBI 5` に
    畳んで `-` を呼ぶ #2557 の不具合があるが、go-mrbc は CRuby と同じく
    `+` に -5 を渡す（`mrbc/reference_bug_test.go`）
- `x += 1` / `x -= 1`（ローカル）→ `OP_ADDILV` / `OP_SUBILV`
- `a[i]` / `a[0]` / `a[i] = v` → `OP_GETIDX` / `OP_GETIDX0` / `OP_SETIDX`
- 引数なし呼び出し → `OP_SEND0` / `OP_SSEND0`

テストは `mrbc/operator_opcode_test.go`（opcode の存在・非融合条件・命令数）と
`conformance/testdata/conformance/operator_opcodes/`（C mruby での実行結果一致）に追加した。

### 推奨コミット粒度

```text
compiler: emit OP_ADD and friends for one-argument operators
compiler: fold integer immediates into OP_ADDI/OP_SUBI
compiler: fuse local +=/-= into OP_ADDILV/OP_SUBILV
compiler: compile simple assignments directly into locals
docs: update operator opcode status
```

### 完了条件

- go-mrbc の加算ループが 2.11x → **1.2x 前後**（達成）
- go-mrbc の mixed.rb が 1.33x → **1.1x 前後**（達成）
- Phase 4 まで入れて公式 mrbc とほぼ同等（1.04〜1.1x、達成）
- `go test ./...`、go-mrbvm の `vmcompat` / `official manifest` がすべて成功（達成）
