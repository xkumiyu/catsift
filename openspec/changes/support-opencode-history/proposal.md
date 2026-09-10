## Why

agentstatsは現在、Codexのローカル履歴とctxのevent streamを集計できますが、OpenCodeを直接利用している場合はOpenCodeの履歴を集計できません。OpenCodeのローカル履歴を読み取り専用で取り込めるようにし、ctxを導入していない環境でも複数のAI coding agentの利用状況を同じreportで確認できるようにします。

## What Changes

- OpenCodeのローカルに保存されたsession履歴を、新しいhistory sourceとして読み取る。
- `stats`、`tools`、`skills`でOpenCode sourceを選択できるようにし、OpenCode固有の履歴rootを指定できるようにする。
- OpenCodeの履歴を共通のsession、turn、user prompt、Tool、Skill、および取得可能なtoken usageへ正規化する。
- 既存の期間filter、決定的なreport、JSON、warningとerrorの分離、およびread-onlyの扱いをOpenCode sourceにも適用する。
- OpenCode履歴の正規化済みcacheを既存のcache規則に従って再利用できるようにする。
- Codex sourceとctx sourceの既存動作は変更しない。

## Capabilities

### New Capabilities

- `opencode-history-ingestion`: OpenCodeのローカル履歴を探索・読み取り、共通利用モデルへ正規化する入力契約。

### Modified Capabilities

- `usage-event-normalization`: OpenCodeのraw履歴からsession、prompt、Tool、Skill、およびtoken usageを共通Eventへ変換する要件を追加する。
- `usage-statistics-cli`: `--source opencode`とOpenCode source固有の履歴root指定を各統計commandで利用できる要件を追加する。
- `history-ingestion-cache`: OpenCode sourceのscope、revision、およびparser versionに基づくcache再利用要件を追加する。

## Impact

- `internal/opencode`の履歴adapterとテストfixtureを追加する。
- `internal/usage`のsource識別、共通正規化、および`cmd/agentstats`のsource選択・option validationを変更する。
- `internal/cache`とreport metadataにOpenCode sourceを追加する。
- `README.md`と`README.ja.md`へOpenCode sourceの利用方法と対応範囲を追記する。
- OpenCodeの保存形式を読み取るための依存関係が必要な場合は、designで最小の実装方法と保守対象versionを決定する。履歴データを外部へ送信したり、OpenCode側のデータを変更したりはしない。
