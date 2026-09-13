## ADDED Requirements

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
