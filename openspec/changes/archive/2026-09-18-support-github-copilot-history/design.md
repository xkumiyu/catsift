## Context

既存のsource adapterは、それぞれの履歴形式を`usage.Turn`、`usage.Session`、`usage.Warning`へ変換し、CLIがsourceを選択して共通のquery・report・TUI・cacheへ渡す構成になっている。詳細は[proposal.md](proposal.md)とchange内のspecを参照する。

GitHub Copilot CLIのsessionはuser home配下へ永続化されることが公式documentationで説明されており、現在確認できるlocal dataでは`~/.copilot/session-state/<session-id>/events.jsonl`が、`id`、`parentId`、`timestamp`、`type`、`data`を持つevent streamになっている。`logs`や`session-store.db`は利用統計の入力には含めない。

## Goals / Non-Goals

**Goals:**

- `copilot` sourceを既存のsource-neutral pipelineへ追加する。
- session、turn、user prompt、Tool、Skill、Model、token usageを取得可能な範囲で既存の共通modelへ変換する。
- malformedまたは将来のunknown eventに耐え、read-only・local-only・privacy-safeな処理を維持する。
- 既存のreport、TUI、cache、warning、source選択の規則を再利用する。

**Non-Goals:**

- Copilotのdiagnostic log、hook output、`session-store.db`を利用統計へ変換すること。
- Copilot CLIを起動してexportすること、履歴を修復・書換え・削除すること。
- raw prompt、tool argument、provider payloadをreport、diagnostic、cacheへ保存すること。
- 未確認の将来event typeを推測で利用統計へ加えること。
- 新しい外部dependencyを追加すること。

## Decisions

### `internal/githubcopilot`を独立adapterとして追加する

`internal/codex`、`internal/ctx`、`internal/opencode`と同じ境界で`internal/githubcopilot`を追加する。packageはresolver、event file discovery、streaming reader、normalizer、source revision、cache付き`Load`を担当し、集計と表示は既存の`internal/usage`、`internal/query`、`internal/output`、`internal/tui`へ委譲する。

独立packageを選ぶのは、Copilotのevent schemaを既存adapterへ混ぜるとsourceごとのwarning、parser version、revision管理が曖昧になるためである。CLIの起動やCopilotのexport commandへ依存する案は、外部binaryのversion・PATH・出力仕様に依存し、read-onlyと部分的なwarning処理をadapter側で制御しにくいため採用しない。

### data rootは`~/.copilot`から解決する

CLIから任意のrootを指定するoptionは追加せず、OS user homeから`.copilot`を組み立てる。resolverはuser homeを注入できる`ResolveHomeFrom`形式にし、hostの実環境へ依存しないtestを作る。rootが存在しない・directoryでない・読み取り不能な場合はsource errorとし、rootは存在するがsession eventがない場合は空入力とする。adapterの直接testではsynthetic rootを渡せるようにする。

`copilot`は既存の既定sourceへ追加せず、明示したときだけ読む。これにより、Copilotが未インストールの環境で既存commandが新たにerrorにならず、既存のdefault sourceと利用者向け挙動を保てる。`usage.SourceKind`、`Valid`、`AllSourceKinds`、source order、help、source load label、およびCLI loaderのcaseだけを拡張し、既存の集計意味論は変更しない。

### `session-state/*/events.jsonl`だけを決定的に発見する

解決したroot直下の`session-state`を確認し、session directoryごとの通常file `events.jsonl`だけを候補にする。session directory名をprovider session IDとして使用し、候補fileは相対pathの昇順で処理する。`logs`、database、lock、checkpoint、researchなどの補助fileは探索対象にしない。

各lineは`bufio.Reader`相当のbounded readerで逐次decodeする。envelopeは`id`、`parentId`、`timestamp`、`type`、`data`を保持し、source path、line、session ID、event IDを`usage.SourceRef`へ付与する。unknown typeやmalformed lineはprivacy-safeなwarningへ集約し、後続lineと別sessionの処理を継続する。raw `data`はnormalizerが処理する間だけ保持し、下流のsnapshotへ渡さない。

履歴eventの入力は`events.jsonl`に限定するが、各session directoryにある`workspace.yaml`はsession metadataとして個別に読み取る。top-level `name`、`repository`、`git_root`、`cwd`だけをboundedなread-only parserで取得し、Session title、project name、project pathへ渡す。`session-store.db`、logs、checkpoint、plan、およびその他のworkspace artifactは統計入力として解釈しない。

### provider eventを意味単位へ写像し、既存normalizerを再利用する

Copilot eventの処理はevent typeと安定したIDを軸に、次の境界で行う。

| Copilot event | 共通eventへの扱い |
| --- | --- |
| `session.start`、`session.shutdown` | Session metadata、CLI version、作成・更新時刻の補助情報。`session.shutdown.data.modelMetrics[*].usage`はtoken aggregateとして扱い、利用countは増やさない |
| `session.context_changed` | `repository`をproject nameへ、`cwd`をproject pathへ写像する。repositoryがない場合は`gitRoot`または`cwd`の末尾をproject nameへfallbackする。workspace metadataにも同じfallbackを適用する |
| `user.message` | `role=user`かつ実入力である場合だけUser Prompt。本文は保存しない |
| `assistant.turn_start`、`assistant.turn_end`、`model.turn_started`、`model.turn_ended` | `turnId`を優先したturn境界。欠損時はsession内の決定的ordinalへfallback |
| `assistant.message`、`model.model_call_*`、`session.model_change` | Model observation。Model変更は後続turnのidentityへ適用し、過去のModelを上書きしない |
| `tool.execution_start`、`tool.execution_complete` | `toolCallId`単位のruntime Tool。name、status、turn、timestampを保持 |
| `skill.invoked` | `data.name`をconfirmed Skill evidenceへ変換する。`path`と`content`は読み取り中だけ参照し、結果へ保存しない |
| model responseのusage event | call ID単位でterminal usageを1回だけTokenUsageEventへ変換 |
| `permission.*`、`session.info`、未対応metadata | 利用countを増やさず、必要な場合だけ型付きwarningまたはdiagnosticへ要約 |

`turnId`、`toolCallId`、`callId`が存在する場合はそれをdeduplication keyにする。Toolは開始と完了を1 callへまとめ、runtime観測がある場合は既存の`usage.EffectiveTools`でmodel wrapperを抑制する。Token usageは同一callの複数通知を優先順位付きで1件へまとめ、Model identityとtimestampを失わない。`skill.invoked`の明示event、具体的なSkill tool、またはSkill file accessだけを既存の`usage` skill detectorへ渡し、通常文の文字列から推測しない。

Copilotのpersisted token usageは`inputTokens`、`outputTokens`、`cacheReadTokens`、`cacheWriteTokens`、および`reasoningTokens`を使用し、それぞれ共通のinput、output、cached input、cache-write input、reasoning outputへ写像する。`totalTokens`が保存されていない場合はinputとoutputの合計をtotalとして補完する。`session.shutdown`のmodel aggregateはper-call eventと重複して計上せず、per-call eventがない場合だけ終端turnへ関連付ける。turnが存在しない場合はaggregate-only turnへ保持する。raw `modelMetrics` payloadは保存しない。

本文やtool argumentが必要な判定はadapter内の一時memoryで行う。`usage.Turn`にはcount、timestamp、canonical name、evidence、Model、token usageだけを残し、既存のcache snapshot契約をそのまま利用する。

### cacheはroot単位のsnapshotとevent file revisionで管理する

Copilot sourceのcache scopeは解決後rootのclean absolute path、source namespaceは`copilot`とする。revisionは`session-state`配下の候補`events.jsonl`と対応する`workspace.yaml` metadata fileを相対path順に並べ、path、size、modification timeを連結して作る。parse前後で候補一覧またはrevisionが変わった場合はsnapshotをcacheへ公開しない。parser schemaまたはevent mappingを変更した場合はCopilot専用parser versionを更新する。

既存の`cache.Store`とsource-neutral `cache.Snapshot`を再利用し、cache hit時は全期間snapshotへ既存のperiod filterを適用する。cacheへ保存するのはsession（project nameとproject pathを含む）、turn、Tool、Skill、Model、token usage、およびwarning summaryだけで、raw本文、arguments、provider payload、source pathは保存しない。

### CLI・TUI接続はsource分岐だけを拡張する

`loadHistory`の`copilot` caseからadapterを呼ぶ。source path、Agents、Warnings、cache diagnosticsは既存の`loadedHistory`とdiagnostic writerへ流す。`AgentDisplayName`では`copilot`を`GitHub Copilot`として表示し、canonical IDをそのままquery、report、TUIへ渡す。

Session report、Session detail、およびJSONでは取得したproject nameを表示し、project pathもmachine-readable metadataとして保持する。既存のproject filterはproject pathを対象とする。

`--source copilot`は既存のsource selection validationと同じ経路で処理し、`--days`、日付range、`--json`、`--verbose`、TUI reloadへ同じfilterとsnapshotを適用する。既存のdefault sourceは変更しない。

### synthetic fixtureを中心に検証する

実ユーザーのCopilot履歴をrepositoryへ持ち込まず、次の最小fixtureを用意する。

- session start、user message、model response、tool start/complete、token usageを含む正常系
- malformed line、unknown event、欠損timestamp、欠損turn IDを含むrecoverable warning系
- 同じcall IDの重複通知、Tool failure、Model切替、Skill evidenceを含むdeduplication系
- 空root、diagnostic fileだけ、複数session、file変更中のcache writeを含む境界系

parser package testでsource位置・正規化結果・privacyを確認し、CLI testでsource選択、report、JSON、period filter、cache hit/missを確認する。最終的な全体検証はrepository標準の`mise run check`を使う。

## Risks / Trade-offs

- [Copilotのevent schemaが将来変更される] → envelopeは未知fieldを無視し、未知typeはwarningとして処理する。意味のあるmapping変更ではparser versionを更新し、既知schemaだけをfixtureで固定する。
- [同じmodel callの通知が複数回token usageを報告する] → `callId`とterminal eventの優先順位でdeduplicateし、token eventを一度だけ採用する。
- [統計実行中にsession eventが追記される] → 読み取り前後の候補一覧・revisionを比較し、不一致時はcache writeを省略する。fresh report自体は読み取り時点のsnapshotとして返す。
- [Copilotが未インストールの環境でsource errorになる] → `copilot`をdefault sourceへ加えず、明示選択時だけ`~/.copilot`を解決する。
- [promptやTool payloadがdiagnosticへ漏れる] → warningとdiagnosticにはreason、type、path、line、countだけを許可し、raw `data`は出力しないtestを置く。

## Migration Plan

1. `internal/githubcopilot`、共通source識別、CLI loader、cache revisionを追加する。
2. synthetic fixtureでparser、normalizer、filter、cache、CLI/TUIのsource接続を検証する。
3. help、README、README.ja.mdのsource説明を更新し、`mise run check`を実行する。
4. 問題がある場合は`--source copilot`の利用を停止し、既存sourceを継続利用する。rollbackはCopilot adapterとsource分岐を戻すだけで、provider-owned dataのmigrationや削除は行わない。
