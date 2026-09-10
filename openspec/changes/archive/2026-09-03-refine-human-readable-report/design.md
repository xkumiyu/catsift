## Context

現在のhuman rendererは、全command共通の `CatSift · CODEX` title、1行に連結したcontext、および `Rows` を含むfooterを `internal/output` で組み立てている。`TerminalCapabilities` と `--color` の挙動、集計結果の型、JSON renderer、terminal幅ごとのtable layoutは既存の契約として維持する。変更対象はproposal.mdとdelta specで定めたhuman-readable reportの表示層に限定する。

## Goals / Non-Goals

**Goals:**

- reportの内容が最初に分かるheadingと、読み分けやすいcontext行を共通形式にする。
- Tool数・Skill数と合計値を、利用者が意味を推測せず理解できるfooterへ変換する。
- 色を情報の構造・異常・証拠状態のscan補助に限定し、plain outputでも同じ情報を保持する。
- 現在の集計値、sort、width別column選択、JSON schema、TTY/`NO_COLOR` 契約を維持する。

**Non-Goals:**

- 新しいcommand、option、Agent adapter、集計field、JSON fieldの追加。
- interactive TUI、animation、progress indicator、box layoutの導入。
- reportの日本語化や多言語対応。
- terminal libraryや外部dependencyの追加。

## Decisions

### 1. グローバルな製品名ではなくcommand固有の内容headingを使う

各reportの先頭を次のheadingに統一する。

```text
USAGE OVERVIEW
TOOL USAGE
SKILL USAGE
```

`stats`には既存のtable headingがないため `USAGE OVERVIEW` を追加し、`tools` と `skills` は既存のsection headingをreport headingとして利用する。`CatSift`、`catsift stats` のような実行ファイル名・製品名だけのheadingは出力しない。対象Agentはheadingへ埋め込まず、全human reportで `Agent: Codex` のcontext lineとして表示する。

製品名を残す案は、通常のCLI実行ではcommand名と重複し、集計内容を伝えないため採用しない。内容headingを残すことで、reportを単独でコピーした場合にも何の値かは判別できる。

### 2. contextは順序を固定したlabel付き行として描画する

`contextLine`の単一文字列を拡張し、commandごとに必要な項目を次の順で別行へ描画する。

```text
Agent: Codex
Period: all time
Layer: effective
```

`stats`は `Agent`、`Period`、`Skill grouping`、`tools`は `Agent`、`Period`、`Layer`、`skills`は `Agent`、`Period`、`Group by`、`Strict`を表示する。値の順序とlabelは固定し、`·`や別の装飾的なUnicode区切りは使わない。改行形式は項目幅に依存しないため、狭いterminalでもcontext自体を折り返さずに済む。

human-readable用のAgent表示は`Codex`とする一方、JSONの既存の小文字識別子`codex`は`agent` fieldへそのまま保持する。human rendererとmachine rendererの表示責務を混ぜない。

### 3. footerはreport kindに応じてdomain用語を生成する

`Rows`を汎用的な実装用語として出力するのをやめ、footer生成をreport kindごとのhelperへ集約する。

```text
3 tools, 879 calls total
12 skills, 87 uses total
```

対象件数は集計row数、合計値は既存のTool CallsまたはSkill Usesのsumを使用する。件数・合計が1の場合は英語の単数形へ変換し、0または複数の場合は複数形を使う。空結果でも `0 tools, 0 calls total` または `0 skills, 0 uses total` とし、empty-state messageと矛盾しないsummaryを残す。

footerの情報自体を削除する案は、table全体の規模と合計値を末尾で確認できる既存の利点を失うため採用しない。`Entries`のような置換も利用者のdomain理解を助けないため採用しない。

### 4. 色はsemantic tokenを少数に限定する

既存の`ColorsEnabled`判定をそのまま利用し、描画関数へ色を直接散在させない。colorが有効な場合の役割を次のようにする。

| 対象 | 表現 |
| --- | --- |
| report heading | 共通のaccent color + bold |
| table header | bold。headingと異なる階層として識別可能にする |
| summaryの主要値 | boldを基本とし、通常rowと区別する |
| table内のFailures値 | terminal既定色。列名と数値で意味を示す |
| Skillのevidence status値 | terminal既定色。列名で意味を示す |
| context、footer、Last Used | faintなどの補助style |
| 通常のlabel、row、count | terminal既定色 |

通常状態の全rowへsuccess colorを付けたり、色だけで値の意味を伝えたりしない。table内のFailures値とSkillのevidence status値は個別セルをsemantic colorで強調せず、列名と数値によって意味を示す。既存のANSI基本色を利用し、truecolorや新しいcolor profile依存は導入しない。`ColorNever`、non-TTY、`NO_COLOR`では全styleを無効化し、`ColorAlways`だけが明示的にstyleを有効化する既存優先順位を維持する。

全rowへ色を付ける案は視線誘導が弱くなり、terminal themeによるコントラスト差も増えるため採用しない。見出しを一色、例外をsemantic colorへ限定することで、plain textとstyled reportの情報構造を一致させる。

### 5. rendererの責務とlayout境界を維持する

`RenderHuman`の共通構造を次へ変更する。

```text
content heading
context lines
blank line
summary or table
blank line
domain footer
```

`stats`にはsummary前の`USAGE OVERVIEW`を追加し、`tools`と`skills`は既存のsection headingを先頭へ移す。`renderTools`、`renderSkills`のcolumn選択とellipsis規則は維持し、heading・contextの追加によってtableの利用可能幅を減らさない。contextは1項目1行なので横幅のlayout計算対象から除外する。

JSONは既存の`RenderJSON`を経由し、human heading、context、footer、ANSI styleを生成する処理から分離したままにする。これにより`--json --color always`でもmachine outputへ表示変更やescape sequenceが漏れない。

### 6. 表示契約をplain report中心にテストする

表示内容のgoldenまたは文字列assertionは`ColorNever`を正規形とし、heading、contextの行分け、footerのdomain用語、単数・複数形、JSON不変を固定する。`ColorAlways`ではANSIの存在と主要style対象を確認し、特定terminalのescape byte列に過度に依存しない。60・80・120 column、TTY/non-TTY、`NO_COLOR`、empty state、Unicodeを含むlong nameは既存のwidth testと合わせて確認する。

## Risks / Trade-offs

- [グローバルな製品名を削ると、単独保存したreportの出所が分かりにくくなる] → `USAGE OVERVIEW`等の内容headingと`Agent: Codex`を必ず出力し、JSONの`agent` fieldは維持する。
- [contextを複数行にすると出力が縦に長くなる] → 3 commandで情報量を優先し、compact layoutのtable行や不要な装飾は増やさない。狭い幅でもcontextを折り返さない。
- [ANSI色がterminal themeで見づらくなる] → 基本色・bold・faintに限定し、色を意味の唯一の手段にしない。`--color never`と`NO_COLOR`を維持する。
- [既存のsnapshotや利用者の文字列assertionが旧heading/footerに依存する] → report test、README例、関連するOpenSpecの表示シナリオを同じ変更で更新し、JSON契約は変更しない。
- [英語の単数・複数形が将来のlocalizationを制約する] → 今回は既存の英語outputの範囲に限定し、localizationは別changeで扱う。

## Migration Plan

永続data、CLI option、JSON schemaのmigrationは不要である。実装時はhuman rendererのheading/context/footer/style helperを更新し、plain reportのtest、styled reportのtest、READMEの例を順に新しい表示契約へ合わせる。集計・JSON・履歴読取への変更は行わない。

rollbackはbinaryを以前のversionへ戻すだけで完了し、catsiftが読むCodex履歴やユーザー設定へ影響しない。
