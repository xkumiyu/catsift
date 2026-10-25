## MODIFIED Requirements

### Requirement: ctx履歴を公開された読み取り専用イベント列挙から取得する

`ctx` sourceが選択された場合、システムはctxが提供する公開された読み取り専用の履歴イベント列挙を入力として使用しなければならない（SHALL）。ctxの `usage.sqlite`、検索インデックス、またはその他の内部schemaを統計入力として直接参照してはならず（MUST NOT）、ctxまたはprovider-owned履歴を変更してはならない（MUST NOT）。履歴内容をnetworkへ送信してはならない（MUST NOT）。ただし、session purpose分類が明示的に有効で`TYPESAFE_API_KEY`が設定されている場合、分類用に抽出したsession titleと実際のuser-authored messageだけをTypeSafe System One互換endpointへ送信してよい。

#### Scenario: ctx sourceを選択する

- **WHEN** userが統計commandへ `--source ctx` を指定する
- **THEN** システムはctxの読み取り専用イベント列挙から入力を取得し、Codex home配下のJSONLを入力へ含めない

#### Scenario: ctxの集計用SQLiteだけが存在する

- **WHEN** ctxの `usage.sqlite` にctx自身の集計行が存在するが、履歴イベント列挙を利用できない
- **THEN** システムは集計行をsession・prompt・Tool・Skillの履歴として解釈せず、入力source errorとして非0で終了する

#### Scenario: ctx入力を実行する

- **WHEN** userがctx sourceで任意の統計commandを実行する
- **THEN** ctxのstorageとprovider-owned履歴は変化せず、分類が無効またはkey未設定なら外部network通信を行わず、分類を有効にした場合もsession titleと実際のuser-authored messageだけを送信する
