# tui-usage-explorer Specification

## Purpose

ローカルに保存されたAgent履歴を、Model、利用状況、個別Skill、SessionとTurnの関係で安全かつ対話的に探索できるTUIを提供する。

## Requirements

### Requirement: TUIは四つの探索対象を提供する

システムはTUIでOverview、Models、Skills、Sessionsのtop-level viewを提供しなければならない（SHALL）。Overviewは対象scopeのSession、Turn、prompt、Token、Tool、Skill利用状況を表示しなければならない（SHALL）。ModelsはproviderとModel nameごとの利用状況を表示し、SkillsはSkill名ごとの利用状況を表示し、SessionsはSession一覧を表示しなければならない（SHALL）。

#### Scenario: Overviewを表示する

- **WHEN** userがTUIを起動してOverview viewを選択する
- **THEN** システムは対象source、Agent、期間と、Session、Turn、prompt、Token、Tool、Skill利用状況を表示する

#### Scenario: Model一覧を表示する

- **WHEN** userがModels viewを選択する
- **THEN** システムはModelのprovider・name、Session数、Turn数、Token usage、およびfirst/last usedを決定的な順序で表示する

#### Scenario: Skill一覧を表示する

- **WHEN** userがSkills viewを選択する
- **THEN** システムはdeduplicated Skill useの件数、Session数、および最終利用時刻をSkillごとに表示する
- **AND** システムはmode・state・検出methodの内訳をSkill detailで表示する

#### Scenario: Session一覧を表示する

- **WHEN** userがSessions viewを選択する
- **THEN** システムはSession名称、Project、および最終利用時刻を表示し、Turn・TokenなどのcountはSession detailで表示する

#### Scenario: 一覧の最終利用時刻を相対表示する

- **WHEN** userがModels、Skills、またはSessionsの一覧を表示する
- **THEN** システムは最終利用時刻を`now`、`Nm ago`、`Nh ago`、`Nd ago`、`Nmo ago`、または`Ny ago`の相対表記で表示し、不明な時刻は`—`と表示する
- **AND** システムはModel、Skill、またはSession detailでは正確な時刻を表示する

### Requirement: Codex Session名称を表示する

Codex sourceでは、存在する`<CODEX_HOME>/session_index.jsonl`を読み、Session IDに対応する最新の非空`thread_name`をSessionの表示名称として使用しなければならない（SHALL）。indexが存在しない、または一部recordを読めない場合も、rollout履歴の読み込みを中断してはならない（MUST NOT）。cache使用時もindexを再読し、名称変更を反映しなければならない（SHALL）。

#### Scenario: Codex Session名称を表示する

- **WHEN** `session_index.jsonl`に履歴Session IDと`thread_name`が存在する
- **THEN** システムはSessions一覧に`thread_name`を表示し、Session detailにSession nameと完全なSession IDを分けて表示する

#### Scenario: Session名称がない場合の一覧表示

- **WHEN** Sessionに表示名称がなく、Sessions viewを表示する
- **THEN** システムは括弧付きかつmuted表示のSession ID先頭8文字を名称の代替として表示し、ID fallbackであることを示す

#### Scenario: Session名称がない場合のdetail表示

- **WHEN** 名称のないSessionのdetailを表示する
- **THEN** システムはSession nameを空値表示（`—`）とし、Session IDには完全なIDを表示する

### Requirement: 一覧から詳細を探索できる

システムはModels、Skills、Sessionsの一覧で選択した項目のdetail viewへ遷移できなければならない（SHALL）。Skill detailはSkill全体のSession/Turn数とmode・stateの内訳、および利用Session単位に集約したdistinct Turn数、時刻、検出methodの内訳を表示しなければならない（SHALL）。Session detailはSession metadataとTurnを時系列で表示し、TurnごとのModel、Token usage、canonical Tool name、Skill利用を表示しなければならない（SHALL）。

#### Scenario: 個別Skillの詳細を開く

- **WHEN** userがSkills viewでSkillを選択してdetailを開く
- **THEN** システムはそのSkillのdeduplicated useをSessionごとに集約し、各Sessionのdistinct Turn数、first/last used、検出methodの内訳を表示する

#### Scenario: Sessionの詳細を開く

- **WHEN** userがSessions viewでSessionを選択してdetailを開く
- **THEN** システムはSession metadataと、古いTurnから新しいTurnの順にTurn summaryを表示する

#### Scenario: 詳細から一覧へ戻る

- **WHEN** userがModel、Skill、またはSession detailでback操作を行う
- **THEN** システムは直前の一覧viewへ戻り、一覧のfilterとselection contextを維持する

### Requirement: TUIは共通filterと決定的なsortを適用する

TUIはsource、Agent、期間、Project、Model、Skillのfilterを適用しなければならない（SHALL）。Model filterはそのModelを含むModel observationまたはToken usage eventに関連するTurnだけを対象とし、Skill filterはそのSkill useに関連するTurnだけを対象としなければならない（SHALL）。filter後に関連する利用がないSessionは一覧から除外しなければならない（SHALL）。同じ入力と基準時刻に対して、入力の列挙順にかかわらず同じ値と順序を表示しなければならない（SHALL）。

#### Scenario: Model filterを適用する

- **WHEN** userがModel `gpt-example`をfilterとして指定する
- **THEN** システムはそのModelを観測したTurnとSessionだけを残し、Model別およびOverviewのToken usageをそのscopeで表示する

#### Scenario: Skill filterを適用する

- **WHEN** userがSkill `review`をfilterとして指定する
- **THEN** システムは`review`のuseを含むTurnとSessionだけを残し、Skill detailへ同じdeduplicated evidenceをSession単位で集約して表示する

#### Scenario: 同数の一覧を表示する

- **WHEN** 複数のModel、Skill、またはSessionの主要countが同じである
- **THEN** システムはcanonical nameまたはsource-qualified keyによるtie-breakerを適用し、列挙順に依存しない順序で表示する

#### Scenario: Session IDで検索する

- **WHEN** userがSessions viewでSession IDの一部または全部を検索する
- **THEN** システムは表示名称の有無にかかわらず該当Sessionを表示する

### Requirement: TUIは履歴内容を漏えいさせない

TUIはprompt本文、command本文、Tool arguments、Skill本文、provider payloadを通常のviewまたはdetail viewに表示してはならない（MUST NOT）。Query結果とTUI stateは、集計値、canonical name、識別子、時刻、状態、および既存のsanitized metadataだけを保持しなければならない（SHALL）。TUIから履歴の変更、削除、再実行、Skill実行を行ってはならない（MUST NOT）。

#### Scenario: Session detailを表示する

- **WHEN** userがpromptやTool argumentsを含むSessionをdetailで開く
- **THEN** システムはTurnのsummaryとcanonical Tool nameだけを表示し、prompt本文とargumentsを表示しない

#### Scenario: TUIで変更操作を試みる

- **WHEN** userがTUIから履歴のedit、delete、rerun、またはSkill実行に相当する操作を行う
- **THEN** システムはその操作を提供せず、履歴を変更しない

### Requirement: 大量の一覧をterminal内で安全に閲覧できる

TUIはterminalの幅と高さに応じて一覧をviewport内へ表示しなければならない（SHALL）。長い表示名はrowを壊さない形で省略し、狭いterminalでは一覧とdetailを一つのpaneで切り替えられなければならない（SHALL）。

#### Scenario: 一覧がterminal高さを超える

- **WHEN** Model、Skill、またはSessionのrow数が表示可能な高さを超える
- **THEN** システムは上下移動で表示範囲をwindowingし、選択中のrowを表示範囲内に保つ

#### Scenario: terminal幅が狭い

- **WHEN** terminal幅が複数paneまたは全columnの表示に不足する
- **THEN** システムはSession名称と最終利用時刻などの主要metadataを残したcompact viewへ切り替え、rowを誤解を招く形で折り返さない

### Requirement: warningとreload状態をTUI内で確認できる

TUIはrecoverableな履歴warningを結果と分離して保持し、warning countまたはdiagnosticsとして確認できるようにしなければならない（SHALL）。起動時は一貫したsnapshotを表示し、明示的なreload操作が完了するまで現在のviewを破棄してはならない（SHALL）。

#### Scenario: 一部履歴をskipして起動する

- **WHEN** 一部の履歴recordにrecoverableな問題があり、残りからsnapshotを生成できる
- **THEN** システムは有効な結果を表示し、warningの存在と件数をTUI内で確認可能にする

#### Scenario: 明示的にreloadする

- **WHEN** userがreload操作を行う
- **THEN** システムは新しいsnapshotとQuery結果を読み込み、完了後に現在のviewを新しい結果へ切り替える
