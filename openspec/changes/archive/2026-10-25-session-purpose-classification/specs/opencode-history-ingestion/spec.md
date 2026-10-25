## MODIFIED Requirements

### Requirement: OpenCode履歴とcacheをlocalかつread-onlyに扱う

システムはOpenCode履歴の統計生成中にOpenCode data root配下の履歴、metadata、およびschemaを変更してはならず（MUST NOT）、履歴内容を外部networkへ送信してはならない（MUST NOT）。正規化済みcacheを生成する場合も、cacheはOpenCode data rootとは分離されたuser cache directoryへ保存し、raw prompt本文、Tool arguments、およびraw provider payloadを保存してはならない（MUST NOT）。ただし、session purpose分類が明示的に有効で`TYPESAFE_API_KEY`が設定されている場合、分類用に抽出したsession titleと実際のuser-authored messageだけをTypeSafe System One互換endpointへ送信してよい。

#### Scenario: OpenCodeの統計を生成する

- **WHEN** userが任意の統計commandでOpenCode sourceを選択する
- **THEN** OpenCode data root内のfile、database、metadataは実行前後で変化せず、分類が無効またはkey未設定なら外部network通信を行わず、分類を有効にした場合もsession titleと実際のuser-authored messageだけを送信する

#### Scenario: OpenCodeのcacheを生成する

- **WHEN** 履歴の完全な正規化が成功してcacheを保存する
- **THEN** システムはlocal user cacheへ最小限の正規化済み情報だけを保存し、OpenCode履歴のraw本文とTool argumentsをcacheへ含めない
