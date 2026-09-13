# CatSift: Sift through Coding Agent Traces

CatSiftは、ローカルに保存されたコーディングエージェントの履歴を解析し、
source、Model、Skill、Sessionごとの利用状況を表示するツールです。

[English version](README.md)

> [!NOTE]
> CatSiftは次の履歴を読み取ります。
> - Codexのローカル履歴
> - OpenCodeのローカル履歴
> - [ctx](https://github.com/ctxrs/ctx)のevent stream

## Quick start

事前のインストールなしで試せます。

```sh
npx catsift
```

## Installation

npmでインストールします。

```sh
npm install --global catsift
```

または、[GitHub Releases](https://github.com/xkumiyu/catsift/releases)からダウンロードできます。

または、ソースからインストールできます。

```sh
go install github.com/xkumiyu/catsift/cmd/catsift@latest
```

## Usage

インタラクティブモードで起動します。

```sh
catsift
```

ローカルのAgent利用状況を、次の4つのviewで確認できます。

| View | 表示内容 |
| --- | --- |
| Overview | 利用状況の合計と日別activity |
| Models | providerおよびModelごとの利用状況と関連Session |
| Skills | [Skill usage](#skill-usage-fields)、evidence state、関連Session |
| Sessions | Session metadataと時系列のTurn詳細 |

必要に応じてsourceと期間を指定します。

```sh
catsift --source codex --days 30
catsift --source ctx --ctx-data-root /path/to/ctx --days 30
catsift --source opencode --opencode-home /path/to/opencode --days 30
```

1回の実行で読み取るsourceは1つです。

## Common options

これらのoptionは、[interactive mode](#usage)と[CLI mode](#cli-usage)の両方で利用できます。

- `--source`で履歴source（`codex`、`ctx`、`opencode`）を選択します。
- `--days N`で対象を直近N日間に制限します。
- `--from YYYY-MM-DD`と`--to YYYY-MM-DD`でUTCの暦日範囲（指定日を含む）を指定します。どちらか一方だけでも指定できます。`--days`とは併用できません。
- `--strict-input`で入力recordがskipされた場合にnon-zeroで終了します。
- `--verbose`で入力とcacheの診断情報を表示します。

## Skill usage fields

Skill usageはactivation modeとevidence stateで分類します。

| Dimension | Field | Meaning |
| --- | --- | --- |
| Activation mode | `Explicit` | `$skill-name`のような明示的なSkill requestまたはinvocation、または構造化されたSkill callが記録されている。 |
| Activation mode | `Implicit` | 明示的なrequestなしで、Skillの`SKILL.md`やscriptsへのruntime accessから利用を推定している。 |
| Activation mode | `Unknown` | Skill利用の証拠はあるが、明示的・暗黙的のどちらで起動されたか履歴から判別できない。 |
| Evidence state | `Confirmed` | Skillの指示やSkill item/toolが読み込まれた、または呼び出されたことを履歴が直接示している。 |
| Evidence state | `Inferred` | runtimeでのfileやscriptへのaccessから利用を推定している。 |
| Evidence state | `Unconfirmed` | 明示的なrequestが記録されているが、確認できる証拠がない。 |

activation modeとevidence stateは独立した軸です。1つの利用に複数のactivation modeの証拠が含まれる場合があるため、modeの内訳の合計が`Total`と一致しないことがあります。

## CLI usage

サブコマンドを指定すると、CLIモードで実行します。

| Command | Description |
| --- | --- |
| `catsift stats` | Agent利用状況の概要を表示 |
| `catsift tools` | canonical Tool名ごとの利用状況を表示 |
| `catsift skills` | Skillの利用状況とevidence stateを表示 |

CLI専用の`--json`でmachine-readableな出力を生成します。

### Usage overview

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
    Output Tokens              13.3M
      Reasoning Tokens        6.40M
```

`Period`は、集計に含まれるdataの期間を表示します。

ctx sourceではtoken usageを利用できません。

### Skill usage

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

### Tool usage

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

### Skill command options

#### Skill usage view

`--view`でSkill usage viewを選択します。

```sh
catsift skills --view compact  # Total only
catsift skills --view mode     # Activation mode
catsift skills --view state    # Evidence state
catsift skills --view all      # Both tables
```

デフォルトの`--view auto`は、terminal幅に応じて`compact`、`mode`、`all`のいずれかを選択します。

`--group-by`でturn単位またはsession単位に集計し、`--strict`で`Confirmed`の利用だけを集計します。

#### Unused skills

`skills`に`--unused`を指定すると、選択した履歴sourceとインストール済みSkill inventoryを比較できます。

```sh
catsift skills --unused
```

inventoryのidentityはcanonical skill nameと絶対physical pathの組み合わせです。
そのため、異なるpathに同名Skillがある場合、その名前が未使用なら別々のrowとして表示されます。
使用済み判定はcanonical name単位のままです。
選択したctx agentのいずれかがその名前を使用していれば、その名前のinventory rowはすべて使用済みとみなします。

## Cache

解析結果は、次回以降の実行を高速化するためOS標準のuser cache directoryに保存されます。

## Data handling

CatSiftはCodexの履歴、ctxの公開されたevent stream、またはOpenCodeのlocal databaseだけを
読み取り、選択したdataを変更しません。履歴を外部へ送信しません。
TUIとreportには、prompt本文、command本文、Tool arguments、Skill bodies、
provider payload、その他のraw event detailsを表示・含めません。
