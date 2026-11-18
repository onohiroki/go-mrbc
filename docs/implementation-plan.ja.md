# go-mrbc 実装計画

このドキュメントは `go-mrbc` の実装計画を日本語で管理するものである。
英語の概要は [roadmap.md](roadmap.md)、設計は [architecture.md](architecture.md)、
バイトコード形式は [bytecode-format.md](bytecode-format.md) を参照。

## 1. 目的

- mruby の `mrbc` を Go で再実装し、Ruby サブセットを mruby 4.0.0 の
  RITE 04.00 バイトコードにコンパイルする。
- パーサには [go-ruby-prism](https://github.com/danielgatis/go-ruby-prism)
  を使う。Prism は WebAssembly で動くため cgo は不要。パーサ以外は pure Go。
- 生成した `.mrb` は本家 mruby 4.0.0 の VM で実行できること。
- 最終的には、Go 製ツールのマクロ言語として mruby を埋め込めるようにする。
- pure Go の mruby VM は別プロジェクトとして並行開発する。本リポジトリには
  VM を含めない。

## 2. 確定事項

| 項目 | 決定 |
| --- | --- |
| 受け入れ基準 | 動作一致（stdout / stderr / 終了コード）。バイト一致は目指さない |
| 例外 | 初期は `raise` のみ。M3 で `begin/rescue/ensure` まで実装済み |
| ターゲット | mruby 4.0.0 固定（RITE 04.00） |
| モジュール | `github.com/onohiroki/go-mrbc` |
| CLI | `gomrbc` |
| VM | pure Go 実装は別プロジェクト。`bytecode` パッケージと適合コーパスを共有 |
| 未対応構文 | 必ず位置付きのコンパイルエラーにする（暗黙に無視しない） |
| 最適化 | 初期実装では行わない。演算子もまず `SEND` で出力する |

## 3. 全体アーキテクチャ

```
Ruby source
    │
    ▼
go-ruby-prism (WASM / wazero)
    │  Prism AST（locals / depth 付き）
    ▼
internal/frontend
    │  lowering: Prism AST → 自前 IR
    │  unless/until/修飾子/for/op-assign などの正規化
    ▼
internal/compiler
    │  スコープ解析、ローカル変数スロット割当、
    │  レジスタ割当、aspec/ENTER、ブロック/upvar、
    │  制御フロー、bytecode emission
    ▼
bytecode.Irep
    │
    ▼
bytecode.DumpBinary (RITE 04.00)
    │
    ▼
.mrb
    │
    ▼
本家 mruby 4.0.0 VM
```

### 3.1 自前 IR を挟む理由

- Prism の AST は種類が多く、Ruby のバージョンで増減する。
- `unless`、`until`、修飾子、`for`、多重代入、op-assign などを一箇所で正規化できる。
- コード生成と golden テストの対象を安定させられる。
- 対応構文を明示的なホワイトリストにできる。未知のノードはエラーにする。

自前 IR はモジュール内部に閉じる。外部との契約は `bytecode` パッケージだけ。

### 3.2 パッケージ構成

```
go-mrbc/
├── go.mod
├── cmd/gomrbc/              CLI
├── mrbc/                    公開コンパイル API
├── bytecode/                RITE / opcode / encoder / decoder / codedump
├── internal/frontend/       Prism AST → IR
├── internal/compiler/       IR → Irep
├── toolchain/               mruby / mrbc の探索（テスト用）
├── testdata/
│   ├── conformance/         動作一致テスト（VM と共有可能）
│   ├── errors/              コンパイルエラーのテスト
│   └── ...
├── docs/
└── Makefile
```

## 4. パーサ統合（M0）

1. `go-ruby-prism` を追加し、パーサを 1 回生成して使い回す。
   - `WithFilePath`、`WithMainScript(true)`、`WithEncoding("UTF-8")` を設定。
2. Prism の `Errors` / `Warnings` を `file:line:col` 付きで報告する。
   失敗時は `.mrb` を書き出さない。
3. Prism AST → 自前 IR の lowering を実装する。
   - 最初はリテラル、ローカル変数、呼び出し、`if`、`while`、`def`、ブロック。
   - 未対応ノードは `unsupported syntax: <NodeName>` として位置付きエラー。
4. デバッグ用に `--dump-ast` と `--dump-ir` を用意する。
5. **スコープ解決の検証**:
   - Prism の `locals` と `depth` を mruby の `GETUPVAR` / `SETUPVAR` と対応付ける。
   - ネストブロック、メソッド内ブロック、クラス本体、ブロックローカル `|x; y|`
     をフィクスチャにして `mrbc -v` と突き合わせる。
   - Prism の情報だけで不足する場合は自前のスコープ解析を追加する。

### 4.1 Prism のスコープ情報

- `LocalVariableReadNode.Depth == 0`: 現在のスコープのローカル。
- `Depth == n`: n 個外側のブロックスコープのローカル。
- mruby の `GETUPVAR (slot, level)` の `level` は、何個外側の irep かを表す。
  Prism の `depth` から mruby の `level` への変換を一箇所に閉じ込める。
- スコープごとの `locals` 配列を使って、後方で代入される変数も先に
  スロットを確保する（Ruby のスコープ規則）。

## 5. RITE writer（M0.5）

コード生成より先に、手書きの IREP を `mruby` で実行して疎通させる。

- RITE ヘッダ（`RITE` + `0400` + size + `MATZ` + `0000`）
- `IREP` セクション、`END` フッタ
- irep record:
  - record size / `nlocals` / `nregs` / `rlen`
  - `clen` / `ilen` / iseq
  - pool（int32 / int64 / float / string / bigint）
  - symbol table
  - 子 irep の再帰
- iseq エンコーダ:
  - operand 型 `Z/B/BB/BBB/BS/BSS/S/W`
  - 16bit / 24bit はビッグエンディアン
  - ジャンプはオペランド終端からの相対オフセット
  - `EXT1` / `EXT2` / `EXT3`
- catch handler（rescue / ensure）は `begin` のコンパイル時に生成する。
- LVAR / DBG はまだ出さない（実行には不要であることを確認済み）。

スモークテスト:

```
puts "hello" を手書き iseq で生成 → .mrb → mruby で実行
```

簡易逆アセンブラ（`--verbose`）も同時に作る。`mrbc -v` と比較できるため、
以降のデバッグ効率が大きく変わる。

## 6. コンパイラ設計（M1 以降）

### 6.1 スコープとレジスタ

- スコープ種別: top-level / method / class body / module body / block / lambda。
- 各スコープで以下を持つ:
  - ローカル変数名 → レジスタ番号
  - パラメータの固定レイアウト
  - `nlocals` / `nregs`
  - 親スコープへの参照（upvar）
- 式評価は `cursp` を進めながら行い、`nregs = max(cursp)`。
- ローカル変数への代入は、可能なら直接そのレジスタへ書く。
  難しければ一時レジスタに評価して `MOVE` する。
- `R0` は `self`。`nlocals` は `R0` を含む。

### 6.2 ENTER / aspec

- メソッド・ブロック・lambda の先頭は `OP_ENTER`。
- aspec:

```
noblock:1 req:5 opt:5 rest:1 post:5 key:5 kdict:1 block:1
```

- オプション引数のデフォルト値は、`ENTER` の直後に引数個数分の 3 バイト
  `JMP` を並べ、その後に初期化コードを置く mruby の方式を踏襲する。
- ブロックは `ENTER` の非 strict パスで引数が展開される点に注意する。
- `yield` は `BLKPUSH` + `BLKCALL`。
  `BLKPUSH` の aspec は ENTER と別形式（`m5:r1:m5:d1:lv4`）なので、
  `vm.c` の `vm_op_blkpush` を仕様として移植する。
- `BLKPUSH` の `lv` は、ネストしたブロックから外側メソッドのブロックを
  参照する場合に必要になる。M2 で対応する。

### 6.3 演算子はまず SEND

- `+`、`-`、`*`、`/`、`==`、`<`、`[]`、`!`、単項 `-` などは `SEND` で出す。
  - 例: `a + b` → `SEND R[a] :+ n=1`
  - mruby の `OP_ADD` などは最適化用であり、意味的にはメソッド呼び出しでよい。
- `&&` / `||` / `!` の短絡評価と真偽値判定だけは制御フローで実装する。
- 後から `ADD` / `ADDI` / `EQ` / `LT` などを追加できる。
- これにより、算術系 opcode の実装と検証を初期から切り離せる。

### 6.4 SEND のエンコーディング

- 引数はレシーバの直後に連続配置する。
- 実装する命令:
  - `SEND` / `SENDB`（明示レシーバ）
  - `SSEND` / `SSENDB`（暗黙 self。`puts` などの private 呼び出しのため）
- キーワード引数は `c = n | k<<4`（15 は keyword hash）。M2 で対応。
- ブロック引数・splat は M2。
- レジスタ配置は `vm.c` の `OP_SEND` 実装と `mrbc -v` を突き合わせて仕様化する。

### 6.5 制御構文

- `if` / `unless` / `while` / `until` / `for` / `case-when` はジャンプで実装。
- `break` / `next` はループコンテキストのスタックで解決する。
- ブロック内の `return` は `RETURN_BLK`、メソッド内は `RETURN`。
- ブロック内の `break` は `OP_BREAK`。
- `ensure` を跨ぐ `break` / `next` / `retry` / `redo` は `JMPUW` で巻き戻す。
- 文字列補間は `STRING` + `STRCAT` で実装する。

### 6.6 クラス・モジュール（M2）

- `CLASS` / `MODULE` / `EXEC` / `OCLASS` / `TCLASS`
- `TDEF`（通常メソッド）、`SDEF`（`def self.foo`）、`DEF`（特異メソッド）
- `GETIV` / `SETIV`、`GETCONST` / `SETCONST`、`GETMCNST` / `SETMCNST`
- `SUPER` / `ARGARY`（zsuper）
- `attr_accessor` などはランタイムのメソッドなので、コンパイラ側の追加実装は不要。

### 6.7 例外（M3）

- catch handler テーブル（type + begin/end/target、13 バイト/件）
- `EXCEPT` / `RESCUE` / `RAISEIF` / `JMPUW`
- `begin` / `rescue` / `ensure`、`rescue` 修飾子
- `ensure` 中の `return` / `break` の巻き戻し
- `retry` / `redo` はさらに後

## 7. 機能の優先順位

### Tier 0: M1（最初の実用版）

| 機能 | コンパイラ側の作業 |
| --- | --- |
| `nil/true/false`、整数、浮動小数、文字列、シンボル | リテラル + pool |
| ローカル変数、グローバル変数、定数読み | レジスタ割当、GETGV/GETCONST |
| `if/unless/while/until`、三項、修飾子 | ジャンプ |
| `case/when` | `===` の SEND |
| `def`（位置引数・デフォルト・rest） | ENTER/aspec |
| メソッド呼び出し（引数、暗黙 self） | SEND/SSEND |
| ブロック、`yield`、`each/map` など | BLOCK、SENDB、BLKPUSH/BLKCALL |
| 配列・ハッシュ・範囲、`[]` / `[]=` | ARRAY/HASH/RANGE、SEND |
| 文字列補間、ヒアドキュメント | STRING/STRCAT |
| `break` / `next` / `return`（ループ・メソッド） | ジャンプ、RETURN |
| `puts/print/p/raise` | ランタイム呼び出し |

### Tier 1: M2（マクロ DSL 向け）

- クラス、モジュール、継承、`super`
- インスタンス変数、クラス変数、定数代入
- キーワード引数、splat、ブロック引数 `&b`
- `lambda` / `proc`
- 多重代入
- `define_method`、`send`、`class_eval` など（ほぼランタイム任せ）

### Tier 2: M3 以降

- `begin/rescue/ensure`
- `retry` / `redo`
- パターンマッチング `case/in`
- `alias` / `undef`
- `defined?`、safe navigation `&.`
- `__FILE__` / `__LINE__`、特殊変数
- `-g`（DBG）、LVAR

### 対象外

- `eval` / `load` / `require`
- エンコーディング・マジックコメント
- Fiber / Thread / TracePoint / Refinement
- `BEGIN` / `END`、`__END__` / DATA
- mruby 3.x の RITE 形式との互換

未対応構文は必ずコンパイルエラーにする。マクロ言語として
「動いたつもりで壊れる」ことを避ける。

## 8. 実装する opcode の段階

| フェーズ | opcode |
| --- | --- |
| M1 | `MOVE`, `LOADNIL/TRUE/FALSE/SELF`, `LOADI_0..7`, `LOADI8/16/32`, `LOADINEG`, `LOADI__1`, `LOADL`, `LOADSYM`, `STRING`, `SYMBOL`, `GETGV/SETGV`, `GETCONST`, `GETUPVAR/SETUPVAR`, `JMP/JMPIF/JMPNOT/JMPNIL`, `SEND/SENDB/SSEND/SSENDB`, `ENTER`, `RETURN`, `BLOCK`, `ARRAY/ARRAY2`, `HASH`, `RANGE_INC/RANGE_EXC`, `STRCAT`, `GETIDX/SETIDX`, `BLKPUSH/BLKCALL`, `STOP`, `EXT1/2/3` |
| M2 | `CLASS/MODULE/EXEC/OCLASS/TCLASS`, `TDEF/SDEF/DEF`, `GETIV/SETIV`, `GETCV/SETCV`, `SETCONST/GETMCNST/SETMCNST`, `SUPER`, `ARGARY`, `KARG/KEY_P/KEYEND`, `LAMBDA`, `BREAK`, `RETURN_BLK`, `ARYPUSH/ARYCAT/ARYSPLAT/APOST`, `HASHADD/HASHCAT` |
| M3 | `EXCEPT/RESCUE/RAISEIF`, `JMPUW`, catch handler table, `SCLASS`, `ALIAS/UNDEF` |
| M4 | `MATCHERR`、最適化 opcode（`ADD` / `ADDI` / `SUB` / `EQ` / `LT` など、`SEND0` / `SSEND0`、短縮 `LOADI`） |

`ENTER` の aspec と `SEND` のオペランド配置は、最初に単体テストで固める。

## 9. テスト戦略

1. **エンコーダ単体テスト**
   - 各 opcode のバイト列
   - ジャンプの相対オフセット
   - pool / symbol のエンコーディング
   - `EXT1` / `EXT2` / `EXT3`
2. **差分実行テスト（主力）**
   - フィクスチャ `.rb` を `mrbc` と `gomrbc` でコンパイル
   - それぞれを mruby 4.0.0 で実行し、stdout / stderr / 終了コードを比較
   - `mrbc --no-optimize` も参照に使う
3. **逆アセンブル比較**
   - 自前 `--verbose` と `mrbc -v` を並べる
   - 最初は目視、慣れたら意味が同じ命令列かを検査する
4. **異常系**
   - 未対応ノード、構文エラー、レジスタ不足、引数過多
   - panic せず、位置付きエラーになること
5. **ランダム生成テスト**
   - サブセット内の小さなプログラムを生成し、両方で実行して比較
   - 生成器は式・文・メソッド・ブロック程度から始める
6. **CI**
   - mruby 4.0.0 を固定で用意
   - ローカルでは `RBENV_VERSION=mruby-4.0.0` を考慮
   - mruby が無い環境では該当テストを skip（CI では必須）

受け入れコマンドの例:

```sh
gomrbc -o /tmp/basic.mrb testdata/conformance/basic/main.rb
RBENV_VERSION=mruby-4.0.0 mruby /tmp/basic.mrb
```

## 10. マイルストーン

| マイルストーン | 内容 | 目安 |
| --- | --- | --- |
| M0 | パーサ統合、IR lowering、スコープ検証、CLI 骨組み | 1–2 日 |
| M0.5 | RITE writer、手書きバイトコードの実行、簡易逆アセンブラ | 2–3 日 |
| M1 | スカラー、制御構文、メソッド、ブロック、配列/ハッシュ、文字列補間 | 5–8 日 |
| M2 | クラス/モジュール、ivars、キーワード引数、lambda、多重代入、super | 5–8 日 |
| M3 | 例外/ensure、catch table、非局所制御、デバッグ情報 | 3–5 日 |
| M4 | 最適化、パターンマッチ、メタプログラミング系の整備、性能 | 継続 |

M1 完了時点で「手続き型＋ブロック＋配列/ハッシュ」のマクロ言語として
実用になる。M2 でクラスベースの DSL も書けるようになる。

## 11. リスクと対策

| リスク | 対策 |
| --- | --- |
| Prism のスコープ / `depth` が mruby とずれる | M0 で `mrbc -v` と突き合わせる。不足なら自前スコープ解析に切り替える |
| `ENTER` / aspec / ブロック引数の不一致 | aspec の全組み合わせを単体テスト。`mrbc -v` と比較する |
| `SEND` の kwargs / block / splat のレジスタ配置 | `vm.c` の該当ケースを仕様化し、引数パターンごとにテストする |
| レジスタ不足・`nregs` の誤り | `nregs = max(cursp)` を不変条件にし、デバッグ時に assert する |
| ブロック内 `break` / `return` / ensure の巻き戻し | M1 / M2 では制限付きでエラーにし、M3 で完全対応する |
| `mrbc` とのバイト差 | 最初からバイト一致を目指さない。動作一致を基準にする |
| mruby のバージョン差 | 4.0.0 固定。RITE バージョンと opcode テーブルをリポジトリに固定する |
| Prism / WASM 依存 | バージョンを `go.mod` で固定。パーサは使い回す |
| 未対応構文の誤コンパイル | ホワイトリスト方式。未知ノードは必ずエラーにする |

## 12. pure Go VM プロジェクトとの関係

- VM 側は `github.com/onohiroki/go-mrbc/bytecode` を import し、
  RITE reader と decoder を共有する。
- コンパイラの適合コーパスを VM の適合テストに再利用する。
- コンパイラは VM に依存しない。常に本家 mruby 4.0.0 を正解とする。
- VM の進捗に応じて、同じ `.mrb` を「C mruby」と「Go VM」の両方で
  実行して比較する三者差分テストを追加できる。
- `bytecode` パッケージは、将来 `go-mruby-bytecode` として独立モジュールに
  切り出せるよう、parser / compiler への依存を持たせない。

## 13. 現在の進捗

### 実装済み

- `bytecode` パッケージ: RITE 04.00 の dump / read、opcode テーブル、
  iseq エンコーダ/デコーダ、`EXT1/2/3`、逆アセンブラ
- `internal/frontend`: Prism AST → 自前 IR、位置付きエラー、IR ダンプ
- `internal/compiler`:
  - ローカル変数・upvar（`GETUPVAR`/`SETUPVAR`）
  - ローカル/グローバル/ivars/クラス変数の複合代入（`+=`, `||=`, `&&=`）
  - リテラル、配列、ハッシュ、範囲、文字列補間
  - `if/unless/elsif`、`while/until`、`case/when`、`&&/||/!`
  - `def`（必須・オプショナル・rest・キーワード引数）、`return`
  - ブロック、`yield`、ブロック引数 `&b`、splat 呼び出し
  - `break`/`next`（while/until、ブロック、lambda）
  - `begin/rescue/ensure/else`、catch handler、`retry`、`redo`
  - `alias` / `undef`
  - DBG / LVAR（`-g` で行番号、デフォルトでローカル変数名）
  - パターンマッチ（値・変数・キャプチャ・配列・ガード・pin）
  - `raise`（例外の送出と捕捉）
  - クラス/モジュール、継承、`super`（通常・zsuper）、クラスメソッド、
    特異メソッド、`class << self`、ネストしたクラス、`A::B` 形式のクラス定義
  - インスタンス変数、クラス変数、定数の読み書き、`A::B` 定数参照
  - キーワード引数（必須・オプショナル・`**rest`）、キーワード hash 呼び出し
  - `lambda` / `proc`、ラムダ内の `return` / `next`
  - 多重代入（ローカル・グローバル・ivars・定数・splat ターゲット）
  - `for` ループ（単一ターゲット）
- `mrbc` パッケージと `gomrbc` CLI
- 差分実行テスト（`mrbc --no-optimize` と mruby 4.0.0 をオラクルに使用）
- 適合コーパス `testdata/conformance/`

### 現時点の制限

- `break`/`next` は while/until、ブロック、lambda で利用可能
- `yield` はメソッド本体と、そのメソッド内のブロックから利用可能。
  クラス本体や lambda 内からの `yield` はエラー
- `for` の分割代入ターゲット（`for a, b in ...`）は未対応
- 無名ブロック引数の転送 `def f(&); g(&); end` は未対応
- 1 つの呼び出しでキーワード引数と splat 引数の併用は未対応
- キーワード splat と明示的なキーワードの併用は未対応
- `super` への明示的なキーワード引数、および「名前付きキーワード + `**rest`」
  からの zsuper 転送は未対応
- 多重代入の index/属性ターゲットは未対応（splat ターゲットは対応）
- 属性・index への複合代入は未対応
- post 引数、`defined?` は未対応
- パターンマッチの hash / alternation / find / 配列 rest は未対応
- `rescue *ERRORS` とグローバル変数の `alias` は未対応

### 次の作業

1. 適合コーパスの拡充（パターンマッチ・デバッグ情報の境界ケース）
2. M4: 最適化 opcode、パターンマッチ拡張（hash/alternation/find/rest）、性能

M0.5 は M0 と独立しているため、並行して進めてもよい。
