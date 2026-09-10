## MODIFIED Requirements

### Requirement: overview統計を表示する

`catsift stats` は内容を示す `USAGE OVERVIEW` headingに続けて、入力source、対象Agent一覧、および期間をlabel付きcontext行として表示し、Sessions、User Prompts、Tool Calls、およびSkill Usesを視覚的に区切られたsummaryとして表示しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、4つの集計値は選択scope内の全Agentを合算しなければならない（SHALL）。グローバルな実行ファイル名または製品名だけのtitle（例: `CatSift`、`catsift stats`）をreportのheadingとして表示してはならない（MUST NOT）。Tool Callsはeffective Tool view、Skill Usesはturn単位で重複排除した全確認状態の利用を使用しなければならない（SHALL）。

#### Scenario: 履歴が存在する

- **WHEN** userがsource未指定の有効なCodex履歴に対して `catsift stats` を実行する
- **THEN** システムは `USAGE OVERVIEW` heading、`Source: Codex (~/.codex)`、`Agents: Codex`、対象期間、および4つの集計値を既定のhuman-readable report形式でstdoutへ出力する

#### Scenario: ctx sourceに複数Agentの履歴が存在する

- **WHEN** userが `catsift stats --source ctx` を実行し、ctxの選択scopeにCodexとOpenCodeの履歴がある
- **THEN** システムは `Agents: Codex, OpenCode` を表示し、両AgentのSessions、User Prompts、Tool Calls、およびSkill Usesを合算して出力する

#### Scenario: sourceと入力pathをcompactに表示する

- **WHEN** userが `catsift stats --source ctx --ctx-data-root /path/to/ctx-data` を実行する
- **THEN** システムは `Source: ctx (/path/to/ctx-data)` を1行で表示し、別の `History` または `Data root` context行を追加しない

#### Scenario: 対象履歴が空である

- **WHEN** 選択sourceに有効な利用Eventが存在しない
- **THEN** システムは入力source、Agent一覧、および選択期間のcontextを表示し、各集計値を0として出力するとともに、選択scopeに利用がないことを説明するempty-state messageを表示して0で終了する

### Requirement: Tool統計を表示する

`catsift tools` は `TOOL USAGE` headingに続けて、入力source、対象Agent一覧、選択中の期間、およびlayerをそれぞれlabel付きのcontext行として表示し、canonical Tool名ごとのCalls、Failures、およびLast Usedを見出し付きtableとして表示しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、同じcanonical Tool名の観測はAgentをまたいで合算しなければならない（SHALL）。footerは対象Tool数と総Callsをdomain用語で表示し、`Rows`という実装用語や中点区切りを使用してはならない（MUST NOT）。既定ではeffective layerを集計し、`--layer effective|runtime|model` により集計layerを選択できなければならない（SHALL）。

#### Scenario: 既定のTool統計を表示する

- **WHEN** userがsource未指定で `catsift tools` を実行する
- **THEN** システムは `TOOL USAGE` heading、Source・Agents・Period・Layerのcontext、effective Tool利用をCalls降順、同数の場合はTool名昇順で出力する

#### Scenario: 複数Agentの同じcanonical Toolを合算する

- **WHEN** ctxのCodexとOpenCodeがそれぞれcanonical Tool `shell` を利用している
- **THEN** システムはTool tableに `shell` を1rowだけ出力し、CallsとFailuresを両Agent分合算する

#### Scenario: Tool footerを表示する

- **WHEN** userが対象Toolのあるhuman-readable `catsift tools` reportを表示する
- **THEN** システムはfooterに対象Tool数と総Callsが分かるdomain用語の表現を表示する

#### Scenario: model layerを指定する

- **WHEN** userが `catsift tools --layer model` を実行する
- **THEN** システムはruntime actionではなくmodel layerのTool callだけを集計する

#### Scenario: 無効なlayerを指定する

- **WHEN** userが定義外の `--layer` 値を指定する
- **THEN** システムは引数errorをstderrへ出力し、非0の終了codeを返す

### Requirement: Skill統計を表示する

`catsift skills` は `SKILL USAGE` headingに続けて、入力source、対象Agent一覧、選択中の期間、grouping、およびstrict状態をそれぞれlabel付きのcontext行として表示し、Skill名ごとのExplicit、Implicit、Confirmed、Inferred、Unconfirmed、Total、およびLast Usedを集計しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、同じcanonical Skill名の利用はAgentをまたいで合算しなければならない（SHALL）。human-readable reportは利用可能な幅に応じて内訳columnを調整できるが、Skill名とTotalを常に表示しなければならない（SHALL）。footerは対象Skill数と総Usesをdomain用語で表示し、`Rows`という実装用語や中点区切りを使用してはならない（MUST NOT）。JSONはすべての集計fieldを保持しなければならない（SHALL）。`--strict` が指定された場合はstateが `confirmed` の利用だけをTotalと各mode集計の対象にしなければならない（SHALL）。

#### Scenario: 全Skill観測を表示する

- **WHEN** userが `catsift skills` を実行する
- **THEN** システムは `SKILL USAGE` heading、Source・Agents・Period・Group by・Strictのcontext、全確認状態のSkill利用をTotal降順、同数の場合はSkill名昇順で出力する

#### Scenario: 複数Agentの同じSkillを合算する

- **WHEN** ctxのCodexとOpenCodeが同じcanonical Skill `review` を利用している
- **THEN** システムはSkill tableに `review` を1rowだけ出力し、Agentをまたぐ利用を合算する

#### Scenario: Skill footerを表示する

- **WHEN** userが対象Skillのあるhuman-readable `catsift skills` reportを表示する
- **THEN** システムはfooterに対象Skill数と総Usesが分かるdomain用語の表現を表示する

#### Scenario: strict modeを使用する

- **WHEN** userが `catsift skills --strict` を実行する
- **THEN** システムは `confirmed` でないSkill利用を件数とLast Usedの算出から除外する

#### Scenario: 同一利用に複数の証拠がある

- **WHEN** 1回の重複排除済みSkill利用が複数の検出方式を持つ
- **THEN** システムはその利用をTotalへ1回だけ加算する

### Requirement: 期間とhistory sourceを各統計commandで指定できる

`stats`、`tools`、`skills` は共通して `--source codex|ctx`、`--days`、sourceに応じた `--codex-home` または `--ctx-data-root`、および `--color auto|always|never` を受け付けなければならない（SHALL）。`--source` の既定値は `codex` でなければならず（SHALL）、`--codex-home` はCodex source、`--ctx-data-root` はctx sourceでのみ有効でなければならない（MUST）。同一実行でCodexとctxを同時に入力へ含めてはならない（MUST NOT）。filterとsource解決はすべての出力形式で同じ結果集合へ適用しなければならない（SHALL）。

#### Scenario: source未指定時はCodexを使用する

- **WHEN** userが `catsift stats` を実行する
- **THEN** システムは既存のCodex home解決規則に従ってCodexだけを入力sourceにする

#### Scenario: ctx sourceとdata rootを指定する

- **WHEN** userが `catsift stats --source ctx --ctx-data-root /path/to/ctx` を実行する
- **THEN** システムは指定されたctx data rootだけを入力scopeとして使用する

#### Scenario: source固有optionを誤って組み合わせる

- **WHEN** userが `--source ctx --codex-home /path`、または `--source codex --ctx-data-root /path` を指定する
- **THEN** システムはoption errorをstderrへ出力し、reportを生成せず非0で終了する

#### Scenario: Codexとctxを同時に指定する

- **WHEN** userが複数のhistory sourceを同時に指定しようとする
- **THEN** システムはsource選択を拒否し、両sourceを合算したreportを生成しない

#### Scenario: 30日分のSkill統計を取得する

- **WHEN** userが任意のsourceで `catsift skills --days 30` を実行する
- **THEN** システムはcutoff以後の観測だけからSkill統計を生成する

### Requirement: human-readable report・JSONを提供する

各統計commandは既定で、report内容を示すheading、入力source、対象Agent一覧、適用中のfilterをlabel付きcontext行、明確なsectionまたはcolumn heading、整列した値、および必要なfooterを持つhuman-readable static reportを出力しなければならない（SHALL）。human-readable reportの`Source`はsourceのdisplay nameを表示し、Codexでは有効なCodex homeを括弧内へ、ctxでは明示された`--ctx-data-root`だけを括弧内へ表示しなければならない（SHALL）。source pathは`Source`行へ含め、別の`History`または`Data root` context行を追加してはならない（MUST NOT）。複数Agentの表示はcanonical IDの決定的な順序に対応するdisplay nameをcomma区切りで示し、context行を中点で連結してはならない（MUST NOT）。countは桁区切りして右揃えにし、tableのLast Usedはtimezoneを含む簡潔なlocal日時で表示しなければならない（SHALL）。`--json` でmachine-readable出力へ切り替えられなければならず（SHALL）、JSONはhuman-readable reportと同じfilter・集計結果を表し、`source` とcanonical Agent IDの `agents` arrayを含まなければならない（SHALL）。既存の `agent` string fieldは後方互換のため保持し、単一Agentでは従来の値、複数Agentではcanonical IDを決定的順序でcomma区切りした値を格納しなければならない（SHALL）。JSONのfield順、timestamp形式、およびwarningをstdoutへ混入させない規則を維持しなければならない（SHALL）。

#### Scenario: 既定reportを表示する

- **WHEN** userが出力形式を指定せず任意の統計commandを実行する
- **THEN** システムはreport内容を示すheading、入力source、対象Agentとfilterのcontext、指標名と値の関係を一読できるsummaryまたはtable、およびdomain用語で表現されたfooterを持つstatic reportをstdoutへ出力する

#### Scenario: contextを読みやすく表示する

- **WHEN** userが `catsift tools` または `catsift skills` を実行する
- **THEN** システムは `Source: ...`、`Agents: ...`、`Period: ...`、およびcommand固有のfilterを別々のlabel付き行へ出力し、項目間の区切りに中点や実装用の`Rows`を使用しない

#### Scenario: 複数Agentのhuman-readable reportを表示する

- **WHEN** ctx sourceのscopeにCodexとOpenCodeが含まれる
- **THEN** システムは `Source: ctx` と `Agents: Codex, OpenCode` を別々のcontext行へ出力する。`--ctx-data-root`が指定された場合はSource行を `Source: ctx (/path/to/ctx-data)` とする

#### Scenario: JSONを出力する

- **WHEN** userが任意の統計commandへ `--json` を指定する
- **THEN** stdout全体は単独の有効なJSON documentとなり、`source`、`agents`、既存の集計fieldを含み、warningや進捗messageを含まない

### Requirement: 同一入力から決定的な結果を生成する

システムは同じhistory source、同じsource固有scope、同じfilter、および同じ基準時刻に対して、入力の列挙順やAgentの発見順に依存しない同一の件数、Agent一覧、row順序、およびJSON値を生成しなければならない（SHALL）。同じterminal capability、幅、およびcolor modeを与えたhuman-readable reportはbyte単位で決定的でなければならない（SHALL）。

#### Scenario: file列挙順が変わる

- **WHEN** 同一内容の履歴fileが異なる順序で列挙される
- **THEN** table・JSONのrow順序と集計値は変化しない

#### Scenario: terminal capabilityを固定する

- **WHEN** 同じ集計resultを同じ幅、color profile、TTY状態、基準時刻で複数回描画する
- **THEN** human-readable reportの出力byte列は一致する

#### Scenario: ctxのAgent発見順が変わる

- **WHEN** 同じctx generationのイベントが異なるAgent順でadapterへ渡される
- **THEN** reportのAgent一覧、集計値、table・JSONのrow順序は一致する

#### Scenario: sourceとfilterを固定する

- **WHEN** 同じsource、scope、filter、基準時刻、およびterminal capabilityで複数回reportを生成する
- **THEN** human-readable reportとJSONの結果は一致する
