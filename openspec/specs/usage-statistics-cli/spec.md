# usage-statistics-cli Specification

## Purpose

Codexのsession・user prompt・Tool・Skill利用を、system logではなく集計条件と要点が伝わるstatic reportとして端末へ表示し、同じ内容をscriptや分析toolから再利用できる軽量CLIとして提供する。

## Requirements

### Requirement: overview統計を表示する
`catsift stats` は内容を示す `USAGE OVERVIEW` headingに続けて、入力source、対象Agent一覧、および期間をlabel付きcontext行として表示し、Sessions、Turns、User Prompts、Tool Calls、Skill Uses (turn)、およびSkill Uses (session)を視覚的に区切られたsummaryとして表示しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、6つの集計値は選択scope内の全Agentを合算しなければならない（SHALL）。グローバルな実行ファイル名または製品名だけのtitle（例: `CatSift`、`catsift stats`）をreportのheadingとして表示してはならない（MUST NOT）。Tool Callsはeffective Tool view、Skill Uses (turn)はturn単位、Skill Uses (session)はsession単位で重複排除した全確認状態の利用を使用しなければならない（SHALL）。`stats` はSkill groupingを単一選択する `--group-by` optionを受け付けてはならない（MUST NOT）。

#### Scenario: 履歴が存在する

- **WHEN** userが有効なCodex履歴に対して `catsift stats` を実行する
- **THEN** システムは `USAGE OVERVIEW` heading、`Source: Codex (~/.codex)`、`Agents: Codex`、対象期間、および6つの集計値を既定のhuman-readable report形式でstdoutへ出力する

#### Scenario: ctx sourceに複数Agentの履歴が存在する

- **WHEN** userが `catsift stats --source ctx` を実行し、ctxの選択scopeにCodexとOpenCodeの履歴がある
- **THEN** システムは `Agents: Codex, OpenCode` を表示し、両AgentのSessions、Turns、User Prompts、Tool Calls、Skill Uses (turn)、およびSkill Uses (session)を合算して出力する

#### Scenario: sourceと入力pathをcompactに表示する

- **WHEN** userが `catsift stats --source ctx --ctx-data-root /path/to/ctx-data` を実行する
- **THEN** システムは `Source: ctx (/path/to/ctx-data)` を1行で表示し、別の `History` または `Data root` context行を追加しない

#### Scenario: 対象履歴が空である

- **WHEN** 読取対象に有効な利用Eventが存在しない
- **THEN** システムは `USAGE OVERVIEW` headingと対象Agent・選択期間のcontextを表示し、各集計値を0として出力するとともに、選択期間に利用がないことを説明するempty-state messageを表示して0で終了する

### Requirement: Tool統計を表示する
`catsift tools` は `TOOL USAGE` headingに続けて、入力source、対象Agent一覧、選択中の期間、およびlayerをそれぞれlabel付きのcontext行として表示し、canonical Tool名ごとのCalls、Failures、およびLast Usedを見出し付きtableとして表示しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、同じcanonical Tool名の観測はAgentをまたいで合算しなければならない（SHALL）。footerは対象Tool数と総Callsをdomain用語で表示し、`Rows`という実装用語や中点区切りを使用してはならない（MUST NOT）。既定ではeffective layerを集計し、`--layer effective|runtime|model` により集計layerを選択できなければならない（SHALL）。

#### Scenario: 既定のTool統計を表示する

- **WHEN** userがlayer指定なしで `catsift tools` を実行する
- **THEN** システムは `TOOL USAGE` heading、Source・Agents・Period・Layerのcontext、effective Tool利用をCalls降順、同数の場合はTool名昇順で出力する

#### Scenario: 複数Agentの同じcanonical Toolを合算する

- **WHEN** ctxのCodexとOpenCodeがそれぞれcanonical Tool `shell` を利用している
- **THEN** システムはTool tableに `shell` を1rowだけ出力し、CallsとFailuresを両Agent分合算する

#### Scenario: Tool footerを表示する

- **WHEN** userが対象Toolのあるhuman-readable `catsift tools` reportを表示する
- **THEN** システムはfooterに `N tools, M calls total` 相当の、対象Tool数と総Callsが分かる表現を表示する。1件の場合は `tool` と `call`、0件の場合は `tools` と `calls` のように自然な単数・複数形を使用する

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
- **THEN** システムはfooterに `N skills, M uses total` 相当の、対象Skill数と総Usesが分かる表現を表示する。1件の場合は `skill` と `use`、0件の場合は `skills` と `uses` のように自然な単数・複数形を使用する

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
- **WHEN** userが `catsift skills --days 30` を実行する
- **THEN** システムはcutoff以後の観測だけからSkill統計を生成する

#### Scenario: 別のCodex homeを集計する
- **WHEN** userが `catsift stats --codex-home /tmp/codex-home` を実行する
- **THEN** システムは指定先だけをsourceとしてoverviewを生成する

### Requirement: human-readable report・JSONを提供する
各統計commandは既定で、report内容を示すheading、入力source、対象Agent一覧、適用中のfilterをlabel付きcontext行、明確なsectionまたはcolumn heading、整列した値、および必要なfooterを持つhuman-readable static reportを出力しなければならない（SHALL）。human-readable reportの`Source`はsourceのdisplay nameを表示し、Codexでは有効なCodex homeを括弧内へ、ctxでは明示された`--ctx-data-root`だけを括弧内へ表示しなければならない（SHALL）。source pathは`Source`行へ含め、別の`History`または`Data root` context行を追加してはならない（MUST NOT）。複数Agentの表示はcanonical IDの決定的な順序に対応するdisplay nameをcomma区切りで示し、context行を中点で連結してはならない（MUST NOT）。countは桁区切りして右揃えにし、tableのLast Usedはtimezoneを含む簡潔なlocal日時で表示しなければならない（SHALL）。`--json` でmachine-readable出力へ切り替えられなければならず（SHALL）、JSONはhuman-readable reportと同じfilter・集計結果を表し、`source` とcanonical Agent IDの `agents` arrayを含まなければならない（SHALL）。既存の `agent` string fieldは後方互換のため保持し、単一Agentでは従来の値、複数Agentではcanonical IDを決定的順序でcomma区切りした値を格納しなければならない（SHALL）。JSONのfield順、timestamp形式、およびwarningをstdoutへ混入させない規則を維持しなければならない（SHALL）。

`Period`は`last`、`from`、`through`などのoption入力をそのまま表示せず、実際に集計へ含まれたレコードの最初の日から最後の日までを、常に`YYYY-MM-DD to YYYY-MM-DD`形式で表示しなければならない（SHALL）。対象レコードがない場合は`no data`と表示しなければならない（SHALL）。

指定された期間が実際に利用データの存在する期間と一致しない場合、human-readable reportは`info:`メッセージでその不一致を説明しなければならない（SHALL）。指定期間に利用がない場合も、empty-state messageを`info: No usage found for the selected period.`として表示しなければならない（SHALL）。これは入力errorやデータ欠損を示すwarningではない（MUST NOT）。

Token usageをhuman-readable reportへ表示する場合、`Total Tokens`を親として`Input Tokens`と`Output Tokens`を階層表示し、`Cached Tokens`および非zeroの`Cache Write Input Tokens`をInputの内訳、`Reasoning Tokens`をOutputの内訳として表示しなければならない（SHALL）。token数は読みやすい`K`、`M`、`B`などのcompact表記を使用し、zeroの`Cache Write Input Tokens`はhuman-readable reportへ表示してはならない（MUST NOT）。JSONでは既存のtoken usage fieldと正確な値を保持しなければならない（SHALL）。

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

#### Scenario: token usageを階層とcompact表記で表示する

- **WHEN** provider token usageが利用可能な`stats` reportをhuman-readable modeで表示し、cache writeがzeroである
- **THEN** システムは`Total Tokens`、Input/Output、および各内訳を階層表示し、token数をcompact表記で出力し、`Cache Write Input Tokens`を表示しない。`--json`では正確なtoken usage fieldを保持する

### Requirement: terminal capabilityに応じて安全に装飾する
既定の `--color auto` では、システムはstdoutがTTYであり `NO_COLOR` が設定されていない場合だけhuman-readable reportへANSI styleを適用しなければならない（SHALL）。`--color always` は明示的にstyleを有効化し、`--color never` は無効化しなければならず、この2つの強制modeは `NO_COLOR` より優先しなければならない（SHALL）。JSONにはoptionやterminal状態にかかわらずANSI escape sequenceを含めてはならない（MUST NOT）。色は補助表現に限り、label、text、数値、または配置なしに意味を伝えてはならない（MUST NOT）。colorが有効なhuman-readable reportでは、report headingを共通のaccent styleで目立たせ、table headerをheadingと区別できるstyleで表示しなければならない（SHALL）。通常のlabel・table row・count・Failures・Skillのevidence status値は既定色を基本とし、table cellをerror colorやwarning colorで強調してはならない（MUST NOT）。通常状態を示すためだけの一律なsuccess colorを全rowへ適用してはならない（MUST NOT）。

#### Scenario: TTYへ既定reportを出力する

- **WHEN** stdoutがcolor対応TTYで、`--color` が未指定か `auto` であり、`NO_COLOR` が設定されていない
- **THEN** システムはreport headingとtable headerを補助styleで判別しやすく表示する

#### Scenario: reportをredirectする

- **WHEN** stdoutがTTYでなく、`--color` が未指定か `auto` である
- **THEN** システムはANSI escape sequenceを含まない、整列したplain-text reportを出力する

#### Scenario: NO_COLORを尊重する

- **WHEN** `NO_COLOR` が設定され、`--color` が明示されていない
- **THEN** システムはhuman-readable reportへANSI styleを適用しない

#### Scenario: colorを明示指定する

- **WHEN** userが `--color always` または `--color never` を指定する
- **THEN** システムはTTY判定や `NO_COLOR` より明示指定を優先してstyleを有効化または無効化する

#### Scenario: machine-readable出力でcolorを強制する

- **WHEN** userが `--json --color always` を指定する
- **THEN** システムはANSIを含まない有効なJSONを出力する

### Requirement: terminal幅に応じてreportをcompact化する
human-readable reportは取得可能なterminal幅へ収まるようにlayoutを選択しなければならない（SHALL）。狭い幅では補助的な内訳columnを省略またはcompact化できるが、report内容を示すheading、対象Agent・期間を示すcontext、row identity、および主要countを保持しなければならない（SHALL）。長いnameはcolumn境界内で省略記号付きに短縮し、異なるrowの値に見える折返しをしてはならない（MUST NOT）。

#### Scenario: 十分に広いterminalで表示する

- **WHEN** terminal幅が全columnを表示できる
- **THEN** システムはToolまたはSkillの定義済み内訳とLast Usedを含むwide layoutを表示する

#### Scenario: 狭いterminalで表示する

- **WHEN** terminal幅がwide layoutに不足する
- **THEN** システムはreport heading、Agent・Periodのcontext、主要nameとcountを残したcompact layoutを表示し、table rowを誤解を招く形で折り返さない

### Requirement: 空結果を行動可能なmessageで示す
`tools` または `skills` の対象rowが0件の場合、human-readable reportは空tableだけを表示せず、対象期間に該当利用がないことと適用中のfilterを説明しなければならない（SHALL）。JSONは空arrayを出力しなければならない（SHALL）。

#### Scenario: 期間内にSkill利用がない
- **WHEN** userが `catsift skills --days 1` を実行し、対象Skill利用が0件である
- **THEN** human-readable reportは選択期間にSkill利用が見つからないことを説明して0で終了する

### Requirement: CLI errorとwarningを分離する
無効なcommand・option・値、または必須sourceを解決できない場合、システムは簡潔なerrorをstderrへ出力して非0で終了しなければならない（SHALL）。一部の履歴だけをskipして有効な結果を生成できる場合は、結果をstdout、warning要約をstderrへ出力して0で終了しなければならない（SHALL）。

#### Scenario: 未知のcommandを指定する
- **WHEN** userが `catsift unknown` を実行する
- **THEN** システムは利用可能なcommandを示すerrorをstderrへ出力し、非0で終了する

#### Scenario: machine-readable出力中にwarningが発生する
- **WHEN** `--json` の集計中に一部recordがskipされる
- **THEN** stdoutは有効なJSON documentのままであり、warning要約はstderrだけに出力される

### Requirement: 同一入力から決定的な結果を生成する
システムは同じhistory source、同じsource固有scope、同じfilter、および同じ基準時刻に対して、入力の列挙順やAgentの発見順に依存しない同一の件数、Agent一覧、row順序、およびJSON値を生成しなければならない（SHALL）。同じterminal capability、幅、およびcolor modeを与えたhuman-readable reportはbyte単位で決定的でなければならない（SHALL）。

#### Scenario: file列挙順が変わる
- **WHEN** 同一内容の履歴fileが異なる順序で列挙される
- **THEN** table・JSONのrow順序と集計値は変化しない

#### Scenario: ctxのAgent発見順が変わる

- **WHEN** 同じctx generationのイベントが異なるAgent順でadapterへ渡される
- **THEN** reportのAgent一覧、集計値、table・JSONのrow順序は一致する

#### Scenario: sourceとfilterを固定する

- **WHEN** 同じsource、scope、filter、基準時刻、およびterminal capabilityで複数回reportを生成する
- **THEN** human-readable reportとJSONの結果は一致する

#### Scenario: terminal capabilityを固定する
- **WHEN** 同じ集計resultを同じ幅、color profile、TTY状態、基準時刻で複数回描画する
- **THEN** human-readable reportの出力byte列は一致する

### Requirement: skills commandでunused viewを選択できる

`catsift skills` は `--unused` optionを受け付け、指定された場合は通常のSkill利用集計ではなく、`unused-skill-report` capabilityで定義された未使用Skillreportを生成しなければならない（SHALL）。`--unused` がない場合の `catsift skills` の集計結果とoutput形式は変更してはならない（MUST NOT）。`--root` は `--unused` と同時に指定された場合だけ有効で、repeatableに受け付けなければならない（SHALL）。

#### Scenario: unused viewを明示的に選択する

- **WHEN** userが `catsift skills --unused` を実行する
- **THEN** システムは通常の利用rowではなく、選択されたscopeの未使用Skillだけをreportする

#### Scenario: rootを複数指定する

- **WHEN** userが `catsift skills --unused --root ~/src --root ~/work` を実行する
- **THEN** システムは2つのrootをscopeとして未使用Skillを判定する

#### Scenario: rootをunused viewなしで指定する

- **WHEN** userが `catsift skills --root ~/src` を実行する
- **THEN** システムは `--root` が `--unused` と共にだけ使えることを示す引数errorをstderrへ出力し、非0で終了する

#### Scenario: 他の統計commandへunused optionを指定する

- **WHEN** userが `catsift stats --unused` または `catsift tools --unused` を実行する
- **THEN** システムはoption errorをstderrへ出力し、既存のstatsまたはtools reportを生成しない

### Requirement: unused viewが既存の履歴filterとoutput optionを継承する

`skills --unused` は `--days`、`--codex-home`、`--strict`、`--group-by`、`--color`、`--json`、`--verbose`、および `--strict-input` を既存の `skills` commandと同じvalidation、履歴source、warning、終了codeの規則で受け付けなければならない（SHALL）。`--codex-home` は履歴sourceだけを変更し、Skill inventoryの既定scopeを暗黙に変更してはならない（MUST NOT）。

#### Scenario: days filterをunused判定へ適用する

- **WHEN** userが `catsift skills --unused --days 30` を実行する
- **THEN** システムは直近30日間の履歴だけを使用済み判定へ使い、report contextにもその期間を示す

#### Scenario: strict filterをunused判定へ適用する

- **WHEN** userが `catsift skills --unused --strict` を実行する
- **THEN** システムは `confirmed` の履歴だけを使用済みとして扱う

#### Scenario: JSON outputのwarningを分離する

- **WHEN** `--json` で履歴の一部をskipするwarningが発生する
- **THEN** stdoutは有効なJSONのまま、warning要約はstderrだけに出力され、既存の `--strict-input` 規則が適用される

### Requirement: Usage Explorerを既定入口として提供する

interactive terminalでcommandを省略した`catsift`は、既存のhistory source選択、期間、source固有root、cache、およびwarningの規則を継承してTUI Usage Explorerを起動しなければならない（SHALL）。Usage Explorerは履歴をread-onlyで扱い、既存の`stats`、`tools`、`skills`を置き換えてはならない（MUST NOT）。

CLIとTUIは同じnormalized data、scope filter、warning、および集計意味論を使用しなければならない（SHALL）。ただしCLIはstaticなsummaryとmachine-readable output、TUIは一覧からdetailへ辿るinteractive viewとして、それぞれの用途に適した表示量を提供してよい（MAY）。

#### Scenario: commandなしでUsage Explorerを起動する

- **WHEN** userがinteractive terminalで `catsift --source codex --days 30` を実行する
- **THEN** システムは既存のCodex sourceと30日間のfilterを使ってUsage Explorerを起動し、Overviewを表示する

#### Scenario: Codex履歴でUsage Explorerを起動する

- **WHEN** userが `catsift --source codex --days 30` を実行する
- **THEN** システムは既存のCodex sourceと30日間のfilterを使ってTUIを起動し、Overviewを表示する

#### Scenario: ctx rootを指定してUsage Explorerを起動する

- **WHEN** userが `catsift --source ctx --ctx-data-root /path/to/ctx` を実行する
- **THEN** システムは指定されたctx data rootだけを読み込み、ctxのsource/Agent contextをTUIへ表示する

#### Scenario: 既存static commandを実行する

- **WHEN** userが `stats`、`tools`、または`skills`を既存optionで実行する
- **THEN** システムはTUIの追加によって既存のhuman-readable/JSON reportのfield、集計、終了規則を変更しない

### Requirement: TUIの出力形式とterminal境界を検証する

Usage Explorerはinteractive terminal向けの出力だけを提供し、`--json`を指定された場合はTUIを起動せずoption errorをstderrへ出力して非0で終了しなければならない（SHALL）。非TTYでcommandなしの`catsift`が起動された場合は、履歴内容やstack traceをstdoutへ漏らさず、interactive terminalが必要であることをstderrへ示して非0で終了しなければならない（SHALL）。

#### Scenario: commandなしでJSONを指定する

- **WHEN** userが `catsift --json` を実行する
- **THEN** システムはTUIを起動せず、`--json`がUsage Explorerでは利用できないことをstderrへ出力し、非0で終了する

#### Scenario: 削除したtui commandを指定する

- **WHEN** userが `catsift tui` を実行する
- **THEN** システムはTUIを起動せず、unknown command errorをstderrへ出力して非0で終了する

#### Scenario: commandなしで非TTYからUsage Explorerを起動する

- **WHEN** stdoutまたはstdinがinteractive terminalではない状態で `catsift --source codex` を実行する
- **THEN** システムはTUI描画や履歴内容をstdoutへ出力せず、interactive terminalが必要であることをstderrへ示して非0で終了する

### Requirement: TUIのwarningとsource errorを既存規則で扱う

一部recordをskipしてTUI用の有効なsnapshotを生成できる場合、システムはTUIのstatusまたはdiagnosticsへwarningを表示し、起動を継続しなければならない（SHALL）。必須sourceを解決できない、optionが無効、またはsnapshotを生成できない場合は、システムは簡潔なerrorをstderrへ出力してTUIを起動せず非0で終了しなければならない（SHALL）。

#### Scenario: recoverable warningがある

- **WHEN** 一部履歴のparseをskipして残りから有効な結果を生成できる
- **THEN** システムはTUIを起動し、warning countまたはdiagnosticsを確認できる状態にする

#### Scenario: sourceを解決できない

- **WHEN** 指定されたsource rootが存在しない、または読み込み不能である
- **THEN** システムはTUIを起動せず、errorをstderrへ出力して非0で終了する
