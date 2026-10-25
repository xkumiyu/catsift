## MODIFIED Requirements

### Requirement: Codex履歴をread-onlyかつlocalに扱う

システムは統計生成のためにCodex home配下のfileを変更してはならず（MUST NOT）、履歴内容をnetworkへ送信してはならない（MUST NOT）。ただし、session purpose分類が明示的に有効で`TYPESAFE_API_KEY`が設定されている場合、分類用に抽出したsession titleと実際のuser-authored messageだけをTypeSafe System One互換endpointへ送信してよい。

#### Scenario: 集計を実行する

- **WHEN** userが統計commandを実行する
- **THEN** Codex home配下のfile内容とmetadataは実行前後で変化せず、分類が無効またはkey未設定なら外部network通信を行わず、分類を有効にした場合もsession titleと実際のuser-authored messageだけを送信する
