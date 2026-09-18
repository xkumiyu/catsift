## ADDED Requirements

### Requirement: GitHub Copilot eventを共通usage eventへ正規化する

システムはGitHub Copilot raw eventから取得できるsession、turn、user prompt、Tool、Skill、Model、およびtoken usageを、既存の共通usage eventへ正規化しなければならない（SHALL）。正規化済み観測のsourceは`copilot`、canonical Agent identityは`copilot`とし、session identityはsource、Agent、およびCopilotのprovider session identityを組み合わせたsource-qualified keyでなければならない（SHALL）。

#### Scenario: Copilot sessionをsource-qualifiedにする

- **WHEN** GitHub Copilotの異なるsession directoryに同じ文字列のevent IDまたはturn IDが存在する
- **THEN** システムはsessionを統合せず、各観測を`copilot` Agentの別sessionとして保持する

#### Scenario: Copilot session nameを保持する

- **WHEN** session eventに対応する`workspace.yaml`からsession nameを取得できる
- **THEN** システムはsource-qualified sessionの`Title`へnameを設定し、event本文をtitleの代わりに保存しない

#### Scenario: Copilot project metadataを保持する

- **WHEN** Copilot sessionに`session.context_changed`または`workspace.yaml`の`repository`、`gitRoot`/`git_root`、または`cwd`が存在する
- **THEN** システムはrepositoryまたはpath末尾をsource-neutral Sessionの`ProjectName`へ、cwdまたは取得できたpathを`ProjectPath`へ設定する

#### Scenario: 実際のuser promptだけを数える

- **WHEN** Copilot sessionにuser message、system message、assistant message、およびtool outputが存在する
- **THEN** システムは実際のuser messageだけをuser promptへ加算し、system・assistant・tool outputをpromptへ加算しない

#### Scenario: turn境界とModelを保持する

- **WHEN** Copilot eventがturn identity、turn境界、またはModel変更を示す
- **THEN** システムはturnをsource位置とtimestampへ関連付け、turnごとのModel identityを後続のModel統計へ渡す

### Requirement: CopilotのTool、Skill、およびtoken usageを二重計上せずに正規化する

システムはCopilot eventから取得できるTool開始・完了、成功・失敗、model call、Skill evidence、およびtoken usageを、既存のmodel/runtime layerとeffective viewの規則へ適用しなければならない（SHALL）。同じTool callの開始・完了eventを複数の利用として数えてはならず（MUST NOT）、対応するruntime観測がある場合に外側のmodel wrapperをeffective Toolへ追加してはならない（MUST NOT）。取得できないfieldは推測で補わず、利用可能な観測だけを保持しなければならない（SHALL）。

#### Scenario: Tool executionを1回へ統合する

- **WHEN** Copilot sessionに同じ`toolCallId`のexecution startとexecution completeが存在する
- **THEN** システムはcanonical Tool利用を1回として保持し、complete statusに応じて失敗を記録する

#### Scenario: runtime観測をeffective viewへ優先する

- **WHEN** 1つのCopilot model callが複数のruntime Tool executionを生成する
- **THEN** effective Tool viewはruntime executionを採用し、外側のmodel callを追加のTool利用として数えない

#### Scenario: Modelとtoken usageを対応付ける

- **WHEN** Copilot eventがModel identityとinputまたはoutput token usageを報告する
- **THEN** システムはprovider `copilot`、Model name、token種別、および観測timestampを保持する

#### Scenario: Copilotのshutdown token usageを正規化する

- **WHEN** persisted Copilot `session.shutdown.data.modelMetrics[*].usage`が`inputTokens`、`outputTokens`、`cacheReadTokens`、`cacheWriteTokens`、または`reasoningTokens`を含む
- **THEN** システムは各値を共通TokenUsageのinput、output、cached input、cache-write input、reasoning outputへ写像し、totalがない場合はinputとoutputから補完し、Model identityとtimestampを保持する。turn境界がない場合もaggregate-only turnへ保持する

#### Scenario: Skill evidenceが構造化されている

- **WHEN** Copilot eventがSkill toolまたはSkill fileへの具体的なaccessを示す
- **THEN** システムは既存のSkill evidence規則に従ってSkill利用を記録し、通常文中の文字列だけからSkill利用を推測しない

#### Scenario: skill.invokedをconfirmed evidenceへ正規化する

- **WHEN** Copilot eventが`skill.invoked`と有効なSkill nameを示す
- **THEN** システムはSkill nameをconfirmed・mode unknownのevidenceとして該当turnへ記録し、eventのpathとcontentを正規化結果へ残さない
