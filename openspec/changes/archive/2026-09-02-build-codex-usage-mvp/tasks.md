## 1. Project基盤とdomain model

- [x] 1.1 Go 1.27のmodule `github.com/xkumiyu/catsift`、`cmd/catsift`、`internal/{codex,usage,aggregate,output}` の最小構成を作成し、human-readable report用に `charm.land/lipgloss/v2@v2.0.6` と `golang.org/x/term` を追加する（Bubble Teaは導入しない）。`go test ./...`、`go build ./cmd/catsift`、`go mod verify` が成功することを確認する
- [x] 1.2 SourceRef、turn、model/runtime Tool観測、SkillEvidence、SkillUse、warning、集計resultのtyped modelを定義し、constructorとzero-valueのunit testで必須fieldとenum値を確認する
- [x] 1.3 user text・path・ID・outputを匿名化したCodex JSONL fixture群を作成し、fixture testで全fileが読取可能か、意図したmalformed lineだけが不正かを確認する

## 2. Codex history ingestion

- [x] 2.1 `--codex-home`、`CODEX_HOME`、既定 `.codex` の解決と `sessions` / `archived_sessions` の決定的なfile探索をtest-firstで実装し、優先順位・片方欠落・空履歴・読取不能pathのtestを通す
- [x] 2.2 `json.RawMessage` を使うline単位envelope decoderとSourceRef付与をtest-firstで実装し、大きなline・未知field・未知type・malformed lineが想定どおり処理されるtestを通す
- [x] 2.3 session metadataと `task_started` / `task_complete` / `turn_aborted` / user message境界を扱うstreaming turn assemblerを実装し、明示turn ID・ordinal fallback・EOF flush・中断turnのtestを通す
- [x] 2.4 timestamp filterとwarning collectorを実装し、cutoff inclusive、無効なdays、一部file error、warningがstderr専用であることのtestを通す

## 3. Tool利用の正規化

- [x] 3.1 `function_call` と `custom_tool_call` をmodel layerへ変換するdecoderをtest-firstで実装し、raw name・call ID・arguments/input・timestamp・sourceが保持されることを確認する
- [x] 3.2 `ItemCompleted` のCommandExecution、McpToolCall、FileChange、WebSearch、ImageView、ImageGeneration、CollabAgentToolCallをruntime layerへ変換し、canonical nameと成功・失敗statusのtable-driven testを通す
- [x] 3.3 effective Tool viewを実装し、runtime優先、code-mode `exec` 抑制、call IDによるdedup、非`exec` fallback、runtimeのない旧turn、失敗Callsのtestを通す

## 4. Skill利用の検出と統合

- [x] 4.1 contextual `<skill>` blockと先頭 `$skill-name` を区別するdetectorをtest-firstで実装し、`explicit-injected` / `explicit-request` と通常prose・assistant message・tool outputの非検出を確認する
- [x] 4.2 native `Skill` / `skill` とnamespaced `skills.read` のstructured argumentsから対象Skillを抽出するdetectorを実装し、対象が一意でないcallをconfirmed扱いしないtestを通す
- [x] 4.3 runtime commandから既知Skillの `SKILL.md` / `scripts` accessだけを検出するpath classifierを実装し、任意pathを開かないこと、削除済みSkill、quoted path、frontmatter名、directory fallbackのtestを通す
- [x] 4.4 `(session, turn, skill)` 単位のSkillEvidence mergeを実装し、state優先順位、explicit/implicit mode、method集合、複数accessのdedup、別turn・別sessionの加算をunit testで確認する

## 5. 集計とoutput

- [x] 5.1 Sessions・User Prompts・effective Tool Calls・Skill Usesのoverview aggregatorを実装し、Skill注入をpromptから除外するtestと空入力testを通す
- [x] 5.2 Tool名別のCalls・Failures・Last Usedと `effective|runtime|model` 選択を実装し、件数降順・名前昇順の決定的sortをunit testで確認する
- [x] 5.3 Skill名別のExplicit・Implicit・Confirmed・Inferred・Unconfirmed・Total・Last Usedとstrict filterを実装し、dedup済み利用を一度だけ数えるunit testを通す
- [x] 5.4 typed resultからhuman-readable static report・JSON schema version 1を生成するrendererを実装し、固定時刻を使うgolden testでtitle/filter/section/footer、桁区切り・右揃え・timezone付きLast Used、field順、RFC 3339、空結果を確認する
- [x] 5.5 Lip Glossと `TerminalCapabilities` を使うhuman rendererを実装し、60/80/120 column、TTY/non-TTY、dark/light profile、`NO_COLOR`、`auto|always|never`、Unicode ellipsis、wide/standard/compact、empty stateのsnapshot testを通す

## 6. CLI統合

- [x] 6.1 標準libraryの `flag.FlagSet` で `stats`、`tools`、`skills` と共通option（`--color auto|always|never`を含む）を実装し、help、未知command、無効値、TTY/redirect/`NO_COLOR`/color mode、Tool layer、Skill strictのargument testを通す
- [x] 6.2 ingestionからrendererまで各commandを接続し、匿名化Codex homeを使うintegration testでhuman reportのplain/styled/compact表示、JSONのANSIなし、各形式の件数一致、stdout/stderr分離、終了codeを確認する
- [x] 6.3 CLI overviewのTool Callsが `tools` のeffective合計、Skill Usesが通常 `skills` のTotal合計と一致するend-to-end testを追加して通す

## 7. Hardeningとhandoff

- [x] 7.1 envelope decoderとSkill detectorへfuzz testを追加し、seed corpus実行でpanicしないことと通常proseをSkill利用にしないことを `go test ./...` で確認する
- [x] 7.2 read-only integration testと実環境smoke testを実行し、Codex homeのfile metadataが変化しないこと、network依存がないこと、warning発生時もhuman report/JSONが有効であることを確認する
- [x] 7.3 READMEへinstall/build、3 command、全option、human reportのlayout・color/`NO_COLOR`・狭いterminal対応、Tool layer、Skill state/strict、privacy、interactive TUIをMVP対象外とすることを記載し、READMEの例がbinaryの `--help` とintegration fixture上の実行結果に一致することを確認する
- [x] 7.4 `gofmt`、`go vet ./...`、`go mod verify`、`go test -race ./...`、`go test ./...`、`go build ./cmd/catsift`、`openspec validate build-codex-usage-mvp --strict` を実行し、すべて成功することを確認する
