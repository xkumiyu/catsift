# Spec Delta

## MODIFIED Requirements

### Requirement: OpenCodeの永続履歴をread-onlyで発見する

システムは解決したOpenCode data rootにあるOpenCodeの永続session履歴を入力として発見しなければならない（SHALL）。既定の`opencode.db`が存在する場合はprimary databaseとして選択しなければならない（SHALL）。`opencode.db`がない場合は、channel suffixを持つ`opencode-<channel>.db`を候補として扱い、各候補のdatabase本体、`-wal`、および`-shm`の最終更新時刻のうち最も新しい時刻で候補を比較しなければならない（SHALL）。比較時刻が同じ候補はpathのlexicographic順で決定しなければならない（SHALL）。履歴の存在しないdata rootは空入力として扱えるが、解決したdata rootが存在しない、directoryでない、または読み取り不能な場合はsource errorとして扱わなければならない（MUST）。OpenCodeの診断用`log`だけを利用統計の入力として解釈してはならない（MUST NOT）。

#### Scenario: OpenCodeの履歴が存在する

- **WHEN** data rootにOpenCodeのsession、message、またはTool履歴が保存されている
- **THEN** システムは選択したdatabaseからそれらを読み取り、session履歴を共通の正規化処理へ渡す

#### Scenario: primary databaseが存在する

- **WHEN** data rootに`opencode.db`と1つ以上のchannel databaseが存在する
- **THEN** システムはchannel databaseの更新時刻が新しくても`opencode.db`を読み取り対象にする

#### Scenario: primary databaseがなく複数のchannel databaseが存在する

- **WHEN** data rootに複数の`opencode-<channel>.db`があり、`opencode.db`が存在しない
- **THEN** システムはdatabase本体と`-wal`、`-shm`の最終更新時刻が最も新しいchannel databaseを読み取り対象にする

#### Scenario: channel databaseの更新時刻が同じである

- **WHEN** primary databaseがなく、複数のchannel databaseの比較時刻が同じである
- **THEN** システムはpathのlexicographic順で先にあるdatabaseを読み取り対象にする

#### Scenario: 履歴がないdata rootを読む

- **WHEN** 解決したdata rootは読み取り可能だが利用統計へ変換可能な履歴が存在しない
- **THEN** システムは空のsession、turn、Tool、Skill入力として正常終了する

#### Scenario: 解決したOpenCode data rootを読めない

- **WHEN** 解決したOpenCode data rootが存在しない、directoryでない、または読み取り不能である
- **THEN** システムは対象pathを含むerrorをstderrへ出力し、非0の終了codeを返す

#### Scenario: 診断logだけが存在する

- **WHEN** data rootに診断用logは存在するが、利用統計用の永続履歴が存在しない
- **THEN** システムは診断logをsessionやToolとして集計せず、空入力として扱う

## ADDED Requirements

### Requirement: 複数のOpenCode databaseをwarningする

システムは複数のOpenCode database候補を発見した場合、選択したdatabaseだけを読み取ったことと未選択database数を`opencode_multiple_databases` warningとしてnormalized resultへ付加しなければならない（SHALL）。このwarningの`Count`は未選択database数、`Path`はdata root、`Source`はOpenCodeを示さなければならない（SHALL）。このwarningは履歴recordのskip件数として扱ってはならない（MUST NOT）。

#### Scenario: 複数databaseを発見する

- **WHEN** data rootに2つ以上のOpenCode database候補がある
- **THEN** システムは選択したdatabase以外の件数を含む`opencode_multiple_databases` warningを返す

#### Scenario: databaseが1つだけである

- **WHEN** data rootに読み取り可能なOpenCode database候補が1つだけある
- **THEN** システムは複数database warningを返さない

#### Scenario: cache hitで複数database warningを再表示する

- **WHEN** 複数databaseを検出した結果のnormalized cacheが存在し、同じdata rootをcache hitとして読み取る
- **THEN** システムはwarningをcache snapshotへ永続化せず、現在のdatabase discoveryに基づくwarningを再度返す
