## 1. 正規化データ契約

- [x] 1.1 `usage`へModel identityとModel observationを追加し、TurnおよびToken usage eventへ帰属させる。Model切替とunknown Modelを合成データで検証するfocused testを追加する
- [x] 1.2 source共通のSession metadataとsource-qualified identityを追加し、Codex、ctx、OpenCodeのSession metadata mappingを実装する。source間の同一ID衝突と時刻導出をadapter/query testで検証する

## 2. Cache互換性

- [x] 2.1 Model observation、Token usage eventのModel帰属、Session metadataをsanitized cache snapshotへ保存・復元する。round tripとprompt/Tool arguments非保存をcache testで検証する
- [x] 2.2 新しいsnapshot/parser versionを適用し、旧versionのcacheを安全なmissとして再生成する。既存cache validation testとfocused cache testが通ることを確認する

## 3. Query / Read Model

- [x] 3.1 正規化snapshotからsource、Agent、期間、Project、Model、Skillを適用し、決定的なsortとSession/Turn包含規則を提供する。列挙順を変えた合成snapshotのquery testが同じ結果になることを検証する
- [x] 3.2 OverviewとModel summary/detailを生成し、Session数、Turn数、prompt数、Token usage、first/last used、unknown Modelを表示用型へ集計する。Model切替を含むquery testを追加する
- [x] 3.3 Skill summary/detailとSession summary/detailを生成し、deduplicated evidence、mode/state/method、Session metadata、時系列Turn summaryを返す。raw prompt、command、Tool arguments、Skill本文がread modelに含まれないことをtestで検証する

## 4. CLI共通入力経路

- [x] 4.1 既存のsource選択、期間filter、cache、warningをQuery入力へ接続し、`stats`、`tools`、`skills`が従来のaggregate reportを維持する。既存CLI regression testと`go test ./cmd/catsift ./internal/aggregate ./internal/output`を通す

## 5. TUI

- [x] 5.1 既存のGo terminal依存と互換するTUI event loop、raw terminal lifecycle、viewport、入力境界を追加する。TTY判定、restore、resize、quit、非TTYエラーをstate/terminal testで検証する
- [x] 5.2 Overview、Models、Skills、Sessionsの一覧と各detail routeをread modelへ接続する。selection、back、filter、windowing、相対最終利用時刻、warning表示、明示的reloadをTUI state/view testで検証する
- [x] 5.3 `catsift`の既定Usage Explorer dispatch、help、既存source/期間/root option、`--json`拒否、source error処理を追加する。CLI integration testでTTY/non-TTYのexit code、stderr、既存command互換性、および旧`tui` commandの拒否を検証する

## 6. 公開面と最終検証

- [x] 6.1 `README.md`と`README.ja.md`へ`catsift`をUsage Explorerの既定入口とする使い方、scope、privacy、基本操作をcontent-equivalentに追加し、help textとcommand一覧を更新する。READMEの対応するoption・section・exampleを比較して検証する
- [x] 6.2 TUI、Query、cache、adapter、既存CLIのsynthetic testをまとめて実行し、`mise run check`、`go test -race ./...`、`go build -o ./bin/catsift ./cmd/catsift`を通す。主要画面をTTYで手動確認する

## 7. Codex Session名称

- [x] 7.1 `<CODEX_HOME>/session_index.jsonl`の最新`thread_name`をSession metadataへ結合し、cache使用時の名称更新、欠損・破損indexの継続処理、およびTUI表示をsynthetic testで検証する
- [x] 7.2 Sessions一覧をSession名称、Project、最終利用時刻のcompact表示にし、名称なしは括弧付きmuted短縮IDで示す。Turn数・Token usageをSession detailへ限定し、Session name空値・完全ID・Session ID検索・aborted状態表示をTUI testで検証する
