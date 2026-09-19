# Tasks

## 1. Database discovery

- [x] 1.1 `opencode.db`のprimary優先とchannel databaseの更新時刻・lexicographic tie-breakを実装し、`internal/opencode`の選択テストで確認する
- [x] 1.2 database本体より新しい`-wal`の更新時刻が候補選択に勝つ回帰テストを追加し、`go test ./internal/opencode`で確認する
- [x] 1.3 cache hitでも選択結果とwarningを維持し、diagnosticでsourceを再読していないことを確認する

## 2. Warning rendering

- [x] 2.1 `opencode_multiple_databases`をnormalized cacheへ保存せずcold readとcache hitで付加する処理を確認する
- [x] 2.2 CLI summaryでignored database数をrecord skip数と分離し、CLI testで確認する
- [x] 2.3 TUIでOpenCode database warningの単数・複数文言を表示し、TUI testで確認する
- [x] 2.4 filter適用順をcold readとcache hitで統一し、既存のfilter/cache testが通ることを確認する

## 3. Specification and verification

- [x] 3.1 OpenSpec proposal、design、spec deltaを追加し、複数database選択・warning・cache hitの契約を記録する
- [x] 3.2 `openspec validate select-opencode-database-and-warn --strict`、`mise run check`、および`git diff --check`を実行して全体検証を完了する
