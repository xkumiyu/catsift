## Purpose

Codexのsession・user prompt・Tool・Skill利用を、system logではなく集計条件と要点が伝わるstatic reportとして端末へ表示し、同じ内容をscriptや分析toolから再利用できる軽量CLIとして提供する。

## ADDED Requirements

### Requirement: overview統計を表示する
`catsift stats` は対象Agentと期間が分かるreport titleに続けて、Sessions、User Prompts、Tool Calls、およびSkill Usesを視覚的に区切られたsummaryとして表示しなければならない（SHALL）。Tool Callsはeffective Tool view、Skill Usesはturn単位で重複排除した全確認状態の利用を使用しなければならない（SHALL）。

#### Scenario: 履歴が存在する
- **WHEN** userが有効なCodex履歴に対して `catsift stats` を実行する
- **THEN** システムはCodexと対象期間を示すtitle、およびlabel付きの4つの集計値を既定のhuman-readable report形式でstdoutへ出力する

#### Scenario: 対象履歴が空である
- **WHEN** 読取対象に有効な利用Eventが存在しない
- **THEN** システムは各集計値を0として出力するとともに、選択期間に利用がないことを説明するempty-state messageを表示して0で終了する

### Requirement: Tool統計を表示する
`catsift tools` はreport titleと選択中の期間・layerに続けて、canonical Tool名ごとのCalls、Failures、およびLast Usedを見出し付きtableとして表示し、row数と総Callsをfooterへ表示しなければならない（SHALL）。既定ではeffective layerを集計し、`--layer effective|runtime|model` により集計layerを選択できなければならない（SHALL）。

#### Scenario: 既定のTool統計を表示する
- **WHEN** userがlayer指定なしで `catsift tools` を実行する
- **THEN** システムはeffective Tool利用をCalls降順、同数の場合はTool名昇順で、選択layerが分かるhuman-readable tableとして出力する

#### Scenario: model layerを指定する
- **WHEN** userが `catsift tools --layer model` を実行する
- **THEN** システムはruntime actionではなくmodel layerのTool callだけを集計する

#### Scenario: 無効なlayerを指定する
- **WHEN** userが定義外の `--layer` 値を指定する
- **THEN** システムは引数errorをstderrへ出力し、非0の終了codeを返す

### Requirement: Skill統計を表示する
`catsift skills` はreport titleと選択中の期間・strict状態に続けて、Skill名ごとのExplicit、Implicit、Confirmed、Inferred、Unconfirmed、Total、およびLast Usedを集計しなければならない（SHALL）。human-readable reportは利用可能な幅に応じて内訳columnを調整できるが、Skill名とTotalを常に表示しなければならない（SHALL）。JSONはすべての集計fieldを保持しなければならない（SHALL）。`--strict` が指定された場合はstateが `confirmed` の利用だけをTotalと各mode集計の対象にしなければならない（SHALL）。

#### Scenario: 全Skill観測を表示する
- **WHEN** userが `catsift skills` を実行する
- **THEN** システムは全確認状態のSkill利用をTotal降順、同数の場合はSkill名昇順で、確認状態の内訳が判別できるhuman-readable tableとして出力する

#### Scenario: strict modeを使用する
- **WHEN** userが `catsift skills --strict` を実行する
- **THEN** システムは `confirmed` でないSkill利用を件数とLast Usedの算出から除外する

#### Scenario: 同一利用に複数の証拠がある
- **WHEN** 1回の重複排除済みSkill利用が複数の検出方式を持つ
- **THEN** システムはその利用をTotalへ1回だけ加算する

### Requirement: 期間とCodex homeを各統計commandで指定できる
`stats`、`tools`、`skills` は共通して `--days`、`--codex-home`、`--color auto|always|never` を受け付けなければならない（SHALL）。filterとsource解決はすべての出力形式で同じ結果集合へ適用しなければならない（SHALL）。

#### Scenario: 30日分のSkill統計を取得する
- **WHEN** userが `catsift skills --days 30` を実行する
- **THEN** システムはcutoff以後の観測だけからSkill統計を生成する

#### Scenario: 別のCodex homeを集計する
- **WHEN** userが `catsift stats --codex-home /tmp/codex-home` を実行する
- **THEN** システムは指定先だけをsourceとしてoverviewを生成する

### Requirement: human-readable report・JSONを提供する
各統計commandは既定で、title、適用中のfilter、明確なsectionまたはcolumn heading、整列した値、および必要なfooterを持つhuman-readable static reportを出力しなければならない（SHALL）。countは桁区切りして右揃えにし、tableのLast Usedはtimezoneを含む簡潔なlocal日時で表示しなければならない（SHALL）。`--json` でmachine-readable出力へ切り替えられなければならず（SHALL）、JSONはhuman-readable reportと同じfilter・集計結果を表してfield順をcommandごとに安定させ、timestampをRFC 3339で出力しなければならない（SHALL）。

#### Scenario: 既定reportを表示する
- **WHEN** userが出力形式を指定せず任意の統計commandを実行する
- **THEN** システムはraw eventやsystem log風のline列ではなく、対象・期間・指標名と値の関係を一読できるstatic reportとしてstdoutへ出力する

#### Scenario: JSONを出力する
- **WHEN** userが任意の統計commandへ `--json` を指定する
- **THEN** stdout全体は単独の有効なJSON documentとなり、warningや進捗messageを含まない

### Requirement: terminal capabilityに応じて安全に装飾する
既定の `--color auto` では、システムはstdoutがTTYであり `NO_COLOR` が設定されていない場合だけhuman-readable reportへANSI styleを適用しなければならない（SHALL）。`--color always` は明示的にstyleを有効化し、`--color never` は無効化しなければならず、この2つの強制modeは `NO_COLOR` より優先しなければならない（SHALL）。JSONにはoptionやterminal状態にかかわらずANSI escape sequenceを含めてはならない（MUST NOT）。色は補助表現に限り、label、text、数値、または配置なしに意味を伝えてはならない（MUST NOT）。

#### Scenario: TTYへ既定reportを出力する
- **WHEN** stdoutがcolor対応TTYで、`--color` が未指定か `auto` であり、`NO_COLOR` が設定されていない
- **THEN** システムはtitle、heading、補助情報を判別しやすいstyleで表示する

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
human-readable reportは取得可能なterminal幅へ収まるようにlayoutを選択しなければならない（SHALL）。狭い幅では補助的な内訳columnを省略またはcompact化できるが、report title、対象期間、row identity、および主要countを保持しなければならない（SHALL）。長いnameはcolumn境界内で省略記号付きに短縮し、異なるrowの値に見える折返しをしてはならない（MUST NOT）。

#### Scenario: 十分に広いterminalで表示する
- **WHEN** terminal幅が全columnを表示できる
- **THEN** システムはToolまたはSkillの定義済み内訳とLast Usedを含むwide layoutを表示する

#### Scenario: 狭いterminalで表示する
- **WHEN** terminal幅がwide layoutに不足する
- **THEN** システムは主要nameとcountを残したcompact layoutを表示し、table rowを誤解を招く形で折り返さない

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
システムは同じ履歴、同じfilter、および同じ基準時刻に対して、file探索順に依存しない同一の件数とrow順序を生成しなければならない（SHALL）。同じterminal capability、幅、およびcolor modeを与えたhuman-readable reportはbyte単位で決定的でなければならない（SHALL）。

#### Scenario: file列挙順が変わる
- **WHEN** 同一内容の履歴fileが異なる順序で列挙される
- **THEN** table・JSONのrow順序と集計値は変化しない

#### Scenario: terminal capabilityを固定する
- **WHEN** 同じ集計resultを同じ幅、color profile、TTY状態、基準時刻で複数回描画する
- **THEN** human-readable reportの出力byte列は一致する
