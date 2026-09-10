## Why

`catsift`は履歴全体を毎回再解析しているため、Codexでは`--days 1`でも全期間とほぼ同じ時間がかかり、ctxでは大量のイベント取得・JSON解析・正規化に時間とmemoryを消費している。日次の利用や複数のreport commandを実用的な待ち時間で繰り返せるよう、入力処理と再実行経路を改善する。

## What Changes

- Codexとctxの入力を、必要な正規化済み情報だけを保持しながらstreamingで処理し、raw履歴と重複したJSON解析・走査を保持しないようにする。
- ctxイベント列挙のpage sizeを調整し、同一のimmutable generationを確認しながらpage取得回数とプロセス起動 overheadを削減する。
- Codexは履歴file単位、ctxはimmutable generation単位で、parser versionとsource scopeを含むローカル永続キャッシュを利用する。
- cache hit時はキャッシュ済みの正規化データへ期間filterを適用し、`--days 1`のために全履歴を再取得・再解析しないようにする。
- cacheはread-onlyな入力sourceや外部networkの状態を変更せず、壊れた・古い・未完了のcacheは安全に無視して再構築する。
- reportの件数、並び順、source scope、warning、JSON形式など既存の外部動作は維持する。

## Capabilities

### New Capabilities

- `history-ingestion-cache`: Codexとctxの正規化済み履歴を、source固有の変更検出情報とparser versionに基づいて安全に再利用する仕組みを定義する。

### Modified Capabilities

- `codex-history-ingestion`: streaming処理、期間filter、read-only/local制約を維持したまま、file単位cacheを利用できるようにする。
- `ctx-history-ingestion`: immutable generationの完全性・決定性・read-only制約を維持したまま、page取得の効率化とgeneration単位cacheを利用できるようにする。

## Impact

- 影響コードは`internal/codex`、`internal/ctx`、共通の正規化・集計処理、およびCLIの入力構築部分。
- ユーザーのcache directoryへ、集計に必要な最小限の正規化済み履歴情報を保存する。raw prompt本文やtool引数など不要な履歴内容は保存対象にしない。
- 新しい外部serviceやnetwork通信は追加しない。SQLiteなどの外部DB依存は追加せず、標準libraryで扱えるcache fileを使用する。
- cacheのschema変更時はparser versionで無効化し、書き込みはatomicに行う。
