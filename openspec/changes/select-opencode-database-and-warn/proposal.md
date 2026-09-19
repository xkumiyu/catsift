# Proposal

## Why

OpenCodeのchannel切り替え後に複数のdatabaseが残ると、古い履歴を読む、またはどのdatabaseを選んだか利用者に分からない状態になります。現在の実装で導入した決定的な選択とwarningを仕様として固定し、cache利用時を含めて同じ結果を保証します。

## What Changes

- `opencode.db`をprimary databaseとして優先し、存在しない場合はchannel databaseを更新時刻で選択します。
- database本体に加えて`-wal`と`-shm`の更新時刻を選択判定へ含め、同時刻はpathのlexicographic順で決定します。
- 複数databaseが見つかった場合、未選択database数を`opencode_multiple_databases` warningとして通知します。
- warningはnormalized cacheへ永続化せず、cache hit時にも再通知します。
- CLIとTUIで複数database warningをrecord skipと混同しない表現で表示します。

## Capabilities

### New Capabilities

### Modified Capabilities

- `opencode-history-ingestion`: 複数databaseの発見、選択順序、warning、およびcache hit時の通知を明文化します。

## Impact

- `internal/opencode`のdatabase discovery、revision、cache warning処理に影響します。
- `internal/usage`、`cmd/catsift`、`internal/tui`のwarning表示に影響します。
- 新しい外部dependency、database migration、またはOpenCode data rootへの書き込みはありません。
