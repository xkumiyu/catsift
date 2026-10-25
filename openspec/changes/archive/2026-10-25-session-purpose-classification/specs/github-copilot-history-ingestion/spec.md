## MODIFIED Requirements

### Requirement: Copilot履歴をread-onlyかつlocal-onlyで扱う

システムはGitHub Copilotのsource file、directory、session管理fileを統計生成のために変更してはならず（MUST NOT）、履歴内容をnetworkへ送信してはならない（MUST NOT）。source errorまたはskip可能なwarningが発生しても、既に読み取ったraw prompt、tool argument、provider payloadを外部へ出力してはならない（MUST NOT）。ただし、session purpose分類が明示的に有効で`TYPESAFE_API_KEY`が設定されている場合、分類用に抽出したsession titleと実際のuser-authored messageだけをTypeSafe System One互換endpointへ送信してよい。

#### Scenario: 履歴fileを変更しない

- **WHEN** userがGitHub Copilot sourceの統計commandを実行する
- **THEN** 入力fileの内容、size、permission、およびmodification timeは実行前後で変化しない

#### Scenario: raw内容を外部へ送信しない

- **WHEN** Copilot履歴からreportまたはcacheを生成する
- **THEN** 分類が無効またはkey未設定ならnetwork requestを行わず、分類を有効にした場合もrequestにはsession titleと実際のuser-authored messageだけを含め、tool argumentを外部へ渡さない
