## ADDED Requirements

### Requirement: cache由来のsessionへsource pathをメモリ上だけで付与する

ingestion cacheから復元したsessionには、対応するsource file pathをメモリ上でのみ付与しなければならない（SHALL）。このpathはcache snapshotへ保存してはならず（MUST NOT）、report、store、diagnosticへ永続化・表示してはならない（MUST NOT）。pathを必要としないsourceでは付与しなくてよい。

#### Scenario: cacheから復元したsessionで本文parse対象を決める

- **WHEN** cacheからsession一覧を復元し、分類の再検証対象を決める
- **THEN** systemはメモリ上のsource pathを使って対象fileを特定できる

#### Scenario: pathを永続化しない

- **WHEN** source pathを付与したsessionを含むsnapshotをcacheへ書き込む、またはreportを出力する
- **THEN** source pathはcacheとreportに含まれない
