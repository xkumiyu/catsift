# opencode-history-ingestion Specification

## Purpose

OpenCodeがlocal machineへ保存したsession履歴を安全に読み取り、ctxを介さずにcatsiftの共通利用統計へ取り込めるようにする。

## Requirements

### Requirement: OpenCode data rootを解決する

システムは、`OPENCODE_HOME`、OpenCodeが使用する`xdg-basedir`既定data directoryの順でOpenCode data rootを解決しなければならない（SHALL）。環境変数が空の場合は次の候補を使用しなければならない（SHALL）。`XDG_DATA_HOME`が空の場合の既定値はuser home配下の`.local/share/opencode`とする。

#### Scenario: OPENCODE_HOMEを使用する

- **WHEN** `OPENCODE_HOME`が空でない
- **THEN** システムは`OPENCODE_HOME`の値を読み取り対象にする

#### Scenario: OpenCodeの既定data rootを使用する

- **WHEN** `OPENCODE_HOME`が空である
- **THEN** システムはOpenCodeの`xdg-basedir`に対応する既定data directoryを読み取り対象にする

### Requirement: OpenCodeの永続履歴をread-onlyで発見する

システムは解決したOpenCode data rootにあるOpenCodeの永続session履歴を入力として発見しなければならない（SHALL）。既定の`opencode.db`がない場合は、channel suffixを持つ`opencode-<channel>.db`も候補として扱わなければならない（SHALL）。履歴の存在しないdata rootは空入力として扱えるが、解決したdata rootが存在しない、directoryでない、または読み取り不能な場合はsource errorとして扱わなければならない（MUST）。OpenCodeの診断用`log`だけを利用統計の入力として解釈してはならない（MUST NOT）。

#### Scenario: OpenCodeの履歴が存在する

- **WHEN** data rootにOpenCodeのsession、message、またはTool履歴が保存されている
- **THEN** システムはそれらを読み取り、session履歴を共通の正規化処理へ渡す

#### Scenario: 履歴がないdata rootを読む

- **WHEN** 解決したdata rootは読み取り可能だが利用統計へ変換可能な履歴が存在しない
- **THEN** システムは空のsession、turn、Tool、Skill入力として正常終了する

#### Scenario: 解決したOpenCode data rootを読めない

- **WHEN** 解決したOpenCode data rootが存在しない、directoryでない、または読み取り不能である
- **THEN** システムは対象pathを含むerrorをstderrへ出力し、非0の終了codeを返す

#### Scenario: 診断logだけが存在する

- **WHEN** data rootに診断用logは存在するが、利用統計用の永続履歴が存在しない
- **THEN** システムは診断logをsessionやToolとして集計せず、空入力として扱う

### Requirement: OpenCode sessionとturnを正規化する

システムはOpenCodeの各sessionをhistory sourceとOpenCode Agent identityを含むsessionとして識別し、session内の実際のuser messageをuser promptとして数えなければならない（SHALL）。assistant message、system message、Tool output、contextとして注入されたSkill本文をuser promptへ加算してはならない（MUST NOT）。各正規化済み観測には、取得できる範囲でsource位置、session identity、turn identity、およびtimestampを保持しなければならない（SHALL）。

#### Scenario: session内にuser promptとassistant responseがある

- **WHEN** OpenCodeの1つのsessionにuser messageとそれに続くassistant responseが保存されている
- **THEN** システムはsessionを1件、対応するuser入力を1件のuser promptとして集計する

#### Scenario: 複数のuser promptが1つのsessionにある

- **WHEN** OpenCodeの同一sessionに時系列の異なるuser messageが複数存在する
- **THEN** システムは各user messageを別turnのuser promptとして集計し、session数は1件に保つ

#### Scenario: context messageがuser roleで保存される

- **WHEN** OpenCode履歴のuser role messageがSkill本文やその他のcontext注入だけを含む
- **THEN** システムはそのmessageをprompt数へ加算せず、prompt以外の利用証拠だけを正規化する

### Requirement: OpenCode Toolとtoken usageを正規化する

システムはOpenCode履歴から取得できるTool invocationと完了結果をruntime Tool観測として正規化し、raw name、安定したcanonical name、call identity、timestamp、および完了statusを保持しなければならない（SHALL）。command実行を表すOpenCode Toolはcanonical name `shell`へ正規化し、その他の既知Toolは集計可能な安定したcanonical nameへ変換しなければならない（SHALL）。assistant messageまたは同等のprovider usage metadataにtoken usageがある場合は、入力、cache、出力、およびreasoningの値を共通token usageへ加算しなければならない（SHALL）。

#### Scenario: OpenCodeのcommand Toolが完了する

- **WHEN** OpenCode履歴にcommand実行Toolとcompleted statusがある
- **THEN** システムはruntime layer、canonical name `shell`、success statusのTool観測を1件生成する

#### Scenario: OpenCodeのToolが失敗する

- **WHEN** OpenCode履歴にfailedまたはerror statusのTool完了結果がある
- **THEN** システムはruntime Tool Callsを1増やし、Failuresも1増やす

#### Scenario: token usageが保存されている

- **WHEN** OpenCodeのassistant usage metadataにtoken値がある
- **THEN** システムは取得できたtoken項目を正確な値でturnのtoken usageへ反映する

### Requirement: OpenCode履歴からSkill利用を検出する

システムはOpenCodeの実際のuser request、構造化されたSkill Tool、注入されたstructured Skill block、および具体的なSkill fileへのruntime accessから、既存のSkill evidence規則に従ってSkill利用を検出しなければならない（SHALL）。通常文中にSkill名らしい文字列があるだけではSkill利用を生成してはならない（MUST NOT）。同一source、Agent、session、turn、およびSkillの複数証拠は共通のdeduplication規則へ渡さなければならない（SHALL）。

#### Scenario: OpenCode userがSkill markerを入力する

- **WHEN** OpenCodeの実際のuser messageが先頭の`$skill-name`で始まる
- **THEN** システムは`explicit-request`のSkill evidenceを生成する

#### Scenario: OpenCodeのToolがSkill fileへアクセスする

- **WHEN** OpenCodeのruntime Tool argumentsが具体的な`SKILL.md`またはSkillの`scripts`配下へのアクセスを示す
- **THEN** システムは`implicit-access`、mode `implicit`、state `inferred`のSkill evidenceを生成する

#### Scenario: OpenCodeの通常文がSkill名に言及する

- **WHEN** assistantまたはuserの通常文に`$skill-name`や`SKILL.md`が現れるだけで、request、structured Tool、またはruntime accessの証拠がない
- **THEN** システムはSkill evidenceを生成しない

### Requirement: OpenCode履歴の部分的な問題から回復する

システムは個別の履歴row、JSON payload、Tool status、または未対応recordの破損や未知fieldによって、他の有効なOpenCode履歴の処理を中断してはならない（MUST NOT）。skipしたrecordの理由と件数はwarningとしてstderrへ出力し、stdoutのhuman-readable reportまたはJSONを汚染してはならない（MUST NOT）。source databaseを開けない、schemaを安全に解釈できない、または履歴全体の完全性を確認できない場合はsource errorとして非0で終了しなければならない（SHALL）。

#### Scenario: 未知の履歴fieldがある

- **WHEN** OpenCodeの履歴payloadに未対応のfieldが含まれる
- **THEN** システムは既知fieldを使った正規化を継続し、未知fieldだけを理由にrecordを失敗扱いしない

#### Scenario: 破損した履歴rowが混在する

- **WHEN** 1つのOpenCode履歴sourceに破損したpayloadと有効なpayloadが混在する
- **THEN** システムは破損rowをwarning対象としてskipし、有効rowからreportを生成する

#### Scenario: source databaseを開けない

- **WHEN** OpenCodeの永続履歴をread-onlyで開けない、または必要なschemaを判定できない
- **THEN** システムはsourceを解決できないerrorをstderrへ出力し、部分結果を完全なreportとして出力しない

### Requirement: OpenCode履歴へ期間filterと決定性を適用する

システムは`--days N`、`--from`、および`--to`で指定された期間をOpenCodeのmessage、Tool、Skill、およびtoken usageのtimestampへ適用しなければならない（SHALL）。timestampがない観測は有効な期間filter中へ暗黙に含めてはならない（MUST NOT）。同じdata root、source revision、filter、および基準時刻からは、履歴の列挙順やrow発見順に依存しない同じ正規化結果、Agent、count、およびrow順序を生成しなければならない（SHALL）。

#### Scenario: cutoff境界の観測を含める

- **WHEN** OpenCode観測のtimestampが`--days`から算出したcutoffと等しい
- **THEN** システムはその観測をreportへ含める

#### Scenario: 期間外のToolとtokenを除外する

- **WHEN** session内のToolまたはtoken usageのtimestampが選択期間外である
- **THEN** システムはその観測を集計から除外し、期間内の観測だけをreportへ含める

#### Scenario: 同一履歴を再読する

- **WHEN** userが同じOpenCode data rootと同じfilterで統計commandを複数回実行する
- **THEN** システムは同じsession、count、Agent、table順序、およびJSON値を生成する

### Requirement: OpenCode履歴とcacheをlocalかつread-onlyに扱う

システムはOpenCode履歴の統計生成中にOpenCode data root配下の履歴、metadata、およびschemaを変更してはならず（MUST NOT）、履歴内容を外部networkへ送信してはならない（MUST NOT）。正規化済みcacheを生成する場合も、cacheはOpenCode data rootとは分離されたuser cache directoryへ保存し、raw prompt本文、Tool arguments、およびraw provider payloadを保存してはならない（MUST NOT）。

#### Scenario: OpenCodeの統計を生成する

- **WHEN** userが任意の統計commandでOpenCode sourceを選択する
- **THEN** OpenCode data root内のfile、database、metadataは実行前後で変化せず、外部network通信も発生しない

#### Scenario: OpenCodeのcacheを生成する

- **WHEN** 履歴の完全な正規化が成功してcacheを保存する
- **THEN** システムはlocal user cacheへ最小限の正規化済み情報だけを保存し、OpenCode履歴のraw本文とTool argumentsをcacheへ含めない
