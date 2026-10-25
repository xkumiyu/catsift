# 設計

## 方針

分類は明示的な設定がある場合だけ実行する。`TYPESAFE_API_KEY`が空ならHTTP clientを呼ばず、結果を未分類として扱う。API URLはTypeSafe System One request/response形式に互換なendpointに限って設定可能とし、既定値は`https://api.typesafe.ai/v1/systemone`、modelは`jev-latest`、question typeはChoiceとする。URL設定だけではOpenAI-compatible APIなど別形式へ対応しない。

設定は`<user-config-dir>/catsift/config.yaml`にYAML形式で置き、分類設定を`session_purpose` sectionにまとめる。taxonomyはCatsift内の固定された9 work modesを使い、設定からは変更できない。`catsift config init`は分類を無効にしたひな形を排他的に作成し、既存ファイルを上書きしない。

分類入力はsession titleとsource adapterがuser-authoredと認識した全messageから作る。system/developer/environment context、assistant output、tool arguments/outputは対象外とする。8,000 code pointsを超える場合、冒頭と末尾を残し、省略markerを入れる。

分類結果はOS user config directory配下の専用storeにatomic writeする。disposable ingestion cacheとは分離し、session keyごとにinput fingerprint、taxonomy fingerprint、classifier version、stable choice IDを保存する。raw inputとAPI keyは保存・表示・diagnostic出力しない。

## 大量requestの確認

新規分類requestが20件以上ある場合、既存結果を差し引いた件数を表示してinteractive terminalで確認する。`yes`以外はrequestを送らず、通常のreportは継続する。interactive terminalがない実行とTUI reloadは確認を得られないためbatch全体をskipしてwarningを出す。requestは最大4件まで並列化する。

## Overviewの統計

現在のfilterに分類済みsessionがある場合だけ、TUI Overviewに分類済み合計とpurpose別件数を表示する。各数値はsession単位で示す。Session detailではPurpose labelをmuted表示し、分類名は通常色で表示する。

## 統合

設定の既存読み込み方式とSession detailのRead Modelを再利用する。API呼び出しと永続storeは内部packageに閉じ、API URLを差し替えたHTTP test serverで検証する。分類の失敗は既存のusage reportを失敗させず、そのsessionを未分類として扱う。
