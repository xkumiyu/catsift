# session-purpose-classification Specification

## Purpose
ユーザーが任意に有効化した場合だけ、sessionの作業目的を分類し、その結果をsession detailで確認できるようにする。

## Requirements

### Requirement: YAML設定を生成して編集する

分類設定は`<user-config-dir>/catsift/config.yaml`の`session_purpose` sectionから読み込まなければならない（SHALL）。分類固有の設定はこのsectionに含め、root直下に置いてはならない（MUST NOT）。`catsift config init`は分類を無効にしたYAML設定ひな形を生成し、既存設定を上書きしてはならない（MUST NOT）。ひな形には編集可能なtaxonomyを含めてはならない（MUST NOT）。

#### Scenario: 設定を初期化する

- **WHEN** 利用者が既存設定のない環境で`catsift config init`を実行する
- **THEN** 分類が無効でtaxonomy設定を含まないYAML設定を生成する

#### Scenario: 既存設定を保護する

- **WHEN** YAML設定がすでに存在する状態で`catsift config init`を実行する
- **THEN** 既存設定を維持してerrorを返す

### Requirement: session purposeを明示的opt-inで分類する

分類機能は既定で無効でなければならない（MUST）。有効な`TYPESAFE_API_KEY`がある場合だけTypeSafe System One APIへrequestし、model `jev-latest`とChoice questionを使用しなければならない（SHALL）。API URLはTypeSafe System One request/response形式に互換なendpointとして設定可能で、既定値は`https://api.typesafe.ai/v1/systemone`でなければならない（SHALL）。loopback以外のendpointにはHTTPSを要求しなければならない（SHALL）。URLの差し替えだけで他のAI gateway APIとの互換性を保証してはならない（MUST NOT）。

#### Scenario: keyがない

- **WHEN** 分類が設定されているが`TYPESAFE_API_KEY`がない
- **THEN** API requestを一切行わず、既存のusage reportを継続する

#### Scenario: HTTPS以外の外部endpointを拒否する

- **WHEN** 分類が有効で、configured API URLがloopback以外のHTTP endpointを指す
- **THEN** systemはrequestを送信せず分類をskipする

### Requirement: 分類入力はuser-authored内容に限定する

requestはsession titleと実際のuser-authored messageを含め、injected system/developer/environment context、assistant output、tool arguments、tool outputを含めてはならない（MUST NOT）。入力は8,000 Unicode code points以下とし、切り詰め時は先頭と末尾を維持して省略を明示しなければならない（SHALL）。

#### Scenario: user messageだけを送信する

- **WHEN** 履歴にuser message、assistant output、tool argumentsおよびtool outputが含まれる
- **THEN** Jev requestにはtitleとuser-authored内容だけが含まれる

#### Scenario: 長い入力を制限する

- **WHEN** titleとuser messageの連結が8,000 code pointsを超える
- **THEN** requestは冒頭と末尾を保ち、省略markerを含む8,000 code points以下となる

### Requirement: 固定されたtaxonomyを使用する

分類はCatsiftに組み込まれたAnthropicの9 work modesと固定されたID、名称、説明を使わなければならない（SHALL）。YAML設定からtaxonomyを変更できてはならない（MUST NOT）。

#### Scenario: 固定taxonomyを使う

- **WHEN** classifierがChoice questionを送信する
- **THEN** requestは組み込みの9 work modesをChoice optionsとして使う

#### Scenario: YAMLからtaxonomyを変更しない

- **WHEN** 利用者が`session_purpose.modes`をYAML設定に指定する
- **THEN** strict config parserは未知fieldとしてerrorを返す

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

### Requirement: Session detailで分類を確認する

Session detailは利用可能な分類結果を表示し、分類が無効・未実行・失敗の場合には誤解を招く分類を表示してはならない（MUST NOT）。

#### Scenario: 分類済みsessionを表示する

- **WHEN** sessionに有効な保存済みclassificationがある
- **THEN** Session detailはpurpose名を表示する

### Requirement: 大量の新規分類request前に確認する
20件以上の新規分類が必要な場合、systemはAPI requestの前にinteractive terminalで明示的な確認を求めなければならない（SHALL）。確認数は有効な保存済み結果を除いたpending session数とし、回答がyes以外、またはinteractive terminalがない場合はrequestを一切行ってはならない（MUST NOT）。TUI reloadでは確認inputを使わず、しきい値以上のbatchをskipしてwarningを出さなければならない（SHALL）。並列API requestは最大4件とする（MUST）。

#### Scenario: 19件以下の新規分類

- **WHEN** 保存済み結果を除いたpending sessionが19件以下
- **THEN** 確認なしで分類requestを行う

#### Scenario: 20件以上の分類で拒否する

- **WHEN** pending sessionが20件以上で利用者が確認を拒否する
- **THEN** API requestを行わず、通常のusage reportを継続する

#### Scenario: 保存済み結果を確認数から除く

- **WHEN** 合計20 sessionのうち1 sessionに現在有効な保存済み結果がある
- **THEN** pending数は19件となり、一括確認を表示しない

#### Scenario: interactive terminalなしで大量分類する

- **WHEN** pending sessionが20件以上でinteractive terminalがない
- **THEN** API requestを行わず、分類をskipしたwarningを出す

#### Scenario: TUI reloadで大量分類する

- **WHEN** TUI reload後にpending sessionが20件以上ある
- **THEN** API requestを行わず、分類をskipしたwarningを出す

### Requirement: Overviewで分類統計を表示する
現在のfilterに含まれるsessionに有効な分類結果が1件以上ある場合、TUI Overviewは分類されたすべてのpurposeとそれぞれのsession数を表示しなければならない（SHALL）。分類済み合計をpurpose list内の項目として表示してはならない（MUST NOT）。purpose listはTop Skillsの後に置き、各件数にはsession単位を表示する。分類済みsessionがない場合は分類統計を表示してはならない（MUST NOT）。

#### Scenario: 分類済みsessionの統計を表示する

- **WHEN** 現在のfilterに分類済みsessionが含まれる
- **THEN** Overviewは分類されたすべてのpurposeとpurpose別session数を表示し、合計行を表示しない

#### Scenario: 分類済みsessionがない

- **WHEN** 現在のfilterに分類済みsessionがない
- **THEN** OverviewはSession Purpose sectionを表示しない

#### Scenario: Purpose valueを通常色で表示する

- **WHEN** Session detailにPurposeがある
- **THEN** Purposeのlabelはmuted style、分類名は通常色で表示する

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
