## MODIFIED Requirements

### Requirement: 期間とhistory sourceを各統計commandで指定できる

`stats`、`tools`、`skills`は共通して`--source codex|ctx|opencode`、`--days`、sourceに応じた`--codex-home`、`--ctx-data-root`、または`--opencode-home`、および`--color auto|always|never`を受け付けなければならない（SHALL）。`--source`の既定値は`codex`でなければならず（SHALL）、`--codex-home`はCodex source、`--ctx-data-root`はctx source、`--opencode-home`はOpenCode sourceでのみ有効でなければならない（MUST）。同一実行でCodex、ctx、およびOpenCodeを複数同時に入力へ含めてはならない（MUST NOT）。filterとsource解決はすべての出力形式で同じ結果集合へ適用しなければならない（SHALL）。

#### Scenario: source未指定時はCodexを使用する

- **WHEN** userが`agentstats stats`を実行する
- **THEN** システムは既存のCodex home解決規則に従ってCodexだけを入力sourceにする

#### Scenario: ctx sourceとdata rootを指定する

- **WHEN** userが`agentstats stats --source ctx --ctx-data-root /path/to/ctx`を実行する
- **THEN** システムは指定されたctx data rootだけを入力scopeとして使用する

#### Scenario: OpenCode sourceとhomeを指定する

- **WHEN** userが`agentstats stats --source opencode --opencode-home /path/to/opencode`を実行する
- **THEN** システムは指定されたOpenCode data rootだけを入力scopeとして使用する

#### Scenario: source固有optionを誤って組み合わせる

- **WHEN** userが`--source ctx --codex-home /path`、`--source codex --ctx-data-root /path`、または`--source opencode --ctx-data-root /path`を指定する
- **THEN** システムはoption errorをstderrへ出力し、reportを生成せず非0で終了する

#### Scenario: Codexとctxを同時に指定する

- **WHEN** userが複数のhistory sourceを同時に指定しようとする
- **THEN** システムはsource選択を拒否し、複数sourceを合算したreportを生成しない

#### Scenario: 30日分のSkill統計を取得する

- **WHEN** userが`agentstats skills --days 30`を実行する
- **THEN** システムはcutoff以後の観測だけからSkill統計を生成する

#### Scenario: 別のCodex homeを集計する

- **WHEN** userが`agentstats stats --codex-home /tmp/codex-home`を実行する
- **THEN** システムは指定先だけをsourceとしてoverviewを生成する

### Requirement: human-readable report・JSONを提供する

各統計commandは既定で、report内容を示すheading、入力source、対象Agent一覧、適用中のfilterをlabel付きcontext行、明確なsectionまたはcolumn heading、整列した値、および必要なfooterを持つhuman-readable static reportを出力しなければならない（SHALL）。human-readable reportの`Source`はsourceのdisplay nameを表示し、Codexでは有効なCodex homeを、ctxでは明示された`--ctx-data-root`を、OpenCodeでは有効なOpenCode data rootを括弧内へ表示しなければならない（SHALL）。source pathは`Source`行へ含め、別の`History`または`Data root` context行を追加してはならない（MUST NOT）。複数Agentの表示はcanonical IDの決定的な順序に対応するdisplay nameをcomma区切りで示し、context行を中点で連結してはならない（MUST NOT）。countは桁区切りして右揃えにし、tableのLast Usedはtimezoneを含む簡潔なlocal日時で表示しなければならない（SHALL）。`--json`でmachine-readable出力へ切り替えられなければならず（SHALL）、JSONはhuman-readable reportと同じfilter・集計結果を表し、`source`とcanonical Agent IDの`agents` arrayを含まなければならない（SHALL）。既存の`agent` string fieldは後方互換のため保持し、単一Agentでは従来の値、複数Agentではcanonical IDを決定的順序でcomma区切りした値を格納しなければならない（SHALL）。JSONのfield順、timestamp形式、およびwarningをstdoutへ混入させない規則を維持しなければならない（SHALL）。

`Period`は`last`、`from`、`through`などのoption入力をそのまま表示せず、実際に集計へ含まれたレコードの最初の日から最後の日までを、常に`YYYY-MM-DD to YYYY-MM-DD`形式で表示しなければならない（SHALL）。対象レコードがない場合は`no data`と表示しなければならない（SHALL）。

指定された期間が実際に利用データの存在する期間と一致しない場合、human-readable reportは`info:`メッセージでその不一致を説明しなければならない（SHALL）。指定期間に利用がない場合も、empty-state messageを`info: No usage found for the selected period.`として表示しなければならない（SHALL）。これは入力errorやデータ欠損を示すwarningではない（MUST NOT）。

Token usageをhuman-readable reportへ表示する場合、`Total Tokens`を親として`Input Tokens`と`Output Tokens`を階層表示し、`Cached Tokens`および非zeroの`Cache Write Input Tokens`をInputの内訳、`Reasoning Tokens`をOutputの内訳として表示しなければならない（SHALL）。token数は読みやすい`K`、`M`、`B`などのcompact表記を使用し、zeroの`Cache Write Input Tokens`はhuman-readable reportへ表示してはならない（MUST NOT）。JSONでは既存のtoken usage fieldと正確な値を保持しなければならない（SHALL）。

#### Scenario: 既定reportを表示する

- **WHEN** userが出力形式を指定せず任意の統計commandを実行する
- **THEN** システムはreport内容を示すheading、入力source、対象Agentとfilterのcontext、指標名と値の関係を一読できるsummaryまたはtable、およびdomain用語で表現されたfooterを持つstatic reportをstdoutへ出力する

#### Scenario: contextを読みやすく表示する

- **WHEN** userが`agentstats tools`または`agentstats skills`を実行する
- **THEN** システムは`Source: ...`、`Agents: ...`、`Period: ...`、およびcommand固有のfilterを別々のlabel付き行へ出力し、項目間の区切りに中点や実装用の`Rows`を使用しない

#### Scenario: 複数Agentのhuman-readable reportを表示する

- **WHEN** ctx sourceのscopeにCodexとOpenCodeが含まれる
- **THEN** システムは`Source: ctx`と`Agents: Codex, OpenCode`を別々のcontext行へ出力する。`--ctx-data-root`が指定された場合はSource行を`Source: ctx (/path/to/ctx-data)`とする

#### Scenario: OpenCode sourceのhuman-readable reportを表示する

- **WHEN** userが`agentstats stats --source opencode --opencode-home /path/to/opencode`を実行する
- **THEN** システムは`Source: OpenCode (/path/to/opencode)`、`Agents: OpenCode`、対象期間、およびOpenCodeから集計した値を表示する

#### Scenario: JSONを出力する

- **WHEN** userが任意の統計commandへ`--json`を指定する
- **THEN** stdout全体は単独の有効なJSON documentとなり、`source`、`agents`、既存の集計fieldを含み、warningや進捗messageを含まない

#### Scenario: token usageを階層とcompact表記で表示する

- **WHEN** provider token usageが利用可能な`stats` reportをhuman-readable modeで表示し、cache writeがzeroである
- **THEN** システムは`Total Tokens`、Input/Output、および各内訳を階層表示し、token数をcompact表記で出力し、`Cache Write Input Tokens`を表示しない。`--json`では正確なtoken usage fieldを保持する

### Requirement: unused viewが既存の履歴filterとoutput optionを継承する

`skills --unused`は`--days`、`--codex-home`、`--ctx-data-root`、`--opencode-home`、`--strict`、`--group-by`、`--color`、`--json`、`--verbose`、および`--strict-input`を既存の`skills` commandと同じvalidation、履歴source、warning、終了codeの規則で受け付けなければならない（SHALL）。`--codex-home`、`--ctx-data-root`、および`--opencode-home`は履歴sourceだけを変更し、Skill inventoryの既定scopeを暗黙に変更してはならない（MUST NOT）。

#### Scenario: days filterをunused判定へ適用する

- **WHEN** userが`agentstats skills --unused --days 30`を実行する
- **THEN** システムは直近30日間の履歴だけを使用済み判定へ使い、report contextにもその期間を示す

#### Scenario: strict filterをunused判定へ適用する

- **WHEN** userが`agentstats skills --unused --strict`を実行する
- **THEN** システムは`confirmed`の履歴だけを使用済みとして扱う

#### Scenario: JSON outputのwarningを分離する

- **WHEN** `--json`で履歴の一部をskipするwarningが発生する
- **THEN** stdoutは有効なJSONのまま、warning要約はstderrだけに出力され、既存の`--strict-input`規則が適用される

#### Scenario: OpenCode sourceでunused viewを使う

- **WHEN** userが`agentstats skills --source opencode --opencode-home /path/to/opencode --unused`を実行する
- **THEN** システムは指定されたOpenCode履歴だけを使用済み判定へ使い、inventoryの既定scopeは変更しない
