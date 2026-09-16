## Purpose

GitHub Copilot CLIがlocal machineへ保存するsession eventを、診断logやprovider payloadへ依存せず、CatSiftの利用統計へ安全かつ再現可能に取り込めるようにする。

## ADDED Requirements

### Requirement: GitHub Copilotのdata rootとsession eventを発見する

システムはGitHub Copilot sourceのdata rootとしてOS user home配下の`.copilot`を使用しなければならない（SHALL）。解決したrootの`session-state`配下にあるsession directoryごとの`events.jsonl`を履歴入力として発見し、session directory名をprovider session identityへ関連付けなければならない（SHALL）。`logs`、diagnostic log、`session-store.db`、およびその他のsession管理fileだけを利用統計の入力として解釈してはならない（MUST NOT）。

#### Scenario: 既定のCopilot rootを探索する

- **WHEN** userが`copilot` sourceを選択する
- **THEN** システムはuser home配下の`.copilot/session-state`を探索し、存在するsessionの`events.jsonl`を入力候補にする

#### Scenario: 診断logだけが存在する

- **WHEN** 解決したrootに診断logまたは`session-store.db`は存在するが、session eventの`events.jsonl`が存在しない
- **THEN** システムは診断logをsession、turn、Tool、またはSkillとして集計せず、空入力として扱う

#### Scenario: data rootを解決できない

- **WHEN** user home配下の`.copilot`が存在しない、directoryでない、または読み取り不能である
- **THEN** システムはsource errorを返し、部分的な成功reportを生成しない

### Requirement: Copilot session eventをstreamingかつ寛容に読み取る

システムは各`events.jsonl`をline単位で処理し、file全体をmemoryへ展開せずに有効なeventを後続の正規化へ渡さなければならない（SHALL）。各eventにはsource file、line番号、session identity、event ID、parent ID、timestamp、およびevent typeを取得できる範囲で関連付けなければならない（SHALL）。単独の不正JSON line、未知のevent type、未知のfield、または個別fileのskip可能な読取問題によって、他の有効なeventの処理を中断してはならない（MUST NOT）。

#### Scenario: event treeを保持する

- **WHEN** `events.jsonl`に`id`、`parentId`、`timestamp`、`type`、および`data`を持つeventが複数存在する
- **THEN** システムはeventの順序と親子identityを失わず、同じsessionの正規化処理へ渡す

#### Scenario: 不正lineの後続eventを処理する

- **WHEN** 有効なeventの間に不正JSON lineが存在する
- **THEN** システムはそのlineをwarningとしてskipし、後続の有効なeventから利用統計を生成する

#### Scenario: 未知eventを安全にskipする

- **WHEN** providerが新しい未知event typeを保存している
- **THEN** システムは既知eventの処理を継続し、未知eventのtypeとsource位置をwarningへ要約する

#### Scenario: skill.invoked eventを読み取る

- **WHEN** `events.jsonl`に`skill.invoked`と`data.name`、`data.path`、または`data.content`が存在する
- **THEN** システムは既知eventとして後続のSkill正規化へ渡し、pathとcontentをwarning、report、またはcacheへ出力しない

#### Scenario: warningをreportへ混入させない

- **WHEN** 不正lineまたは未知eventをskipしてhuman-readable reportまたはJSONを出力する
- **THEN** warningはstderrまたは既存のdiagnostic経路へ出力し、stdoutのreportとJSONを壊さない

### Requirement: Copilot session nameをmetadataから取得する

システムは各session directoryの`workspace.yaml`に保存されたtop-level `name`を、session eventとは分離したread-only metadataとして取得し、対応するsessionへ関連付けなければならない（SHALL）。`name`が存在しない、空である、またはmetadata fileを読めない場合、システムはsession IDを保持したまま利用統計の読取を継続しなければならない（SHALL）。`session-store.db`、logs、plan、checkpoint、およびraw event本文をsession nameの代替として解釈してはならない（MUST NOT）。

#### Scenario: workspace metadataの名前をsessionへ反映する

- **WHEN** session directoryの`workspace.yaml`にtop-level `name`が存在する
- **THEN** システムはその値を対応するSession detailのtitleとして保持する

#### Scenario: 名前のないsessionを安全に扱う

- **WHEN** `workspace.yaml`が存在しない、nameが空である、またはmetadataを読めない
- **THEN** システムは空のtitleとprovider session IDを保持し、他の有効なeventの集計を継続する

### Requirement: Copilot contextからproject metadataを取得する

システムは`session.context_changed`またはsession directoryの`workspace.yaml`にある`repository`を対応するSessionのproject nameとして保持し、`repository`がない場合は`gitRoot`または`cwd`の末尾をproject nameへfallbackしなければならない（SHALL）。同eventまたはmetadataの`cwd`、または取得可能なproject pathはproject pathとして保持しなければならない（SHALL）。project metadataが欠損してもevent集計を中断してはならない（MUST NOT）。

#### Scenario: repositoryをproject nameへ反映する

- **WHEN** `session.context_changed`に`repository: "owner/project"`、`gitRoot`、および`cwd`が存在する
- **THEN** システムはproject nameに`owner/project`を、project pathに`cwd`を設定し、Session detailとJSONへ反映する

#### Scenario: repositoryのないcontextをfallbackする

- **WHEN** `session.context_changed`に`repository`がなく、`gitRoot`または`cwd`が存在する
- **THEN** システムはpathの末尾をproject nameへ設定し、取得できたproject pathを保持する

#### Scenario: workspace metadataからproject nameを取得する

- **WHEN** `session.context_changed`がなく、`workspace.yaml`のtop-levelに`repository`、`git_root`、または`cwd`が存在する
- **THEN** システムはmetadataからproject nameとproject pathを設定し、event集計を継続する

### Requirement: Copilot履歴をread-onlyかつlocal-onlyで扱う

システムはGitHub Copilotのsource file、directory、session管理fileを統計生成のために変更してはならず（MUST NOT）、履歴内容をnetworkへ送信してはならない（MUST NOT）。source errorまたはskip可能なwarningが発生しても、既に読み取ったraw prompt、tool argument、provider payloadを外部へ出力してはならない（MUST NOT）。

#### Scenario: 履歴fileを変更しない

- **WHEN** userがGitHub Copilot sourceの統計commandを実行する
- **THEN** 入力fileの内容、size、permission、およびmodification timeは実行前後で変化しない

#### Scenario: raw内容を外部へ送信しない

- **WHEN** Copilot履歴からreportまたはcacheを生成する
- **THEN** システムはnetwork requestを行わず、prompt本文とtool argumentを外部へ渡さない
