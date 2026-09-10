# CatSift: Sift through Coding Agent Traces

CatSiftは、AIコーディングエージェントの利用状況を確認するコマンドラインツールです。
ローカルの履歴を集計し、Session, Tool, Skillの利用状況を表示します。

[English version](README.md)

> [!NOTE]
> CatSiftは次に対応しています。
> - Codexのローカル履歴（デフォルト）
> - OpenCodeのローカル履歴
> - [ctx](https://github.com/ctxrs/ctx)のevent stream

## クイックスタート

```sh
npx catsift
```

## インストール

npmでインストールします。

```sh
npm install --global catsift
```

または、[GitHub Releases](https://github.com/xkumiyu/catsift/releases)からダウンロードできます。

または、ソースからインストールできます。

```sh
go install github.com/xkumiyu/catsift/cmd/catsift@latest
```

## 使い方

### 利用状況の概要

Agentの利用状況を概要として表示します。

```sh
catsift stats
```

```text
USAGE OVERVIEW
Source: Codex (~/.codex)
Agents: Codex
Period: 2026-01-01 to 2026-01-31

Activity
  Sessions                    123
  Turns                       456
  User Prompts                789
  Tool Calls                1,234

Skill Usage
  By turn                      42
  By session                   24

Token Usage
  Total Tokens                3.16B
    Input Tokens              3.14B
      Cached Tokens           3.06B
    Output Tokens             13.3M
      Reasoning Tokens        6.40M
```

ctx sourceでは、Token usageを利用できません。

### Skillの利用状況

利用されたSkillと、その利用がどのように検出されたかを表示します。

```sh
catsift skills --view mode
```

```text
SKILL USAGE
Source: Codex (~/.codex)
Agents: Codex
Period: 2026-01-01 to 2026-01-31
Group by: turn
Strict: false
View: mode

Skill                       Explicit  Implicit  Unknown  Total
──────────────────────────────────────────────────────────────
code-review                       5         1        0      6
openspec-apply-change             2         1        0      3

2 skills, 9 uses total
```

詳しくは[Skill集計の詳細](#skill集計の詳細)を参照してください。

### Toolの利用状況

canonical Tool名ごとの呼び出し数、失敗数、最後の利用時刻を表示します。

```sh
catsift tools
```

```text
TOOL USAGE
Source: Codex (~/.codex)
Agents: Codex
Period: 2026-01-01 to 2026-01-31
Layer: effective

Tool       Calls  Failures  Last Used
────────────────────────────────────────────────────
shell          42         0  2026-09-01 12:34 JST

1 tool, 42 calls total
```

### 共通オプション

- `--source`で履歴sourceを選択します。デフォルトはCodexのローカル履歴です。1回の実行で利用できるsourceは1つだけです。
- `--days N`でレポートの対象を直近N日間に制限します。
- `--from YYYY-MM-DD`と`--to YYYY-MM-DD`でUTCの暦日範囲（指定日を含む）を指定します。どちらか一方だけでも指定できます。`--days`とは併用できません。
- `Period`は、実際に集計されたデータの期間を表示します。
- `--json`で機械可読な出力を生成します。

## Skill集計の詳細

### Skill利用の項目

`catsift skills`は、次の項目を表示します。

| 軸 | 項目 | 意味 |
| --- | --- | --- |
| activation mode | `Explicit` | `$skill-name`のような明示的なSkill指定、または構造化されたSkill呼び出しが記録されている。 |
| activation mode | `Implicit` | 明示指定なしで、Skillの`SKILL.md`やscriptsへのruntime accessから利用を推定している。 |
| activation mode | `Unknown` | Skill利用の証拠はあるが、明示的・暗黙的のどちらで起動されたか履歴から判別できない。 |
| evidence state | `Confirmed` | Skillの指示やSkill item/toolが読み込まれた、または呼び出されたことを直接示す履歴がある。 |
| evidence state | `Inferred` | ファイルやスクリプトへのruntime accessから利用を推定している。 |
| evidence state | `Unconfirmed` | 明示的な指定はあるが、利用を確認できる証拠がない。 |
| 集計 | `Total` | 重複を除いた利用回数。既定ではturn単位、`--group-by session`指定時はsession単位で数える。 |

activation modeとevidence stateは独立した軸です。1つの利用に複数のactivation modeの証拠が含まれる場合があるため、内訳の合計が`Total`と一致しないことがあります。`--strict`を指定すると`Confirmed`だけを集計します。

### Skill利用表の表示方法

Skill利用表の表示形式は、`--view`で切り替えられます。

```sh
catsift skills --view compact  # Totalのみ
catsift skills --view mode     # activation mode
catsift skills --view state    # evidence state
catsift skills --view all      # 両方の表
```

デフォルトの`--view auto`は端末幅に応じて`compact`、`mode`、`all`のいずれかのviewを表示します。

### 未使用Skillの確認

`skills`に`--unused`を指定すると、選択した履歴ソースとインストール済みSkillの
inventoryを比較できます。

```sh
catsift skills --unused
```

inventoryのidentityはcanonical skill nameと絶対物理PATHの組み合わせです。そのため、
異なるPATHに同名Skillがある場合、その名前が未使用なら別々の行として表示されます。
使用済み判定はcanonical name単位のままです。選択したctxのいずれかのAgentがその名前を
使用していれば、その名前のinventory行はすべて使用済みとみなします。

## キャッシュ

解析結果は、次回以降の実行を高速化するためOS標準のユーザーキャッシュ領域に保存されます。

## データの扱い

Codexの履歴、ctxの公開された読み取り専用event stream、またはOpenCodeのローカルread-only databaseだけを読み取り、選択したデータを変更しません。
履歴を外部へ送信せず、通常の出力にユーザー本文、コマンド本文、その他のraw event詳細を含めません。
