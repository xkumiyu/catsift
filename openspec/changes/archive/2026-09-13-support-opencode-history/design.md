## Context

既存の入力adapterは、CodexのJSONLを`internal/codex`で読み取り、ctxの公開event streamを`internal/ctx`で読み取っている。どちらも`usage.Turn`へ正規化され、CLIはsourceごとのloaderを選択し、正規化済みsnapshotを`internal/cache`へ保存する。

現行のOpenCodeは、診断用`log`とは別に、data root配下の`opencode.db`またはchannel suffix databaseへ`session`、`message`、`part`などの構造化履歴をSQLiteとして保存する。OpenCode CLIを経由してexportする方式では全sessionの列挙と部分的な破損からの回復をadapter側で制御しにくいため、構造化履歴を直接read-onlyで読む。

## Goals / Non-Goals

**Goals:**

- OpenCodeの現行SQLite履歴からsession、turn、user prompt、runtime Tool、Skill evidence、およびmessage単位のtoken usageを正規化する。
- `codex`、`ctx`と同じfilter、report、JSON、warning、cacheの契約へ接続する。
- CLIがインストールされていない場合でも、OpenCode data rootだけを指定して読めるようにする。
- cross-platform配布を維持し、OpenCodeの履歴を変更・送信しない。

**Non-Goals:**

- OpenCodeの診断用`log`を利用統計へ変換すること。
- OpenCodeの履歴を修復、migration、削除、export、またはOpenCode API経由で同期すること。
- 旧版や将来版の未確認storage schemaを完全互換にすること。既知schemaと安全に解釈できるfieldを対象にし、互換性はparser versionで管理する。
- Codex、ctx、OpenCodeを1回のCLI実行で混在させること。

## Decisions

### SQLiteをadapterから直接read-onlyで読む

`database/sql`とpure Goの`modernc.org/sqlite`を使用し、`<opencode-data-root>/opencode.db`またはchannel suffix databaseへread-only接続する。接続後に`PRAGMA query_only = ON`を設定し、履歴queryはread transaction内で実行する。CGO依存のSQLite driverや、実行環境にOpenCode CLI・`sqlite3` binaryが存在することには依存しない。

OpenCode CLIの`export`や`db` commandを呼び出す案は、外部実行ファイルのversion・PATH・出力仕様に依存し、全sessionの取得とrow単位のrecoverable warningをadapterが制御できないため採用しない。診断logのline parserも、履歴の意味情報とtoken metadataを安定して取得できないため採用しない。

### data rootの解決はOpenCode専用resolverで行う

`--opencode-home`、`OPENCODE_HOME`、OpenCodeが依存する`xdg-basedir`の既定data directoryの順で解決する。`XDG_DATA_HOME`が空なら`$HOME/.local/share/opencode`を既定値とする。resolverはuser homeとenvironmentを注入可能にして、実環境のprivate dataへ依存しないunit testを作る。

`--opencode-home`はdata rootを表し、直接database fileを指定するoptionにはしない。明示されたrootが存在しない場合はsource error、rootは存在するがOpenCode databaseがまだない場合は空入力とする。

### 履歴queryと正規化を分離する

adapterは次の順で処理する。

1. `session` rowをsession identity、directory、version、created/updated timestampとして読み取り、sourceを`opencode`、Agentを`opencode`として作る。
2. `message` rowをsessionごとにcreated timeとmessage IDで決定的に列挙し、message JSONをdecodeする。user messageをturn開始点、assistant messageを現在turnのresponseとして扱う。user messageを持たないassistant/tool activityには安定したordinal fallback turnを割り当てる。
3. `part` rowをmessage IDとpart IDで決定的に列挙する。`type=text`のuser partだけをprompt・Skill検出用の一時textとして使い、正規化結果やcacheへ本文を保持しない。
4. `type=tool`のterminal stateをruntime Toolへ変換し、Tool metadataを既存の`usage` normalizerへ渡す。OpenCodeのTool partはすでに実行単位なので、同じcallのmodel wrapperを追加しない。
5. assistant messageの`tokens` metadataをmessage timestampでturnへ加算する。message metadataが欠落した場合だけ`step-finish` partのtokensをfallbackとして加算し、OpenCodeが両方へ保存する同一usageを二重計上しない。session tableの集計token列はmessage tokenとの二重計上を避けるためfallbackにも使わない。

rowのdecodeに失敗した場合はwarningを蓄積して次のrowへ進む。必須tableの欠落やSQLiteをread-onlyで開けない場合はsource errorとし、部分結果を成功結果として返さない。

### OpenCode Toolのcanonical nameを限定的に安定化する

command実行を表す`bash`、`shell`、`exec`などは`shell`へ正規化する。`read`、`edit`、`write`、`glob`、`grep`、`webfetch`、`websearch`、`task`など既知のOpenCode Toolは小文字の安定名へ変換し、それ以外は安全な小文字のraw Tool名をfallbackとして使用する。Tool statusは`completed`をsuccess、`error`・`failed`・`cancelled`をfailure、それ以外をunknownへ対応させる。

Skill検出とcommand path解析は既存の`internal/usage`へ委譲し、OpenCode専用の推測ロジックを増やさない。履歴本文をshellとして実行したり、filesystemを探索したりはしない。

### token usageはmessage metadataを正規化する

OpenCodeのmessage `tokens`から`input`、`output`、`reasoning`、`cache.read`、`cache.write`を対応する`usage.TokenUsage`へ写像する。raw `total`があればそれを使い、なければ`input + output`をTotalとする。cache read/writeはInputの内訳として保持し、Totalへ別加算しない。複数assistant messageの値はturnごとに`AddTokenUsageAt`で合算し、期間filter後の再集計に使えるtimestampも保持する。

### cache revisionはSQLite本体と補助fileを含める

cache scopeは解決後のOpenCode data rootのabsolute path、source namespaceは`opencode`とする。revisionは選択したdatabase名とsize・modification timeに加え、存在するdatabaseのWAL/SHM sidecarのsize・modification timeを決定的に連結する。読み取り前後でrevisionが変わった場合はそのsnapshotをcacheへ保存しない。parser versionを上げることでschema解釈や正規化規則の変更をcache invalidationへ反映する。

既存のsource-neutral `cache.Snapshot`を再利用し、raw prompt本文とTool argumentsをsnapshotへ書き込まない。OpenCodeのcacheはCodex・ctxのnamespaceと衝突しない。

### CLI接続は既存のsource分岐を拡張する

`usage.SourceOpenCode`を追加して`Valid`と表示名を拡張し、3つの統計commandのhelp・validationへ`opencode`と`--opencode-home`を追加する。CLIのloader分岐はCodex・ctxと同じく、source errorをstderrへ出し、normalized resultをaggregateへ渡す。report contextのsource pathは解決済みdata rootを使い、Agent一覧は`opencode`一件とする。

## Risks / Trade-offs

- [OpenCodeのSQLite schemaが変更される] → 必須table・JSON shapeを明示的に検証し、未知fieldは無視、未知schemaはsource error、正規化規則の変更はparser version更新で対応する。
- [OpenCodeが統計実行中にdatabaseへ書き込む] → read-only transaction、読み取り前後の本体/WAL/SHM revision確認、およびrevision不一致時のcache破棄で古いcomplete cacheの公開を防ぐ。
- [pure Go SQLite dependencyでbinaryが大きくなる] → CGOを避けて既存のcross-platform/npm配布を保ち、依存追加は1つのdriverに限定する。サイズ増加はbuild artifactで確認する。
- [user promptやTool payloadを一時的に扱う] → row処理中だけmemory上でdecodeし、通常report・cache・diagnosticへ本文とargumentsを出力しない。Toolや履歴内容を実行しない。
- [OpenCodeの診断logを履歴と誤認する] →入力をOpenCodeのdatabase filenameに限定し、log directoryは探索しないことをテストで固定する。

## Migration Plan

1. `modernc.org/sqlite`を追加し、OpenCode adapter、共通source識別、CLI、cacheを実装する。
2. synthetic SQLite fixtureで正常系、Tool/status、token、Skill、期間filter、破損row、read-only、cacheを検証する。
3. help、README、README.ja.md、npm wrapperを更新し、`mise run check`で全体を確認する。
4. 配布後に問題がある場合は`--source opencode`の利用を止め、既存のCodex/ctx sourceを使用する。rollbackはOpenCode分岐とdriver依存を含む変更を戻すだけで、OpenCode data rootへのmigrationは存在しない。
