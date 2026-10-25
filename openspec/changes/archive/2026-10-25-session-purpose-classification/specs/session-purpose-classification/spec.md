# Session Purpose Classification Specification

## Purpose

ユーザーが任意に有効化した場合だけ、sessionの作業目的を分類し、その結果をsession detailで確認できるようにする。

## ADDED Requirements

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

classification結果はdisposable ingestion cacheとは別に保存し、cache eviction後も再利用しなければならない（SHALL）。storeにはraw promptを保存せず、input fingerprint、taxonomy fingerprint、classifier versionのいずれかが変化した場合は再分類しなければならない（SHALL）。API keyおよびraw promptをlogへ出力してはならない（MUST NOT）。

#### Scenario: 同じ入力を再利用する

- **WHEN** input、taxonomy、classifier versionが前回分類時と一致する
- **THEN** systemは保存済み結果を使いAPIを再度呼び出さない

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
