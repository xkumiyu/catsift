## Why

catsiftの既存commandは、期間全体の利用状況、Tool、Skillの集計をstatic reportとして表示する。一方、Modelごとの利用実績、個別Skillの検出根拠、SessionのTurn推移を横断して調べるには、集計結果だけでは情報が足りず、履歴を再読する複数の操作が必要になる。

ローカル履歴をその場で探索できるUsage Explorerを追加し、現在のfrontendとしてTUIを提供する。既存CLIと同じ正規化データ・filter・privacy方針を共有する。現時点ではWeb dashboard、HTTP server、browser frontendは対象にしない。

## What Changes

- TUIと既存CLIが共有できるsource-neutralなHistory Query / Read Modelを導入する。
- Modelのprovider・canonical name・利用時刻を正規化済みデータへ保持し、Model別の利用状況を集計できるようにする。
- Session metadataとTurnの関係を保持し、Session一覧とSession詳細を生成できるようにする。
- Sessions一覧はSession名称、Project、最終利用時刻に絞り、Turn数とToken usageはSession detailで表示する。
- CodexのSession一覧名称を`session_index.jsonl`の`thread_name`から取得する。
- Skillのdeduplicated usage、mode、evidence state、検出method、および利用Session/Turnを個別Skillの詳細へ展開できるようにする。
- `catsift`をinteractive terminalにおけるcanonicalなUsage Explorer入口とし、Overview、Models、Skills、Sessionsの一覧とdetail viewを提供する。
- CLIとTUIは同じnormalized data、filter、warning、および集計意味論を共有し、CLIはautomation向けのstatic report、TUIは人間向けの対話的なdetail探索として役割を分ける。
- source、Agent、期間、Project、Model、Skillを共通filterとして扱い、CLIの期間・source・warning・cache規則をTUIでも再利用する。
- TUIは履歴を読み取り専用で扱い、prompt本文、command本文、Tool引数、Skill本文を通常の画面やcacheへ保存・表示しない。
- 既存の`stats`、`tools`、`skills`のhuman-readable/JSON outputは変更しない。

## Capabilities

### New Capabilities

- `tui-usage-explorer`: ローカルAgent履歴をOverview、Model、Skill、Sessionの一覧・詳細としてTUIで探索する契約。

### Modified Capabilities

- `usage-event-normalization`: Model identity、source-qualified Session identity、および詳細表示に必要なTurn-level metadataを正規化済みEventへ保持する要件を追加する。
- `usage-statistics-cli`: `catsift`を既定のUsage Explorer入口とし、既存source/filter規則を継承する対話的なreport入口の要件を追加する。

## Impact

- `internal/usage`にModelと共通Session metadataを追加し、token usage eventとTurnのModel attributionを保持する。
- `internal/codex`、`internal/ctx`、`internal/opencode`の各adapterと`internal/cache`へ新しいmetadataを接続する。
- `internal/aggregate`の既存集計を再利用しつつ、Model・Skill・Session detailを生成するQuery / Read Model packageを追加する。
- `internal/tui`と`cmd/catsift`へTUIの状態管理、navigation、terminal lifecycleを追加する。
- 既存のstatic report rendererとJSON schemaは互換性を維持する。
- 新しい外部サービス、Web server、browser runtime、履歴データのmigrationは導入しない。
