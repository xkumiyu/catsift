## ADDED Requirements

### Requirement: activityでOverviewのdaily activityを取得できる

`catsift activity`は、TUI Overviewの対象scopeと同じnormalized historyからdaily activityを生成し、日付、Sessions、およびTurnsだけをhuman-readable reportへ表示しなければならない（SHALL）。`--json`を併用した場合は、同じscopeと集計結果を`rows` arrayとして出力しなければならない（SHALL）。既存の`catsift stats`はsummaryのみを表示し、出力を変更してはならない（MUST NOT）。

#### Scenario: daily activityを表示する

- **WHEN** userが `catsift activity --days 30` を実行する
- **THEN** システムは選択された30日間の実際の対象範囲について、日付ごとのSessionsとTurnsをdaily activityとして表示する

#### Scenario: daily activityをJSONで取得する

- **WHEN** userが `catsift activity --json` を実行する
- **THEN** stdoutのJSONはstats overviewを含めず、human-readable reportと同じ日付・Sessions・Turnsを持つ`rows` arrayを含む

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

既存の`catsift skills`は`skills detail NAME`を受け付け、TUI Skill detailと同じnormalized Skill evidenceから、Skill全体のUses、Sessions、Turns、usage mode、evidence state、検出method、および利用Sessionごとのsummaryを表示しなければならない（SHALL）。`skills detail`のhuman-readable reportとJSONは同じdetail情報を表さなければならない（SHALL）。detail subcommandはlist/unused専用optionと併用してはならない（MUST NOT）。

#### Scenario: Skill detailを表示する

- **WHEN** userが `catsift skills detail review --json` を実行する
- **THEN** システムは`review`のdeduplicated usage、distinct Turns、mode・state・methodの内訳、および利用SessionごとのUses、Turns、時刻をJSONで返す

#### Scenario: Skill detailの人間向け表示を行う

- **WHEN** userが `catsift skills detail review` を実行する
- **THEN** システムはSkill名、summary、evidenceの内訳、および関連Session一覧をstatic reportとして表示する

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

追加するCLI commandとdetail subcommandは、既存のsource選択、期間、warning、終了code、および`--json`のstdout/stderr分離規則を継承しなければならない（SHALL）。同じsource、期間、および基準時刻を指定した場合、CLIのsummary/detailはTUIのQuery / Read Modelと同じ集計意味論、deduplication、決定的なsortを使用しなければならない（SHALL）。human-readable reportとJSONは同じ論理的な情報を表さなければならない（SHALL）。prompt本文、command本文、Tool arguments、Skill本文、およびprovider payloadを出力してはならない（MUST NOT）。

#### Scenario: CLIとTUIで同じscopeを表示する

- **WHEN** userが同じsourceと期間でTUIを起動し、CLIで `models`、`skills detail`、または `sessions detail` を実行する
- **THEN** システムは共通のnormalized dataから同じsummary、detail、warning、およびrowの包含規則を使用する

#### Scenario: warningをJSONから分離する

- **WHEN** detail reportの入力中にrecoverableなrecord skipが発生する
- **THEN** stdoutは単独の有効なJSON documentのままで、warning summaryはstderrだけに出力される

#### Scenario: sensitive contentを含む履歴をdetail表示する

- **WHEN** SessionまたはSkillのdetail対象にprompt本文、Tool arguments、Skill本文、またはprovider payloadが存在する
- **THEN** CLIはそれらを出力せず、boundedなmetadata、集計値、canonical name、識別子、時刻、および状態だけを表示する
