# usage-statistics-cli Specification

## Purpose

Codexのsession・user prompt・Tool・Skill利用を、system logではなく集計条件と要点が伝わるstatic reportとして端末へ表示し、同じ内容をscriptや分析toolから再利用できる軽量CLIとして提供する。

## Requirements

### Requirement: overview統計を表示する
`catsift stats` は入力source、対象Agent一覧、実際の対象期間、およびSessions、Turns、User Prompts、Tool Calls、Skill Uses (turn)、Skill Uses (session)を既存JSON fieldへ出力しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、6つの集計値は選択scope内の全Agentを合算しなければならない（SHALL）。stdoutへtext headingを追加してはならない（MUST NOT）。Tool Callsはeffective Tool view、Skill Uses (turn)はturn単位、Skill Uses (session)はsession単位で重複排除した全確認状態の利用を使用しなければならない（SHALL）。`stats` はSkill groupingを単一選択する `--group-by` optionを受け付けてはならない（MUST NOT）。

#### Scenario: 履歴が存在する

- **WHEN** userが有効なCodex履歴に対して `catsift stats` を実行する
- **THEN** システムはCodexのsource・agents、対象期間、および6つの集計値を既定のJSON形式でstdoutへ出力する

#### Scenario: ctx sourceに複数Agentの履歴が存在する

- **WHEN** userが `catsift stats --source ctx` を実行し、ctxの選択scopeにCodexとOpenCodeの履歴がある
- **THEN** システムは canonical agentsをJSONに含め、両AgentのSessions、Turns、User Prompts、Tool Calls、Skill Uses (turn)、およびSkill Uses (session)を合算して出力する

#### Scenario: 対象履歴が空である

- **WHEN** 読取対象に有効な利用Eventが存在しない
- **THEN** システムは対象source・AgentとperiodをJSONで表示し、各集計値を0として出力して0で終了する

### Requirement: Tool統計を表示する
`catsift tools` は入力source、対象Agent一覧、対象期間、layer、およびcanonical Tool名ごとのCalls、Failures、Last Usedを既存JSON fieldへ出力しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、同じcanonical Tool名の観測はAgentをまたいで合算しなければならない（SHALL）。text footerを出力してはならない（MUST NOT）。既定ではeffective layerを集計し、`--layer effective|runtime|model` により集計layerを選択できなければならない（SHALL）。

#### Scenario: 既定のTool統計を表示する

- **WHEN** userがlayer指定なしで `catsift tools` を実行する
- **THEN** システムは Source・Agents・Period・LayerのJSON context、effective Tool利用をCalls降順、同数の場合はTool名昇順で出力する

#### Scenario: 複数Agentの同じcanonical Toolを合算する

- **WHEN** ctxのCodexとOpenCodeがそれぞれcanonical Tool `shell` を利用している
- **THEN** システムはJSON rowsに `shell` を1rowだけ出力し、CallsとFailuresを両Agent分合算する

#### Scenario: Tool footerを表示する

- **WHEN** userが対象Toolのある`catsift tools` reportを表示する
- **THEN** システムはtext footerを出力せず、JSON rowsのCallsとFailuresを保持する

#### Scenario: model layerを指定する

- **WHEN** userが `catsift tools --layer model` を実行する
- **THEN** システムはruntime actionではなくmodel layerのTool callだけを集計する

#### Scenario: 無効なlayerを指定する

- **WHEN** userが定義外の `--layer` 値を指定する
- **THEN** システムは引数errorをstderrへ出力し、非0の終了codeを返す

### Requirement: Skill統計を表示する
`catsift skills` は入力source、対象Agent一覧、対象期間、grouping、およびstrict状態をJSON contextへ含め、Skill名ごとのExplicit、Implicit、Confirmed、Inferred、Unconfirmed、Total、およびLast Usedを集計しなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合、同じcanonical Skill名の利用はAgentをまたいで合算しなければならない（SHALL）。text heading・table・footerをstdoutへ追加してはならない（MUST NOT）。JSONはすべての集計fieldを保持しなければならない（SHALL）。`--strict` が指定された場合はstateが `confirmed` の利用だけをTotalと各mode集計の対象にしなければならない（SHALL）。

#### Scenario: 全Skill観測を表示する

- **WHEN** userが `catsift skills` を実行する
- **THEN** システムは Source・Agents・Period・Group by・StrictのJSON context、全確認状態のSkill利用をTotal降順、同数の場合はSkill名昇順で出力する

#### Scenario: 複数Agentの同じSkillを合算する

- **WHEN** ctxのCodexとOpenCodeが同じcanonical Skill `review` を利用している
- **THEN** システムはJSON rowsに `review` を1rowだけ出力し、Agentをまたぐ利用を合算する

#### Scenario: Skill footerを表示する

- **WHEN** userが対象Skillのある`catsift skills` reportを表示する
- **THEN** システムはtext footerを出力せず、JSON rowsの全集計fieldを保持する

#### Scenario: strict modeを使用する

- **WHEN** userが `catsift skills --strict` を実行する
- **THEN** システムは `confirmed` でないSkill利用を件数とLast Usedの算出から除外する

#### Scenario: 同一利用に複数の証拠がある

- **WHEN** 1回の重複排除済みSkill利用が複数の検出方式を持つ
- **THEN** システムはその利用をTotalへ1回だけ加算する

### Requirement: 期間とhistory sourceを各統計commandで指定できる

`stats`、`tools`、`skills`は共通して`--source codex|ctx|opencode|copilot`、`--days`、および`--color auto|always|never`を受け付けなければならない（SHALL）。`--source`の既定値は`codex`でなければならず（SHALL）。同一実行でCodex、ctx、OpenCode、およびGitHub Copilotを複数同時に入力へ含めてはならない（MUST NOT）。filterとsource解決はすべての出力形式で同じ結果集合へ適用しなければならない（SHALL）。

#### Scenario: source未指定時はCodexを使用する

- **WHEN** userが `catsift stats` を実行する
- **THEN** システムは既存のCodex home解決規則に従ってCodexだけを入力sourceにする

#### Scenario: GitHub Copilot sourceを明示する

- **WHEN** userが `catsift stats --source copilot`、`catsift tools --source copilot`、または`catsift skills --source copilot`を実行する
- **THEN** システムはGitHub Copilot sourceだけを入力scopeとして使用し、他sourceの履歴を暗黙に合算しない

#### Scenario: Codexとctxを同時に指定する

- **WHEN** userが複数のhistory sourceを同時に指定しようとする
- **THEN** システムはsource選択を拒否し、Codex、ctx、OpenCode、またはGitHub Copilotを合算したreportを生成しない

#### Scenario: 30日分のSkill統計を取得する

- **WHEN** userが任意のsourceで `catsift skills --days 30` を実行する
- **THEN** システムは選択されたsourceのcutoff以後の観測だけからSkill統計を生成する

#### Scenario: GitHub Copilotと別sourceを同時に指定する

- **WHEN** userがGitHub CopilotとCodex、ctx、またはOpenCodeを同時に入力へ指定しようとする
- **THEN** システムはsource選択を拒否し、異なるsourceを合算したreportを生成しない

#### Scenario: 30日分のGitHub Copilot Skill統計を取得する

- **WHEN** userが `catsift skills --source copilot --days 30` を実行する
- **THEN** システムはGitHub Copilot eventのcutoff以後の観測だけからSkill統計を生成する

### Requirement: human-readable report・JSONを提供する

すべてのreport subcommand（stats/activity/models/tools/skills/sessions、unusedとdetailを含む）は既定で単独のJSON documentをstdoutへ出力しなければならない（SHALL）。static human report modeは提供してはならない（MUST NOT）。既存のJSON schema、field順、集計、filter、timestamp、warningと終了codeの規則を維持しなければならない（SHALL）。`agent`互換fieldとcanonical `agents` arrayを維持し、periodは実際の最初と最後の日付（YYYY-MM-DD to YYYY-MM-DD）、空ならno dataとし、token usageは正確な数値を保持しなければならない（SHALL）。人向けの探索は引数なしのTUIを使用しなければならない（SHALL）。

#### Scenario: 既定reportを表示する
- **WHEN** userが任意のreportまたはdetailを出力形式指定なしで実行する
- **THEN** stdoutは有効なJSONだけを含み、既存のsource、agents、periodおよび集計fieldを保持する

#### Scenario: 診断とJSONを分離する
- **WHEN** warningやstrict-inputによる失敗が発生する
- **THEN** reportのJSONをstdoutへ出力し、診断はstderrへ出力する

#### Scenario: contextを読みやすく表示する
- **WHEN** reportを実行する
- **THEN** contextはhuman labelでなくJSON fieldとして保持する

#### Scenario: 複数Agentのhuman-readable reportを表示する
- **WHEN** 複数Agentのreportを実行する
- **THEN** static human modeは提供せず、agentsをJSONに含める

#### Scenario: OpenCode sourceのhuman-readable reportを表示する
- **WHEN** OpenCodeのreportを実行する
- **THEN** static human modeは提供せず、sourceをJSONに含める

#### Scenario: JSONを出力する
- **WHEN** reportを実行する
- **THEN** 追加flagなしでJSONを出力する

#### Scenario: token usageを階層とcompact表記で表示する
- **WHEN** token usageのあるreportを実行する
- **THEN** JSONの正確な数値を維持し、compact human表記はTUIで扱う

### Requirement: terminal capabilityに応じて安全に装飾する

JSON reportと診断はTTY、NO_COLOR、`--color`の値にかかわらずANSIを含んではならない（MUST NOT）。`--color auto|always|never`はvalidation付きの互換optionとして受け付けなければならない（SHALL）。TUIの装飾は既存のTUI規則に従わなければならない（SHALL）。

#### Scenario: TTYへ既定reportを出力する
- **WHEN** userがreportをTTYまたはredirect先へ出力し、color設定を指定するか省略する
- **THEN** stdoutはANSIを含まないJSONで、診断もplain textを維持する

#### Scenario: reportをredirectする
- **WHEN** userがreportをTTYまたはredirect先へ出力し、color設定を指定するか省略する
- **THEN** stdoutはANSIを含まないJSONで、診断もplain textを維持する

#### Scenario: NO_COLORを尊重する
- **WHEN** userがreportをTTYまたはredirect先へ出力し、color設定を指定するか省略する
- **THEN** stdoutはANSIを含まないJSONで、診断もplain textを維持する

#### Scenario: colorを明示指定する
- **WHEN** userがreportをTTYまたはredirect先へ出力し、color設定を指定するか省略する
- **THEN** stdoutはANSIを含まないJSONで、診断もplain textを維持する

#### Scenario: machine-readable出力でcolorを強制する
- **WHEN** userがreportをTTYまたはredirect先へ出力し、color設定を指定するか省略する
- **THEN** stdoutはANSIを含まないJSONで、診断もplain textを維持する

### Requirement: 空結果を行動可能なmessageで示す
`tools`または`skills`の対象rowが0件の場合、JSONはcontextと空のrows arrayを出力して0で終了しなければならない（SHALL）。人向けのempty-state説明はTUIで提供しなければならない（SHALL）。

#### Scenario: 期間内にSkill利用がない
- **WHEN** userが `catsift skills --days 1` を実行し、対象Skill利用が0件である
- **THEN** JSON reportはrows: []とcontextを出力して0で終了する

### Requirement: CLI errorとwarningを分離する
無効なcommand・option・値、または必須sourceを解決できない場合、システムは簡潔なerrorをstderrへ出力して非0で終了しなければならない（SHALL）。一部の履歴だけをskipして有効な結果を生成できる場合は、結果をstdout、warning要約をstderrへ出力して0で終了しなければならない（SHALL）。

#### Scenario: 未知のcommandを指定する
- **WHEN** userが `catsift unknown` を実行する
- **THEN** システムは利用可能なcommandを示すerrorをstderrへ出力し、非0で終了する

#### Scenario: machine-readable出力中にwarningが発生する
- **WHEN** `--json` の集計中に一部recordがskipされる
- **THEN** stdoutは有効なJSON documentのままであり、warning要約はstderrだけに出力される

### Requirement: 同一入力から決定的な結果を生成する
システムは同じhistory source、同じsource固有scope、同じfilter、および同じ基準時刻に対して、入力の列挙順やAgentの発見順に依存しない同一の件数、Agent一覧、row順序、およびJSON値を生成しなければならない（SHALL）。JSONの値はterminal capability、幅、color modeに依存してはならない（MUST NOT）。

#### Scenario: file列挙順が変わる
- **WHEN** 同一内容の履歴fileが異なる順序で列挙される
- **THEN** table・JSONのrow順序と集計値は変化しない

#### Scenario: ctxのAgent発見順が変わる

- **WHEN** 同じctx generationのイベントが異なるAgent順でadapterへ渡される
- **THEN** reportのAgent一覧、集計値、table・JSONのrow順序は一致する

#### Scenario: sourceとfilterを固定する

- **WHEN** 同じsource、scope、filter、基準時刻、およびterminal capabilityで複数回reportを生成する
- **THEN** JSONの結果は一致する

#### Scenario: terminal capabilityを固定する
- **WHEN** 同じ集計resultを同じ幅、color profile、TTY状態、基準時刻で複数回描画する
- **THEN** JSON reportの出力byte列は一致する

### Requirement: skills commandでunused viewを選択できる

`catsift skills` は `--unused` optionを受け付け、指定された場合は通常のSkill利用集計ではなく、`unused-skill-report` capabilityで定義された未使用Skillreportを生成しなければならない（SHALL）。`--unused`がない場合は通常の利用集計を既定のJSONで出力しなければならない（SHALL）。CLI reportの`--root`は`skills --unused`だけで有効で、TUIもinventory用のrepeatable `--root`を受け付けなければならない（SHALL）。

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

`skills --unused`は`--days`、`--source`、`--strict`、`--group-by`、`--color`、`--json`、`--verbose`、および`--strict-input`を既存の`skills` commandと同じvalidation、履歴source、warning、終了codeの規則で受け付けなければならない（SHALL）。source選択は履歴だけを変更し、Skill inventoryの既定scopeを暗黙に変更してはならない（MUST NOT）。

#### Scenario: days filterをunused判定へ適用する

- **WHEN** userが `catsift skills --unused --days 30` を実行する
- **THEN** システムは直近30日間の履歴だけを使用済み判定へ使い、report contextにもその期間を示す

#### Scenario: strict filterをunused判定へ適用する

- **WHEN** userが `catsift skills --unused --strict` を実行する
- **THEN** システムは `confirmed` の履歴だけを使用済みとして扱う

#### Scenario: JSON outputのwarningを分離する

- **WHEN** `--json` で履歴の一部をskipするwarningが発生する
- **THEN** stdoutは有効なJSONのまま、warning要約はstderrだけに出力され、既存の `--strict-input` 規則が適用される

#### Scenario: OpenCode sourceでunused viewを使う

- **WHEN** userが`catsift skills --source opencode --unused`を実行する
- **THEN** システムは有効なOpenCode履歴だけを使用済み判定へ使い、inventoryの既定scopeは変更しない

### Requirement: Usage Explorerを既定入口として提供する

interactive terminalでcommandを省略した`catsift`は、既存のhistory source選択、cache、およびwarningの規則を継承してTUI Usage Explorerを起動しなければならない（SHALL）。Usage Explorerの期間はTUI上で変更でき、CLI report用の`--days`、`--from`、`--to` optionを受け付けてはならない（MUST NOT）。Usage Explorerは履歴をread-onlyで扱い、既存の`stats`、`tools`、`skills`を置き換えてはならない（MUST NOT）。

#### Scenario: commandなしでUsage Explorerを起動する

- **WHEN** userがinteractive terminalで `catsift --source codex` を実行する
- **THEN** システムは既存のCodex sourceを使ってUsage Explorerを起動し、Overviewを表示する

#### Scenario: TUIで期間をfilterする

- **WHEN** userがUsage Explorerで`d`を押し、`30`を入力して確定する
- **THEN** システムは直近30日間のperiod filterをTUIの全viewへ適用する

#### Scenario: 既存static commandを実行する

- **WHEN** userが `stats`、`tools`、または`skills`を既存optionで実行する
- **THEN** システムは既存のJSON field、集計、終了規則を維持し、既定でJSONを出力する

### Requirement: CLIとTUIで共通の集計と期間表示を提供する

CLIとTUIは同じnormalized data、scope filter、warning、および集計意味論を使用しなければならない（SHALL）。TUIの`Period`は実際に集計されたdata範囲を表示し、`Daily activity`も同じ日付範囲を表示しなければならない（SHALL）。指定されたperiodと一致しない場合は`Input notes`で差分を説明し、period確定直後にはtab直下に`Notice:` messageを表示幅に合わせて折り返し表示してtab移動またはclearまで保持しなければならない（SHALL）。ただしCLIはJSONのsummary/detail、TUIは一覧からdetailへ辿るinteractive viewとして、それぞれの用途に適した表示量を提供してよい（MAY）。

#### Scenario: 同じscopeをCLIとTUIで表示する

- **WHEN** userが同じnormalized dataとscope filterでCLIとTUIを使う
- **THEN** warningと集計意味論は共通となる
- **AND** CLIはJSONのsummary/detail、TUIは一覧からdetailへ辿るinteractive viewとして、それぞれの用途に適した表示量を提供してよい

#### Scenario: 指定periodと実際のdata範囲が異なる

- **WHEN** Usage Explorerでperiodを確定し、実際の集計data範囲が指定periodと一致しない
- **THEN** PeriodとDaily activityは同じ実際のdata範囲を表示し、Input notesは指定periodとの差分を説明する
- **AND** tab直下のNotice: messageは表示幅で折り返され、tab移動またはclearまで保持される

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

#### Scenario: sourceのdata rootを解決できない

- **WHEN** 選択したsourceのdata rootが存在しない、または読み込み不能である
- **THEN** システムはTUIを起動せず、errorをstderrへ出力して非0で終了する

### Requirement: GitHub Copilot sourceをreportとJSONへ反映する

GitHub Copilot sourceを選択した各統計reportは、canonical source `copilot`、canonical Agent ID `copilot`、適用中のperiod、および取得できた集計値を既定のJSON出力規則で表示しなければならない（SHALL）。履歴が空の場合も、既存のempty-stateと終了codeの規則を維持しなければならない（SHALL）。

#### Scenario: GitHub Copilotのhuman-readable reportを表示する

- **WHEN** userが `catsift stats --source copilot` を実行し、有効なCopilot eventが存在する
- **THEN** システムはGitHub Copilotのsource表示、`copilot`に対応するAgent表示、Period、およびSessions・Turns・User Prompts・Tool Callsなどの集計を出力する

#### Scenario: GitHub CopilotのJSONを表示する

- **WHEN** userが `catsift tools --source copilot --json` または別の統計commandへ`--json`を指定する
- **THEN** stdoutは単独の有効なJSON documentとなり、`source`、`agents`、既存の集計fieldを含み、prompt本文・tool argument・warningを含まない

#### Scenario: GitHub Copilotのtoken usageをJSONへ表示する

- **WHEN** Copilotのpersisted usageにcache read、cache write、またはreasoning tokenが存在し、userがtoken統計を含むreportをJSONで出力する
- **THEN** JSONは既存のtoken usage fieldへ正規化された値を含み、raw provider payloadを含まない

#### Scenario: GitHub Copilotのproject nameを表示する

- **WHEN** userがproject metadataを含むCopilot sessionで`catsift sessions --source copilot`またはSession detailを実行する
- **THEN** JSONは`project_name`と取得可能な`project_path`をsanitized metadataとして含む

#### Scenario: GitHub Copilotの履歴が空である

- **WHEN** userが `catsift stats --source copilot` を実行し、有効な利用eventが存在しない
- **THEN** システムはsource、Agent、Period、および0件のJSON summaryを出力して0で終了する

### Requirement: activityでOverviewのdaily activityを取得できる

`catsift activity`は、TUI Overviewの対象scopeと同じnormalized historyからdaily activityを生成し、日付、Sessions、およびTurnsを既定のJSON rows arrayへ出力しなければならない（SHALL）。`catsift stats`はJSON summaryのみを出力しなければならない（SHALL）。

#### Scenario: daily activityを表示する

- **WHEN** userが `catsift activity --days 30` を実行する
- **THEN** システムは選択された30日間の実際の対象範囲について、日付ごとのSessionsとTurnsをdaily activityとして表示する

#### Scenario: daily activityをJSONで取得する

- **WHEN** userが `catsift activity --json` を実行する
- **THEN** stdoutのJSONはstats overviewを含めず、同じscopeの日付・Sessions・Turnsを持つ`rows` arrayを含む

### Requirement: CLIでModel一覧とModel detailを取得できる

`catsift models`は、TUI Models viewと同じscopeからprovider/nameごとのModel summaryを表示しなければならない（SHALL）。summaryは少なくともSessions、Turns、Token usage、およびLast Usedを含まなければならない（SHALL）。`catsift models detail provider/name`を指定した場合は、対象ModelのTurns、Prompts、Tools、Skills、Token usage、First Used、Last Used、および関連Sessionごとのsummaryを表示しなければならない（SHALL）。

#### Scenario: Model一覧を表示する

- **WHEN** userが `catsift models --source codex` を実行する
- **THEN** システムはCodex scopeのModelを決定的な順序で一覧表示し、各Modelの利用状況を表示する

#### Scenario: Model detailを表示する

- **WHEN** userが `catsift models detail openai/gpt-example --json` を実行する
- **THEN** システムは指定Modelのsummaryと、そのModelが観測されたSessionごとのTurns、Token usage、Project、および利用時刻をJSONで返す

#### Scenario: 存在しないModelを指定する

- **WHEN** userが現在のscopeに存在しないModelを `models detail`で指定する
- **THEN** システムはdetailを生成せず、対象Modelが存在しないことをstderrへ出力して非0で終了する

### Requirement: CLIでSkill detailを取得できる

既存の`catsift skills`は`skills detail NAME`を受け付け、TUI Skill detailと同じnormalized Skill evidenceから、Skill全体のUses、Sessions、Turns、usage mode、evidence state、検出method、および利用Sessionごとのsummaryを表示しなければならない（SHALL）。`skills detail`は既定で同じdetail情報をJSONへ出力しなければならない（SHALL）。detail subcommandはlist/unused専用optionと併用してはならない（MUST NOT）。

#### Scenario: Skill detailを表示する

- **WHEN** userが `catsift skills detail review --json` を実行する
- **THEN** システムは`review`のdeduplicated usage、distinct Turns、mode・state・methodの内訳、および利用SessionごとのUses、Turns、時刻をJSONで返す

#### Scenario: Skill detailの人間向け表示を行う

- **WHEN** userが `catsift skills detail review` を実行する
- **THEN** システムはSkill名、summary、evidenceの内訳、および関連Session一覧をJSONとして表示し、人向けの表示にはTUIを使用する

#### Scenario: unused viewとdetailを併用する

- **WHEN** userが `catsift skills detail review --unused` を実行する
- **THEN** システムはoption errorをstderrへ出力し、unused reportもSkill detailも生成しない

### Requirement: CLIでSession一覧とSession detailを取得できる

`catsift sessions`は、TUI Sessions viewと同じscopeからSession name、Session ID、Project、およびLast Usedを一覧表示しなければならない（SHALL）。`catsift sessions detail ID`を指定した場合は、scope内で一致するSessionのmetadata、集計値、および時系列のTurn summaryを表示しなければならない（SHALL）。Turn summaryはModel、Token usage、Tools、Skills、Status、Started、およびEndedを含まなければならない（SHALL）。Session IDがscope内で複数のSessionに一致する場合は曖昧な指定として扱い、候補をstderrへ示して非0で終了しなければならない（SHALL）。

#### Scenario: Session一覧を表示する

- **WHEN** userが `catsift sessions --source opencode` を実行する
- **THEN** システムはOpenCode scopeのSession name、Session ID、Project、およびLast Usedを決定的な順序で一覧表示する

#### Scenario: Session detailをJSONで表示する

- **WHEN** userが `catsift sessions detail session-001 --json` を実行する
- **THEN** システムは対象Sessionのsafe metadata、Prompts・Tools・Skills・Tokensの集計、および古いTurnから新しいTurnへ並んだTurn summaryをJSONで返す

#### Scenario: 存在しないSessionを指定する

- **WHEN** userが現在のscopeに存在しないSession IDを `sessions detail`で指定する
- **THEN** システムはdetailを生成せず、対象Sessionが存在しないことをstderrへ出力して非0で終了する

### Requirement: CLI detail reportはTUIと同じscopeとprivacy境界を使用する

追加するCLI commandとdetail subcommandは、既存のsource選択、期間、warning、終了code、およびJSONのstdout/stderr分離規則を継承しなければならない（SHALL）。同じsource、期間、および基準時刻を指定した場合、CLIのsummary/detailはTUIのQuery / Read Modelと同じ集計意味論、deduplication、決定的なsortを使用しなければならない（SHALL）。TUI detailとJSONは同じ論理的な情報を表さなければならない（SHALL）。prompt本文、command本文、Tool arguments、Skill本文、およびprovider payloadを出力してはならない（MUST NOT）。

#### Scenario: CLIとTUIで同じscopeを表示する

- **WHEN** userが同じsourceと期間でTUIを起動し、CLIで `models`、`skills detail`、または `sessions detail` を実行する
- **THEN** システムは共通のnormalized dataから同じsummary、detail、warning、およびrowの包含規則を使用する

#### Scenario: warningをJSONから分離する

- **WHEN** detail reportの入力中にrecoverableなrecord skipが発生する
- **THEN** stdoutは単独の有効なJSON documentのままで、warning summaryはstderrだけに出力される

#### Scenario: sensitive contentを含む履歴をdetail表示する

- **WHEN** SessionまたはSkillのdetail対象にprompt本文、Tool arguments、Skill本文、またはprovider payloadが存在する
- **THEN** CLIはそれらを出力せず、boundedなmetadata、集計値、canonical name、識別子、時刻、および状態だけを表示する

### Requirement: JSON互換flagの廃止を予告する

`--json`は互換boolean flagとして受け付けなければならない（SHALL）。明示指定時は値にかかわらずJSONを維持し、JSONが既定なのでflagは不要でv0.7.0に削除される旨をstderrへ警告しなければならない（SHALL）。未指定時は警告してはならない（MUST NOT）。helpはJSON既定と廃止予定を説明しなければならない（SHALL）。

#### Scenario: 明示flagを指定する
- **WHEN** userがreportへ`--json`、`--json=true`、`--json=false`または`-json`を渡す
- **THEN** stdoutはJSONのままで、stderrに廃止警告が一度だけ出力される

#### Scenario: helpと無効なflagを扱う
- **WHEN** userがhelp、無効なboolean値、またはTUIへの`--json=false`を指定する
- **THEN** helpは通常のhelpを表示し、無効値とTUIへのJSON flagはreportを出力せず引数errorとして拒否する
