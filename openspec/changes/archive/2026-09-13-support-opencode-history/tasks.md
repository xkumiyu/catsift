## 1. 共通source基盤と依存関係

- [x] 1.1 `modernc.org/sqlite`をGo moduleへ追加し、CGOなしで依存関係を解決できることを`go mod tidy`と`go test ./...`で検証する
- [x] 1.2 `usage.SourceOpenCode`、source validation、Agent表示名、およびsource-neutralな識別値を追加し、既存の`internal/usage`テストとOpenCode source identityテストが通ることを検証する
- [x] 1.3 cache snapshotとsource metadataが`opencode`を保持できることを追加し、Codex/ctxの既存snapshotのJSON値が変化しないことをcacheテストで検証する

## 2. OpenCode data rootとSQLite reader

- [x] 2.1 `internal/opencode` packageの公開loader・options・result型を定義し、`--opencode-home`、`OPENCODE_HOME`、`xdg-basedir`既定pathの優先順をresolver unit testで検証する
- [x] 2.2 `<data-root>/opencode.db`またはchannel suffix databaseへのpure Go SQLite read-only接続、`query_only`設定、および必須table検証を実装し、書き込み不能なsynthetic databaseとschema errorの挙動をテストする
- [x] 2.3 `session`、`message`、`part` rowを決定的な順序で逐次取得するreaderを実装し、全tableを一括memory展開しないこととdatabase close errorをreaderテストで検証する
- [x] 2.4 履歴databaseがないrootを空入力、存在しない・非directory・読取不能rootをsource errorとして扱い、対象pathを含むエラーを返すことをテストする
- [x] 2.5 診断用`log`を探索せず、未知fieldを無視し、破損JSON payloadをwarningとしてskipして有効rowの処理を継続することをsynthetic fixtureで検証する

## 3. OpenCode履歴の正規化

- [x] 3.1 `session` metadataからOpenCode sessionを生成し、source `opencode`、Agent `opencode`、directory、version、timestamp、および安定したsession identityを設定することをテストする
- [x] 3.2 user messageをturn境界としてassistant messageとpartを対応付け、実際のuser入力だけをpromptに数え、context・assistant・Tool outputをpromptから除外することをfixtureで検証する
- [x] 3.3 OpenCode `type=tool` partのterminal stateをruntime Toolへ変換し、command系を`shell`へcanonicalize、既知Toolとfallback名を安定化、call IDとstatusを保持することをテストする
- [x] 3.4 同一Tool callの重複と未完了Tool stateを二重計上せず、failed・error・cancelledをfailureとして集計することをnormalizer testで検証する
- [x] 3.5 assistant messageの`tokens`と`cache` metadataを`usage.TokenUsage`へ写像し、複数messageの加算、raw totalの優先、total fallback、および期間filter用timestampをテストする
- [x] 3.6 user text、structured Skill Tool、Skill注入block、およびSkill pathを既存`internal/usage` helperへ渡し、explicit/implicit evidenceとdeduplicationが既存規則どおりになることをテストする
- [x] 3.7 `--days`、`--from`、`--to`をprompt、Tool、Skill、token usageへ適用し、cutoff境界を含めること、timestampなし観測を期間filterへ含めないことをテストする
- [x] 3.8 session・message・partの列挙順を変えても同じturn、count、Agent、row順序、timestamp、JSON値になることを決定性テストで検証する

## 4. OpenCode cache

- [x] 4.1 OpenCode data rootをcache scopeとして正規化し、選択したdatabaseと存在するWAL/SHMのsize・mtimeからrevisionを作り、parser versionをnamespaceへ反映することをテストする
- [x] 4.2 完全に読み取れたOpenCode結果だけを既存`cache.Snapshot`へ保存し、raw prompt、Tool arguments、provider payloadがcache JSONへ含まれないことをテストする
- [x] 4.3 cache hit、database変更、WAL変更、別data root、破損cache、および読み取り中のrevision変更を実装し、fresh readとのreport一致とcache非保存をテストする
- [x] 4.4 cache利用時にも期間filterを正規化済みtimestampへ適用し、freshな全履歴読取後のfilter結果と一致することをintegration testで検証する

## 5. CLIとreport接続

- [x] 5.1 `stats`、`tools`、`skills`の`--source` validationとhelp textへ`opencode`を追加し、`--opencode-home`をOpenCode sourceだけで受け付けることをCLI testで検証する
- [x] 5.2 CLIのOpenCode loader分岐、progress表示、source error、empty input、Agent一覧、および解決済みdata rootのreport contextを実装し、JSONとhuman-readableの両方をCLI integration testで検証する
- [x] 5.3 OpenCodeのTool、Skill、token、期間filter、warningをaggregateへ接続し、`stats`、`tools`、`skills --unused`の集計値が期待値になることをCLI testで検証する
- [x] 5.4 OpenCodeのwarningをstderrへ分離し、`--json` stdoutが単独JSON、`--strict-input`だけがwarning時に非0になることをCLI testで検証する
- [x] 5.5 source固有option、`--days`、`--from`、`--to`、`--layer`、`--strict`、`--group-by`の既存validationとOpenCode sourceの組み合わせを網羅し、Codex/ctxの既存CLI testが通ることを検証する

## 6. Documentation and final verification

- [x] 6.1 `README.md`と`README.ja.md`へOpenCode source、既定path、`--opencode-home`、対応する履歴項目、read-only制約を同じsection coverageで追記する
- [x] 6.2 OpenCode version差異、未対応schema、診断log非対応、cacheの制約をユーザーが誤解しない短い説明として両READMEへ反映し、内容同等性をレビューする
- [x] 6.3 synthetic fixtureのみでOpenCode adapter、cache、CLI、reportを検証し、実ユーザー履歴や秘密情報がGit管理対象へ入っていないことを`git status`とfixture reviewで確認する
- [x] 6.4 `mise run check`を実行し、format、lint、vet、race test、build、npm wrapper test、npm package contentsの全チェックが成功することを確認する
