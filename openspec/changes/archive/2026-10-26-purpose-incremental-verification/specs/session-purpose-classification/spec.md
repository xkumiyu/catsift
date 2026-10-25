## MODIFIED Requirements

### Requirement: 結果をraw promptなしで専用storeへ保存する

classification結果はdisposable ingestion cacheとは別に保存し、cache eviction後も再利用しなければならない（SHALL）。storeにはraw promptを保存せず、input fingerprint、taxonomy fingerprint、classifier version、検証時刻、revision証拠を保存しなければならない（SHALL）。storeは`schema_version`を持ち、store形式の互換性をclassifier versionとは独立に管理しなければならない（SHALL）。API keyおよびraw promptをlogへ出力してはならない（MUST NOT）。

classifier versionはCLI release versionから独立し、通常のCLI修正では更新しない。分類結果の互換性を失う入力生成・分類指示・taxonomy・model等の変更時にのみ更新の必要性を判断し、更新時は既存結果への影響と移行・再分類方針を検討する。

#### Scenario: 同じ入力を再利用する

- **WHEN** input、taxonomy、classifier versionが前回分類時と一致する
- **THEN** systemは保存済み結果を使いAPIを再度呼び出さない

#### Scenario: store形式を移行する

- **WHEN** `schema_version`を持たない既存storeを読み込む
- **THEN** systemは旧形式として結果を維持し、次回保存時に現行schema versionを書き込む

#### Scenario: 検証時刻を記録する

- **WHEN** sessionのinput fingerprintが保存値と一致する
- **THEN** systemはAPIを呼び出さず、そのentryの検証時刻とrevision証拠を更新する

## ADDED Requirements

### Requirement: 新規sessionだけを既定で分類し既存sessionを証拠で再利用する

分類が有効な実行は、新規sessionを分類しなければならない（SHALL）。保存済み結果を持つsessionは、classifier version、taxonomy fingerprint、またはrevision証拠が変化した場合だけ本文をparseして再検証しなければならない（SHALL）。revision証拠が一致するsessionでは本文を追加parseしてはならない（MUST NOT）。revision証拠はsession title、更新時刻、選択されたprompt数とprompt時刻列、およびCodexで取得可能な場合はsource file revisionから作らなければならない（SHALL）。OpenCode、GitHub Copilot、ctxではsource pathやfile revisionを証拠に含めてはならない（MUST NOT）。

#### Scenario: 既存sessionが変化していない

- **WHEN** 保存済み結果を持ち、version、taxonomy、revision証拠がいずれも一致するsessionがある
- **THEN** systemは本文をparseせず保存済み結果を再利用し、API requestを送らない

#### Scenario: 新規sessionを分類する

- **WHEN** storeに対応する結果を持たないuser-authored message付きsessionがある
- **THEN** systemはそのsessionの本文をparseして分類する

#### Scenario: titleが変化した

- **WHEN** 保存済み結果を持つsessionのtitleまたは更新時刻が変化している
- **THEN** systemはそのsessionの本文だけをparseしてfingerprintを照合する

#### Scenario: 期間変更で対象promptが変わる

- **WHEN** 期間指定の変更でsessionの選択済みpromptが変化し、prompt数が同じかsession更新時刻が変わっていない
- **THEN** systemは変更を検出して本文のfingerprintを照合し、入力が異なる場合は再分類する

#### Scenario: cacheの有無でsource pathが変わる

- **WHEN** 同じOpenCode、GitHub Copilot、ctxのhistoryをcache missとcache hitで読み込む
- **THEN** systemはsource pathの有無に影響されず同じrevision証拠を生成する

### Requirement: 予算付き巡回検証でstaleの上限を設ける

systemは、revision証拠に依存する再利用の見逃しに備え、検証時刻が7日以上前のsessionを検証時刻の古い順に最大50件選び、再検証しなければならない（SHALL）。検証時刻を持たないentryを最も古いものとして扱う（SHALL）。検証時刻が7日以内のsessionは巡回検証の対象にせず、revision証拠による再利用を優先する（SHALL）。巡回検証は本文parseとinput fingerprintの照合だけを行い、fingerprintが一致した場合はAPI requestを送ってはならない（MUST NOT）。fingerprintが一致しない場合だけ分類対象に加える（SHALL）。

#### Scenario: 巡回検証で一致する

- **WHEN** 巡回検証の対象sessionのinput fingerprintが保存値と一致する
- **THEN** systemはAPI requestを送らず、検証時刻を更新する

#### Scenario: 巡回検証で不一致になる

- **WHEN** 巡回検証の対象sessionのinput fingerprintが保存値と一致しない
- **THEN** systemはそのsessionを分類対象に加える

#### Scenario: 検証に失敗する

- **WHEN** 対象sessionの本文parseまたはfingerprint計算に失敗する
- **THEN** systemはそのentryの検証時刻を更新せず、次回の実行で再試行する

#### Scenario: 巡回検証の本文を取得できない

- **WHEN** revision証拠が一致する巡回検証対象の本文を取得できない
- **THEN** systemは保存済み分類をreportに維持し、検証時刻を更新しない

### Requirement: 分類のための本文parseを再検証対象に限定する

分類が有効な実行は、まずmessage bodyを保存しないingestion cacheからsession一覧を読み込み、再検証対象を決定しなければならない（SHALL）。本文は再検証対象のsessionに限ってsourceからparseし、対象外sessionの本文をparseしてはならない（MUST NOT）。Codexでは対象sessionに対応するfileだけを読み、単一databaseを読むsourceでは対象sessionが存在する場合に限りdatabaseをparseする（SHALL）。parseした本文はingestion cacheへ書き込んではならない（MUST NOT）。

#### Scenario: 対象外sessionの本文を読まない

- **WHEN** 証拠一致で再利用できるsessionだけがhistoryに含まれる
- **THEN** systemはどのsourceからもmessage bodyをparseしない

#### Scenario: Codexの対象fileだけを読む

- **WHEN** 再検証対象のCodex sessionが特定のsource fileに対応する
- **THEN** systemはそのfileだけを本文parseの対象にする

#### Scenario: 本文をcacheへ保存しない

- **WHEN** 分類のためにsourceから本文をparseする
- **THEN** ingestion cacheの内容は変化せず、message bodyは永続化されない

#### Scenario: cache missで本文を取得済み

- **WHEN** 再検証対象sessionの全turnで、取得済みmessage数と選択済みprompt数が一致する
- **THEN** systemはその本文を再利用し、そのsessionを分類のために追加parseしない

#### Scenario: 本文が不足するsessionだけを追加loadする

- **WHEN** 再検証対象に、本文を取得済みのsessionと本文が不足するsessionが混在する
- **THEN** systemは本文が不足するsessionだけを本文loaderへ渡す

#### Scenario: OpenCodeのmessageが複数のtext partを持つ

- **WHEN** 1つのuser messageが複数のtext partから構成される
- **THEN** systemは注入されたSkill本文だけのpartを除外し、残りを1つの分類用messageとして扱い、完全な本文を追加parseしない

#### Scenario: 一部sourceの本文parseに失敗する

- **WHEN** 再検証対象のうち1つのsourceで本文parseに失敗する
- **THEN** systemは他sourceの分類と結果保存を継続し、失敗をwarningとして通知する
