## MODIFIED Requirements

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
- **THEN** システムは既知eventの処理を継続し、未知eventのtypeとsource位置と件数をwarningへ要約し、CLI/TUIの案内は統計への影響が未確認であることを示す。machine-readable warningのreason、type、source、path、line、countは維持する

#### Scenario: skill.invoked eventを読み取る

- **WHEN** `events.jsonl`に`skill.invoked`と`data.name`、`data.path`、または`data.content`が存在する
- **THEN** システムは既知eventとして後続のSkill正規化へ渡し、pathとcontentをwarning、report、またはcacheへ出力しない

#### Scenario: warningをreportへ混入させない

- **WHEN** 不正lineまたは未知eventをskipしてhuman-readable reportまたはJSONを出力する
- **THEN** warningはstderrまたは既存のdiagnostic経路へ出力し、stdoutのreportとJSONを壊さない

## ADDED Requirements

### Requirement: 確認済みのmetadataと参照形式Skillを区別する

システムは確認済みのevent typeを明示的に受理しなければならない（SHALL）。metadataからturn、prompt、Tool、token観測を生成してはならない（MUST NOT）。`skill.invoked_ref`はactive turn内で確認済みのnameまたはpathから既存の`skill.invoked`と同じSkill evidenceを生成し、同一turn・同一Skillの利用を重複集計してはならない（MUST NOT）。本文参照の展開や未確認fieldの推測を行ってはならない（MUST NOT）。

#### Scenario: 報告されたmetadataを受理する

- **WHEN** `abort`、`human_response.recorded`、`subagent.selected`、`subagent.configured`、`session.binary_asset`、`session.mode_changed`、`session.task_complete`、`session.mode_notice_delivered`、`session.permissions_changed`、`session.warning`、`subagent.failed`を読み取る
- **THEN** unknown type警告を生成せず、これらだけでturnや利用観測を生成しない。metadataだけのfileも既存metadataと同様にsession identityと時刻を保持し、期間filterなしの利用実績のないsession一覧へ表示できる

#### Scenario: 参照形式のSkillを重複排除する

- **WHEN** 同一turnに同一Skillの`skill.invoked`と`skill.invoked_ref`が存在する、または参照形式だけが存在する
- **THEN** 正規化できるSkillごとに1件のconfirmed evidenceを保持し、path、contentId、Skill本文をreportやcacheへ保存しない

#### Scenario: 別turnで同じSkillを使う

- **WHEN** 別turnに同じSkillの参照形式eventが存在する
- **THEN** 各turnでSkill利用を保持し、本文のcontentIdを使ってsession全体を重複排除しない

#### Scenario: 利用を特定できない参照形式

- **WHEN** `skill.invoked_ref`に解釈可能なnameとpathがない
- **THEN** 未確認のfieldからSkill利用を推測せず、turnを生成せず、既存turnの時刻・観測を変更しない

#### Scenario: 旧cacheから再構築する

- **WHEN** 修正前のCopilot parser cacheに既知typeの警告と欠落したSkill利用が存在する
- **THEN** 旧cacheを無効化して再解析し、cold/warm cacheで警告と集計が一致し、既存の`session.usage_record`集計も維持する

#### Scenario: 帰属不明の参照を統計から除外する

- **WHEN** active turnがない状態、turn終了後、またはsession.shutdown後にidentityを持つ`skill.invoked_ref`が存在する
- **THEN** システムはturnも参照だけのsessionも新設せず、既存turn・session時刻・shutdown由来のtoken帰属を変更せず、帰属不能な参照件数とtype/sourceを既存のdiagnostic経路に記録する。inline側に集計済みの同名Skillがあっても帰属不能な参照件数を保持し、集計欠落の断定を避ける。参照のraw payloadは保存せず、cold/warm cacheで診断件数と統計を維持する

#### Scenario: inline identityの既存互換性を維持する

- **WHEN** 既存の`skill.invoked`にscalarのnameが存在する
- **THEN** 既存のstring変換によるidentity解決を維持する。新規の`skill.invoked_ref`は公開schemaのstring name/pathだけを受理する
