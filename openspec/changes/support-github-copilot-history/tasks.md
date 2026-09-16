## 1. Source identity and data-root resolution

- [x] 1.1 `usage.SourceCopilot`を追加し、`Valid`、`AllSourceKinds`、source order、display name、およびhelp上のsource一覧を拡張する。既存の`DefaultSourceKinds`と既存sourceの順序・挙動は変更せず、`go test ./internal/usage`でsource identityと表示名のテストが通ることを確認する。
- [x] 1.2 `internal/githubcopilot`にuser home配下の`.copilot`を解決するresolverと、`session-state/*/events.jsonl`を決定的に発見するdiscoveryを実装する。root不在・非directory・読み取り不能、空root、diagnostic fileだけ、補助file除外、および複数sessionのテストが通ることを確認する。

## 2. Copilot event reader

- [x] 2.1 `events.jsonl`をboundedなline streamingで読むenvelope decoderを実装し、`id`、`parentId`、`timestamp`、`type`、`data`、session identity、source path、line番号を保持する。malformed line、unknown type、欠損timestampのwarningと後続event継続をreader testで確認する。
- [x] 2.2 実データを含まないsynthetic JSONL fixtureを追加し、正常event、malformed/unknown event、diagnostic-only root、複数session、巨大line境界を検証する。reader実行前後でfixtureのsize・permission・modification timeが変わらないread-only testが通ることを確認する。

## 3. Event normalization

- [x] 3.1 Copilot eventからsource `copilot`、Agent `copilot`、source-qualified session、turn境界、Model observation、および実際の`user.message`だけのUser Promptを生成するnormalizerを実装する。異なるsessionの同一event/turn IDが統合されず、system・assistant・tool outputがpromptに加算されないnormalizer testが通ることを確認する。
- [x] 3.2 `tool.execution_start`と`tool.execution_complete`を`toolCallId`単位のruntime Toolへ統合し、status、failure、model/runtime/effective layer、および複数runtime actionのdeduplicationを既存`usage`規則へ接続する。Tool countとfailure countを確認するnormalizer・usage testが通ることを確認する。
- [x] 3.3 Model responseのtoken usageを`callId`単位で一度だけ正規化し、Model切替をturn単位で保持し、構造化Skill toolまたはSkill file accessだけを既存Skill detectorへ渡す。raw prompt、tool argument、provider payloadが`usage.Turn`へ残らないprivacy testとtoken/skill testが通ることを確認する。

## 4. Load and cache integration

- [x] 4.1 Copilot normalizerの結果を`usage.Turn`・`usage.Session`・warnings・Agentsへ組み立て、既存のperiod filterとempty-source/error規則を適用する。全期間、`--days`、日付range、空入力、source errorのload testが通ることを確認する。
- [x] 4.2 Copilot rootとevent file群から決定的なsource revisionを作り、既存`cache.Store`の`copilot` namespaceへsanitized snapshotを保存・再利用する。cache hit/miss、event file追加・変更・削除、parser version変更、読み取り中のrevision変更、raw field非保存をcache testで確認する。

## 5. CLI and TUI connection

- [x] 5.1 `--source copilot`をCLIのsource optionへ追加し、adapterは標準の`~/.copilot`を読む。source未指定時の既存default維持を`cmd/catsift`のflag testで確認する。
- [x] 5.2 `loadHistory`、source load label、source path、warning、複数source選択、およびTUI reloadへ`copilot`を接続する。`stats`、`tools`、`skills`、`models`、`sessions`、およびinteractive TUIでCopilot sourceが読み込まれ、既存sourceの回帰testが通ることを確認する。
- [x] 5.3 Copilot sourceのhuman-readable report、JSON、Agent display、Period、empty-state、Tool/Skill/Model/Session detailを既存rendererへ反映する。prompt本文・tool argument・warningがstdout JSONへ混入せず、`go test ./cmd/catsift ./internal/output ./internal/query ./internal/tui`が通ることを確認する。

## 6. Documentation and user-facing contract

- [x] 6.1 `README.md`と`README.ja.md`をcontent-equivalentに更新し、対応source、`copilot`の明示選択、標準root、read-only/privacy制約、diagnostic log非対応を記載する。headings、option一覧、example、code fenceの比較と`git diff --check`が通ることを確認する。

## 7. Verification checkpoints

- [x] 7.1 Tasks 1〜4完了時点で`go test ./internal/githubcopilot ./internal/usage ./internal/cache`と`go test -race ./internal/githubcopilot ./internal/usage ./internal/cache`を実行し、reader・normalizer・cacheの境界とprivacy条件が通ることを確認する。
- [x] 7.2 Tasks 5〜6完了時点で`mise run check`、`go test -race ./...`、および`go build -o ./bin/catsift ./cmd/catsift`を実行し、既存sourceの回帰がなく、Copilot sourceのhelp・JSON・reportが生成できることを確認する。

## 8. Naming and default-root refinement

- [x] 8.1 source ID、Agent ID、Provider ID、cache namespace、およびCLI help/reportを`copilot`へ統一し、`--github-copilot-home`を削除する。GitHub Copilotの表示名と`~/.copilot`の標準rootは維持する。
- [x] 8.2 `copilot` sourceのCLI・adapter・TUI・README・OpenSpec記述を検証し、既存sourceの回帰がないことを`mise run check`とOpenSpec validationで確認する。

## 9. Session name metadata

- [x] 9.1 各session directoryの`workspace.yaml`からtop-level `name`をread-onlyで取得し、`Session.Title`へ反映する。metadata欠損・空name・不正入力ではevent集計を継続し、raw本文や`session-store.db`は読まない。
- [x] 9.2 `workspace.yaml`の追加・変更をsource revisionへ含め、cache hit/missとSession titleのfresh/cache一致をsynthetic testで検証する。対象テスト、race test、`mise run check`、OpenSpec validationを実行する。

## 10. Skill evidence refinement

- [x] 10.1 `skill.invoked`を既知Copilot eventとして読み取り、`data.name`または安全なpath fallbackだけをconfirmed Skill evidenceへ正規化する。path、content、およびraw provider payloadは保存しない。
- [x] 10.2 実際のCopilot event shapeを模したsynthetic fixtureでSkill取得、unknown warning非発生、turn紐付け、cache/privacyを検証し、対象test、race test、`mise run check`、OpenSpec validationを実行する。

## 11. Project name metadata

- [x] 11.1 `session.context_changed`とboundedな`workspace.yaml` metadataを読み取り、`repository`を`Session.ProjectName`へ、`gitRoot`または`cwd`の末尾をfallbackへ正規化し、`ProjectPath`を後方互換で保持する。
- [x] 11.2 `ProjectName`をcache、query、human-readable report、JSON、およびTUIへ接続し、repository/fallback、fresh/cache一致、privacy、および既存project filterをsynthetic testで検証する。対象test、race test、`mise run check`、OpenSpec validationを実行する。

## 12. Persisted token usage refinement

- [x] 12.1 `session.shutdown.data.modelMetrics[*].usage`を読み取り、`cacheReadTokens`、`cacheWriteTokens`、`reasoningTokens`を共通TokenUsageへ写像する。`assistant.usage`とsnake/camel case alias、total補完、個別usageとの重複回避、および終端turnへの関連付けをnormalizer testで確認する。
- [x] 12.2 shutdown aggregateを含むsynthetic fixtureでfresh/cold/warm cache一致、JSONのsanitized token field、旧parser versionのcache invalidationを検証し、対象test、race test、`mise run check`、OpenSpec validationを実行する。
