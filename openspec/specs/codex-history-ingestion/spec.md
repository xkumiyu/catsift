# codex-history-ingestion Specification

## Purpose

Codex がローカルに保存する session JSONL を安全かつ再現可能に探索・読取し、期間指定やschema差異があっても利用統計の入力として扱えるようにする。

## Requirements

### Requirement: Codex homeを一意に解決する
システムは `CODEX_HOME`、OS user home配下の `.codex` の優先順で Codex homeを解決しなければならない（SHALL）。環境変数が空の場合は既定値を使用しなければならない（SHALL）。

#### Scenario: CODEX_HOMEを使用する
- **WHEN** `CODEX_HOME` が空でない
- **THEN** システムは `CODEX_HOME` の値を読取対象にする

#### Scenario: 既定のCodex homeを使用する
- **WHEN** `CODEX_HOME` が空である
- **THEN** システムは現在のOS user home配下の `.codex` を読取対象にする

### Requirement: session履歴を発見する
システムは解決した Codex homeの `sessions` および `archived_sessions` 配下から `.jsonl` fileを再帰的に発見しなければならない（SHALL）。片方が存在しない場合は、存在する方の探索を継続しなければならない（SHALL）。

#### Scenario: 通常sessionとarchiveが存在する
- **WHEN** `sessions` と `archived_sessions` の両方にJSONLが存在する
- **THEN** システムは両方のJSONLを入力に含め、同一fileを重複して読まない

#### Scenario: 履歴fileが存在しない
- **WHEN** Codex homeは読取可能だが対象JSONLが存在しない
- **THEN** システムは空の入力として正常終了できる

#### Scenario: Codex homeを読めない
- **WHEN** 解決した Codex homeが存在しない、または読取不能である
- **THEN** システムは対象pathを含むerrorをstderrへ出力し、非0の終了codeを返す

### Requirement: JSONLをstreamingで処理する
システムは各JSONLをline単位で処理し、有効なlineをfile全体のmemory展開なしに後続処理へ渡さなければならない（SHALL）。各観測には少なくともsource file、line番号、session識別子、timestamp、および取得できる場合はCodex CLI versionを関連付けなければならない（SHALL）。

#### Scenario: 大きなsession fileを処理する
- **WHEN** 多数のlineを含む有効なJSONLを読み取る
- **THEN** システムはfile全体のsizeに比例する完全なJSON treeをmemory上に構築せず、全lineを処理する

#### Scenario: session metadataを取得する
- **WHEN** JSONLに `session_meta` が含まれる
- **THEN** システムは後続recordをそのsession ID、project path、Codex CLI versionと関連付ける

### Requirement: 未知・破損recordから回復する
システムは未知のrecord type、未知のfield、単独の不正JSON line、または個別fileの読取errorによって、他の有効な履歴の集計を中断してはならない（MUST NOT）。skip件数と理由の要約はstderrへwarningとして出力し、stdoutのtable・JSONを汚染してはならない（MUST NOT）。

#### Scenario: 未知のrecord typeがある
- **WHEN** 対応していない `type` を持つ有効なJSON lineがある
- **THEN** システムはそのlineをskipし、対応済みrecordの処理を継続する

#### Scenario: 不正JSON lineが混在する
- **WHEN** 1つのfileに不正JSONと有効なJSON lineが混在する
- **THEN** システムは不正lineをwarning対象にし、有効なlineから結果を生成して0で終了する

#### Scenario: 一部fileだけ読取不能である
- **WHEN** 複数の対象fileのうち一部だけが読取不能である
- **THEN** システムは読取可能なfileを集計し、読取不能fileをstderrで報告する

### Requirement: 期間filterをrecord timestampへ適用する
`--days N` が指定された場合、システムは実行時刻から `N * 24時間` 前をcutoffとし、cutoff以後のtimestampを持つ観測だけを対象にしなければならない（SHALL）。`N` は1以上の整数でなければならない（MUST）。指定がない場合は利用可能な全期間を対象にしなければならない（SHALL）。

#### Scenario: cutoff上のrecordを含める
- **WHEN** recordのtimestampが算出されたcutoffと等しい
- **THEN** システムはそのrecordを集計対象に含める

#### Scenario: 無効なdaysを拒否する
- **WHEN** userが `--days 0`、負数、または整数でない値を指定する
- **THEN** システムは引数errorをstderrへ出力し、非0の終了codeを返す

### Requirement: Codex履歴をread-onlyかつlocalに扱う
システムは統計生成のためにCodex home配下のfileを変更してはならず（MUST NOT）、履歴内容をnetworkへ送信してはならない（MUST NOT）。

#### Scenario: 集計を実行する
- **WHEN** userが任意の統計commandを実行する
- **THEN** Codex home配下のfile内容とmetadataは実行前後で変化せず、外部network通信は発生しない

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
