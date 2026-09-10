## Context

本changeは実装がまだ存在しないrepositoryへ、最初の実行可能なCLIとdomain modelを導入する。要件の動機は `proposal.md`、外部動作は3つのdelta specを参照する。

Codexの通常rollout JSONLにはmodelが選択した `FunctionCall` / `CustomToolCall` と、完了した `TurnItem` が保存される。一方、Codex内部の `SkillInvocation` はanalytics subsystem用であり、rolloutに専用 `skill_call` として永続化されない。そのため、Toolはmodel layerとruntime layerを分離し、Skillは注入context・structured Tool・file accessなどの証拠から導出する必要がある。

履歴は複数versionのCodex CLIから生成され、file数・line数が継続的に増える。parserは既知fieldを厳密に利用しながら未知fieldには寛容で、履歴全体をmemoryへ展開しないことが制約となる。

既定stdoutはdebug dumpやsystem logではなく、対象・期間・主要値・内訳の関係が分かるstatic reportとする。interactiveなevent loopは不要だが、TTY、color profile、terminal幅、redirect、`NO_COLOR` に応じて安全に描画を切り替える必要がある。

## Goals / Non-Goals

**Goals:**

- Codex固有のdecodeと、Agent非依存の利用Event・集計を分離する。
- 生の観測、導出されたeffective Tool利用、統合済みSkill利用の関係を追跡可能にする。
- schema driftや部分的な破損があっても、有効な履歴から決定的な結果を生成する。
- fixtureとgolden testにより、検出規則とCLI contractを独立して検証できる構造にする。
- terminal capabilityに応じて情報hierarchyと可読性を保つ、非interactiveなhuman-readable reportを提供する。
- single binaryとして配布しやすく、実行時依存を持たない構成にする。

**Non-Goals:**

- Codexのanalytics endpointまたはtelemetryを利用すること。
- code-mode内部の完全なcausal traceを再構築すること。
- 任意のshell/JavaScriptを実行して履歴内容を評価すること。
- 初期実装で汎用plugin system、並列scan、永続cacheを導入すること。
- interactive TUI、keyboard navigation、screen遷移、prompt、animationを導入すること。

## Decisions

### 1. Go 1.27、標準library、限定した描画libraryで実装する

moduleは `github.com/xkumiyu/catsift`、実行入口は `cmd/catsift` とする。subcommandは `flag.FlagSet`、JSONは `encoding/json` で実装する。human-readable reportにはstableな `charm.land/lipgloss/v2`、TTY判定とterminal幅取得には `golang.org/x/term` を使用する。選定時点のLip Gloss stable releaseはv2.0.6であり、実装時は互換性を確認したversionを `go.mod` / `go.sum` で固定する。

3 commandと少数optionの解析には標準libraryで十分であり、Cobraやinteractive frameworkは追加しない。一方、ANSI、Unicode幅、color profile、table styleを独自実装すると描画bugと保守costが増えるため、その責務だけをLip Glossへ委ねる。Bubble TeaはModel-Update-Viewのevent loopを必要とするinteractive TUI向けであり、本MVPには導入しない。`text/tabwriter`だけの実装も検討したが、TTY-awareなstyleと幅別layoutを安全に扱うには不足する。

### 2. 一方向pipelineと小さなpackage境界を採用する

```text
filesystem discovery
        ↓
Codex envelope decoder
        ↓
turn assembler / raw observations
        ↓
Tool + Skill normalizers
        ↓
aggregators
        ↓
table / JSON renderers
```

想定する境界は次のとおりとする。

```text
cmd/catsift       argument parsingと終了code
internal/codex      source探索、JSONL decode、turn組立
internal/usage      共通Event、Tool/Skill検出と重複排除
internal/aggregate  stats/tools/skills view
internal/output     human report / JSON renderer
```

初期実装ではAgent interfaceやadapter registryを先に抽象化しない。Codex parserが共通Eventを返す境界だけを守り、2つ目のAgentを追加するときに実際の差異からinterfaceを抽出する。

### 3. JSONL envelopeを段階的にdecodeする

最初に `timestamp`、top-level `type`、`payload` を `json.RawMessage` としてdecodeし、既知typeだけを専用の小さなstructへdecodeする。upstream record全体を1つの巨大structで再現しないことで、field追加を互換な変更として扱う。

fileはpathでsortしてから1つずつ開き、buffered readerでline単位に処理する。単一lineだけはmemory上に保持するが、session全体は保持しない。極端に大きなlineには十分余裕のある上限を設け、超過時は破損recordと同じwarning経路へ送る。

各decode結果には `SourceRef {path, line, cli_version}` を付ける。user contentやtool output本文は集計後に保持せず、必要なname・path・status・IDだけを抽出する。

### 4. turn assemblerをstreaming集計の単位にする

`task_started` / `task_complete` / `turn_aborted` とuser message境界を使って、session file内のrecordをlogical turnへ割り当てる。明示的なturn IDがある場合はそれを優先し、ないversionではsession内の単調増加ordinalを使う。

model Tool、runtime action、Skill evidenceをcurrent turnだけbufferし、turn終了時に正規化・重複排除してaggregatorへ渡す。これにより、effective Tool選択とSkillのturn単位dedupを行いながら、memory使用量を最大turn sizeへ抑える。中断された最終turnはEOFでflushする。

### 5. Toolはmodel・runtime・effectiveの3 viewを持つ

model layerは `function_call` / `custom_tool_call` のraw nameとcall IDを保持する。runtime layerは完了した `TurnItem` を次の安定したcanonical nameへ変換する。

| TurnItem | Canonical name |
|---|---|
| `CommandExecution` | `shell` |
| `McpToolCall` | `mcp:<server>/<tool>` |
| `FileChange` | `file_change` |
| `WebSearch` | `web_search` |
| `ImageView` | `image_view` |
| `ImageGeneration` | `image_generation` |
| `CollabAgentToolCall` | `collaboration:<tool>` |

effective viewはruntime観測を優先し、同じturnのcode-mode wrapper `exec` を除外する。call IDでruntime actionと対応付けられるmodel callも除外する。対応付け不能な非`exec` model callは、runtime側に同種の観測がなければfallbackとして残す。runtime record自体がない旧turnではmodel callをそのまま採用する。

`tools --layer model|runtime` は導出前の各layerを直接集計し、既定の `effective` だけが上記の抑制規則を使う。成功・失敗にかかわらず完了した呼び出しをCallsへ加算し、失敗statusはFailuresにも加算する。

### 6. Skillはevidenceを先に作り、turn終了時にSkillUseへ統合する

detectorは確度の高い順ではなく、独立したevidence producerとして次を生成する。

- `explicit-injected`: contextual input先頭のstructured `<skill>` blockからname/pathを抽出する。
- `structured-tool`: raw Tool名が既知の `Skill` / `skill` / `skills.read` 形式で、引数から対象Skillを一意に取得できる場合に生成する。
- `explicit-request`: 実際のuser prompt先頭にあるcanonical `$name` tokenから生成する。
- `implicit-access`: runtime commandが既知Skillの `SKILL.md` または `scripts` 配下へaccessした場合に生成する。

通常prose、assistant出力、tool output本文を正規表現で横断検索しない。shellやcode-mode JavaScriptも実行せず、runtime actionのcommand fieldと旧形式のstructured argumentsだけを静的に調べる。

同一 `(session, turn, canonical skill name)` のevidenceを1つの `SkillUse` へmergeする。stateは `confirmed > inferred > unconfirmed`、modeはexplicit evidenceがあれば `explicit`、それ以外を `implicit` とし、method集合と最も早いtimestampを保持する。`--strict` はmerge後のstateが `confirmed` のSkillUseだけを選択する。

### 7. Skill名解決は履歴優先で、filesystem accessを制限する

name解決の優先順位はstructured evidence、履歴内metadata、frontmatter、directory名とする。履歴内の注入fragmentからname/path対応をsession catalogへ登録し、後続のfile access判定に使う。

現在の `SKILL.md` を読むのは、履歴または既知のSkill rootからSkill pathだと確認できた場合だけに限定する。任意のcommand文字列に含まれるpathを無条件に開かない。fileが削除済み、移動済み、または読取不能なら、履歴pathの `skills/<name>/...` componentからdirectory名をfallbackとして使う。

### 8. 集計結果とrendererを分離する

aggregatorはrendererに依存しないtyped resultを返す。rowはCallsまたはTotalの降順、同数時はcanonical nameの昇順でsortしてからrendererへ渡す。

JSONはcommandごとのobjectに `schema_version: 1`、基準時刻、適用filter、集計dataを含める。JSON rendererはLip Glossを通さず、ANSIを生成できない経路に分離する。warning collectorはskip理由ごとの件数だけを保持し、render完了後にstderrへ要約するため、machine-readable stdoutへ混入しない。

human rendererには集計resultとは別に、次の値を持つ `ReportContext` と `TerminalCapabilities` を渡す。

```text
ReportContext
  agent / period / tool layer / strict / reference time / timezone

TerminalCapabilities
  is_tty / width / color profile / color mode / no_color
```

実環境では `x/term.IsTerminal` と `x/term.GetSize` からcapabilityを構築し、testでは固定値を注入する。`--color auto` はTTYかつ `NO_COLOR` 未設定の場合だけstyleを有効化し、`always` / `never` は強制modeとして優先する。ただしJSONは常にstyle無効とする。

human reportは次の情報hierarchyを共通化する。

```text
title:    CatSift · CODEX
context:  period / effective|runtime|model / strict
body:     summary metrics または見出し付きtable
footer:   row数 / total calls or uses
```

countは人間向け表示で桁区切りし、数値columnを右揃えにする。Last Usedはreference timeのlocal timezoneで短い日時とzoneを表示し、machine-readable形式ではRFC 3339を維持する。色はtitle・heading・warning・failureの識別を補助するだけとし、text labelや数値を省略しない。

layoutは取得した幅に応じて3段階とする。100 column以上では全内訳を含むwide layout、70〜99では補助columnを減らすstandard layout、70未満ではnameと主要countを中心にしたcompact layoutを使用する。長いnameは表示幅を考慮してellipsisで短縮し、rowを複数lineへ折り返さない。`stats` summaryは狭幅時に横並びから縦並びへ切り替える。幅を取得できないplain outputでは80 column相当のstandard layoutを使用する。

0件時は空tableを描画せず、titleとfilter contextに続けて対象期間に利用がない旨を表示する。JSONは空arrayを維持する。

### 9. fixtureとgolden reportで互換性を固定する

実データをそのままrepositoryへ追加せず、user text、path、ID、tool outputを匿名化した最小fixtureを用意する。少なくとも次を個別fixtureまたは組合せfixtureで覆う。

- `function_call` と `custom_tool_call: exec`
- `ItemCompleted` の各対応runtime action
- `$skill` requestと `<skill>` injectionのpair
- `SKILL.md` と `scripts` access
- 同一turnの重複証拠、複数turn、複数session
- malformed line、unknown record、欠落field、長いline
- `sessions` / `archived_sessions` と期間境界

parser・normalizerにはtable-driven test、各commandにはgolden JSON/report testを置く。human reportは60・80・120 column、TTY/non-TTY、dark/light color profile、`NO_COLOR`、3つのcolor mode、empty stateを固定capabilityでsnapshot化する。ANSI sequenceの有無、wide/compactで保持すべきfield、Unicodeを含むnameのellipsisをassertする。JSON decoderとSkill detectorにはfuzz testを追加し、panicしないことと任意本文をSkillと誤認しないことを確認する。

## Risks / Trade-offs

- [Codex schemaが変更され、既知recordを取りこぼす] → tolerant decoder、CLI version付きwarning、version別fixtureで検知し、detectorを独立して更新できるようにする。
- [effective viewがmodel callとruntime actionを誤って対応付ける] → call IDを最優先し、`exec`以外は確実に重複と判断できる場合だけ抑制する。model/runtime viewを利用者が直接確認できるようにする。
- [`explicit-request` が失敗したSkill選択も利用として数える] → `unconfirmed` として分離し、`--strict` から除外する。注入証拠があれば同一turnでconfirmedへ昇格する。
- [現在のfilesystem状態が過去のSkill名解決へ影響する] → 履歴内metadataを優先し、filesystemは補助情報だけに使う。検出method/stateを結果へ残す。
- [逐次scanが履歴増加に伴い遅くなる] → MVPでは正しさと単純さを優先する。profileで必要性を確認してから、並列scanまたはincremental cacheを別changeで導入する。
- [pathやcommandに機微情報が含まれる] → table/JSONには集計名と件数だけを既定出力し、raw command・tool output・Skill本文は保持または表示しない。
- [ANSI sequenceがpipe・JSONへ漏れる] → machine rendererをstyle layerから分離し、non-TTY・`NO_COLOR`・強制colorを含むgolden testで検証する。
- [terminal幅やUnicode幅でcolumnが崩れる] → terminal capabilityを注入可能にし、Lip Glossの表示幅計算と複数幅のsnapshot testを使用する。
- [描画libraryのdependencyが増える] → Lip Glossとterminal検出に限定し、versionを固定して `go mod verify` とdependency reviewを行う。interactive frameworkは導入しない。

## Migration Plan

既存実装や永続dataがないためmigrationは不要である。実装はdomain modelとfixture、Codex ingestion、normalization、aggregation、machine renderer、human renderer、CLI wiringの順に追加し、各段階でtestを通す。release前に匿名化fixtureとread-onlyな実環境sampleの両方で、plain report・styled report・JSONを比較する。

rollbackはbinaryまたはreleaseを以前の版へ戻すだけで完了する。catsiftはCodex履歴を変更せずcacheも作らないため、user dataのrollback手順は不要である。
