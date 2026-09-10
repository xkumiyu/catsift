## ADDED Requirements

### Requirement: Codex履歴cacheをfile単位で再利用する

Codex sourceは、履歴fileごとのsource revisionを用いてcacheの再利用可否を判定しなければならない（SHALL）。変更されていないfileのcacheは再利用し、新規または変更されたfileだけを現在の履歴として再処理しなければならない（SHALL）。fileの変更前に生成された観測を、変更後のfileの観測としてそのまま使用してはならない（MUST NOT）。

#### Scenario: 変更されていないsession fileを再利用する

- **WHEN** Codex homeのsession fileが前回のcache作成時から変更されていない状態で統計commandを再実行する
- **THEN** システムはそのfileの正規化済みcacheを再利用し、freshな全file読取と同じreportを生成する

#### Scenario: session fileが変更される

- **WHEN** Codex homeの既存session fileが追加、更新、またはtruncateされている
- **THEN** システムはそのfileのcacheを無効として再処理し、変更後の内容を反映したreportを生成する

#### Scenario: session fileが削除される

- **WHEN** 前回cacheに対応するCodex session fileが現在のsessionsまたはarchived_sessionsから削除されている
- **THEN** システムは削除されたfileのcache由来の観測を現在のreportへ含めない

### Requirement: Codex cacheへ期間filterを適用する

Codex sourceで有効なcacheを利用する場合、システムはcache済み観測のtimestampへ`--days N`のcutoffを適用しなければならない（SHALL）。cache利用の有無にかかわらず、cutoff境界、全期間指定、および無効な`--days`のvalidation結果は同一でなければならない（SHALL）。

#### Scenario: Codexの直近1日をcacheから集計する

- **WHEN** 全期間のCodex cacheが存在し、userが`catsift stats --days 1`を実行する
- **THEN** システムはcache済みtimestampを用いて直近24時間だけを集計し、全履歴をfreshに再解析した場合と同じ結果を出力する

#### Scenario: Codex cacheがない状態で期間指定する

- **WHEN** userがcacheのないCodex sourceへ`--days 1`を指定する
- **THEN** システムは既存のCodex履歴filter規則で入力を構築し、生成した結果と正規化済みcacheへ同じcutoffを適用できる
