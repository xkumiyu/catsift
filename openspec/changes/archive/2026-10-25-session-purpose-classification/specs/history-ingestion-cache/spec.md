## MODIFIED Requirements

### Requirement: cacheへ保存する履歴内容を最小化する

システムはreportの再生成に必要な正規化済みfieldだけをcacheへ保存し、raw prompt本文、tool引数、および不要なraw provider payloadを保存してはならない（MUST NOT）。cacheはユーザーのlocal machine内だけで扱い、履歴内容をnetworkへ送信してはならない（MUST NOT）。この規則はCodex、ctx、OpenCode、およびGitHub Copilotのcacheへ同じように適用しなければならない（SHALL）。ただし、session purpose分類が明示的に有効で`TYPESAFE_API_KEY`が設定されている場合に限り、分類処理はcacheとは独立し、session titleと実際のuser-authored messageだけをTypeSafe System One互換endpointへ送信してよい。分類用のraw内容をcacheへ保存してはならない。

#### Scenario: cache内容を生成する

- **WHEN** システムがCodex、ctx、OpenCode、またはGitHub Copilot履歴sourceからcacheを作成する
- **THEN** cacheにはsession・turn・Tool・Skill・Model・token usageとfilterに必要なsanitized情報だけが含まれ、raw prompt本文、tool引数、およびprovider payloadは含まれない

#### Scenario: project metadataをcacheから復元する

- **WHEN** Copilot sessionにproject nameとproject pathが存在し、cache hitでreportを生成する
- **THEN** fresh readと同じproject nameとproject pathをSession detailおよびJSONへ復元する

#### Scenario: Copilotのtoken usageをcacheから復元する

- **WHEN** Copilot sessionのshutdown aggregateにcache read、cache write、またはreasoning tokenが存在し、cache hitでreportを生成する
- **THEN** fresh readと同じtoken usageの値を復元し、raw `modelMetrics` payloadはcacheへ保存しない

#### Scenario: cacheを利用してreportを生成する

- **WHEN** userが有効なCodex、ctx、OpenCode、またはGitHub Copilot cacheを利用する統計commandを実行する
- **THEN** 分類が無効またはkey未設定ならcache・履歴を外部networkへ送信せず、分類を有効にした場合もsession titleと実際のuser-authored messageだけをTypeSafe System One互換endpointへ送り、cacheにはraw内容を保存せず、各sourceのdata rootとprovider-owned履歴を変更しない
