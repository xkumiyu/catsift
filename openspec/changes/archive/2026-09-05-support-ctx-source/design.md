## Context

現状の入力経路は、Codex homeを解決して `sessions` と `archived_sessions` のJSONLを読み取り、共通の `usage.Turn` へ組み立てる構造である。aggregateとrendererは入力の正規化結果を消費するため、source固有処理を入力adapterへ閉じ込める余地がある。

この変更の仕様は `specs/ctx-history-ingestion/spec.md`、`specs/usage-statistics-cli/spec.md`、`specs/usage-event-normalization/spec.md`、および `specs/unused-skill-report/spec.md` に定義する。動機と変更範囲はproposal.mdを参照する。

## Goals / Non-Goals

**Goals:**

- `codex` または `ctx` を1回の実行で1つだけ選択できる入力境界を作る。
- 既存Codex JSONL readerを維持しながら、ctxの公開された読み取り専用event streamを共通normalized modelへ接続する。
- ctx内の複数Agentを検出し、sourceとAgent一覧をreportへ表示し、集計値をAgent横断で合算する。
- Agent・source・session identityを保持して、異なるAgentの同名IDが衝突しないようにする。
- `skills --unused` のinventory identityを物理Skill directoryのabsolute PATHとして維持する。
- 既存のCodex default invocationとJSONの `agent` stringを壊さず、複数Agent表現を追加する。

**Non-Goals:**

- ctxの `usage.sqlite`、Core/Tantivy、WAL、またはその他の内部storage schemaを直接読むこと。
- 1回の実行でCodex sourceとctx sourceを混在・合算すること。
- 初回対応でAgent別の集計table、`--group-by agent`、またはAgent別のTool/Skill比較を提供すること。
- 履歴から使用された物理Skill PATHを復元し、同名SkillのPATHごとにused/unusedを判定すること。
- ctx側のindex refresh、import、daemon、provider-owned履歴を変更すること。

## Decisions

### 1. 入力sourceをadapterで分離する

CLIは `--source codex|ctx` を解決し、sourceに応じて1つのadapterだけを起動する。Codex adapterは現在のhome解決・JSONL decoder・turn assemblerを継続利用し、ctx adapterは同じaggregate境界へnormalized turnを渡す。

sourceを同時指定可能なunionにしないのは、同一Codex履歴が直接のJSONLとctxのindexの両方に存在し得るためである。union方式では二重計上防止、source間のidentity照合、異なる正規化結果の優先順位が必要になり、今回の利用目的を超える。

### 2. ctxは公開CLIのevent streamを読む

ctx adapterは、ctxが提供する機械向けの読み取り専用event列挙を使用する。実装上は `ctx list events --content full --format jsonl` を基本経路とし、明示されたdata rootがある場合だけ `--data-root` を付加する。ctx executableが存在しない、起動に失敗する、またはevent streamのcompletionを確認できない場合はsource errorとする。

この方式を直接SQLite queryより選ぶ理由は、ctxのprovider差異とCore/Tantivyの世代・cursor・content policyをctx自身に解決させられ、内部schema変更にcatsiftが追随する必要を減らせるためである。`usage.sqlite` はctx自身のcontent-freeな利用集計であり、catsiftのsession・prompt・Tool・Skill入力には使用しない。

### 3. pageとcompletionを検証し、完全なsnapshotだけを集計する

ctx event streamはbounded pageとopaque cursorを返し得るため、adapterはcompletion recordまで読み、continuation cursorが残る場合は次pageを取得する。completionがない、cursorを進められない、またはstreamが途中で終了した場合は、部分結果を完全なall-time reportとして成功させない。

ctx側のimmutable generationを1回のreportのsnapshot境界とし、catsift側でeventを再ソートして意味を変えない。Agent一覧と集計rowはcanonical IDで決定的にsortする。

### 4. normalized modelにsource/Agent metadataを追加する

ctx eventの `provider`、provider session、ctx session/event identityを入力metadataとして保持する。既存の `usage` modelには、観測またはその `SourceRef` からsource kindとcanonical Agent IDを追跡できるmetadataを追加し、session・Skill deduplication keyにはsourceとAgentを含める。

異なるAgentで同じprovider session IDが使われても、論理session keyは衝突しない。display用のAgent名とmachine-readable用canonical IDは分離し、未知のAgentは `unknown` scopeへ隔離する。

### 5. ctx eventを既存のturn・Tool・Skill semanticsへ写像する

ctxのnormalized eventは、提供されるidentity・timestamp・role・event type・text・structured content・activityを使って既存のturn assemblerへ渡す。基本的な写像は次のとおりとする。

| ctx eventの事実 | normalized observation |
| --- | --- |
| user roleのmessage | user prompt。ただしcontext injectionと明示されたmessageはpromptから除外 |
| modelのtool call/activity | model Tool observation |
| command completionまたはruntime activity | runtime Tool observation。開始と完了が同一invocation identityなら1回へ統合 |
| Tool result/outputだけ | Tool call数へ追加しない。対応するcompletionのstatus判定へ利用 |
| structured Skill activity | `structured-tool` のconfirmed Skill evidence |
| canonical `$skill` markerまたは具体的Skill PATH access | 既存のexplicit-requestまたはimplicit-access evidence |

ctxがmodel/runtimeの区別またはSkill根拠を保持していない場合は、推測による二重計上やSkill利用の創作を避け、取得できる範囲を正規化し不足をwarningへ記録する。

turn boundaryをctxに依存した専用fieldだけで決めず、session内のevent sequence、user message、およびinvocation identityから決定的に組み立てる。providerが明示的turn identityを持つ場合はそれを優先する。

### 6. report contextはSourceとAgentsを分け、集計rowは合算する

human-readable reportには次のcontextを出す。

```text
Source: Codex (~/.codex)
Agents: Codex
Period: all time
```

ctx sourceで明示的なdata rootが指定された場合は、source名とpathを同じ行へ表示する。

```text
Source: ctx (/path/to/ctx-data)
Agents: Codex, OpenCode
Period: all time
```

Codexのpathは有効なCodex home（`--codex-home`、`CODEX_HOME`、既定の`~/.codex`の優先順）を表示し、ユーザーhome配下は`~`へ短縮する。ctxは`--ctx-data-root`が明示された場合だけpathを表示し、別の`History`や`Data root`行は追加しない。path表示はhuman-readable reportだけの情報であり、JSONのcanonicalな小文字`source`、`agents`、および既存fieldは変更しない。

通常のstats、tools、skillsではAgent別columnを追加せず、選択scope全体を合算する。同じcanonical ToolまたはSkillは1rowへまとめる。Agent一覧はcanonical ID順に並べ、display nameを表示する。

JSONには既存の `agent` stringを残し、`source` と `agents` arrayを追加する。単一Agentでは `agent` の値を従来どおりに保ち、複数Agentではcanonical IDを決定的順序でcomma区切りしたlegacy scope valueとする。新しいconsumerは意味上の正規値として `agents` arrayを使う。

### 7. `skills --unused` はlogical name判定とphysical rowを分ける

inventoryのidentityは `(canonical skill name, absolute path)` とし、同名・異PATHのentryをdeduplicateしない。未使用判定は既存互換のcanonical name単位で行い、ctx内の全AgentのSkill利用集合をunionしてからinventoryと比較する。

このため、同名Skillが2つのPATHにあり、そのcanonical nameが未使用なら2rowを出力する。一方、いずれかのAgentでそのcanonical nameが使用済みなら、PATHごとの使用根拠が取れない段階では両方を使用済みとする。物理PATH単位のused判定は将来の別設計とする。

### 8. testはsynthetic event streamでsource境界を検証する

ctx commandそのものやユーザーのctx data rootに依存しないfixtureを用意する。fixtureにはcompletion、複数page、Codex/OpenCodeの複数Agent、同名Tool/Skill、未知event、失敗Tool、および同名・異PATHのinventoryを含める。

adapter testでは外部command runnerを差し替えてstdout/stderr、exit code、cursor継続、completion未達を検証する。既存Codex fixtureはそのまま実行し、source未指定時の結果とJSON `agent` 値が変わらないことを回帰検証する。

## Risks / Trade-offs

- [ctx CLIのevent contractとcatsiftの想定schemaがずれる] -> adapterにclosedな必須field検証とunknown event warningを設け、実ctx出力を更新時に確認するsynthetic contract testを維持する。
- [ctxのnormalized eventがCodex raw JSONLより情報を失っている] -> `--content full` を使用し、取得できるactivity/structured contentを優先する。取得不能な細部はunknown/partialとして扱い、推測で件数を増やさない。
- [ctx event数が大きくreport実行時間が伸びる] -> ctx側のtime/provider/data-root filterを利用し、stream/page処理で全eventをmemoryへ一括展開しない。集計に不要な本文は保持しない。
- [複数Agentの同名session・Tool・Skillが衝突する] -> normalization keyにsourceとAgentを含め、reportの合算時だけcanonical nameへ畳み込む。
- [複数Agentを1つの合算値だけで示すと内訳が分からない] -> contextへAgent一覧を出し、Agent metadataを内部保持する。Agent別tableは別changeとして追加できる形にする。
- [JSON consumerが単一Agentを前提にしている] -> 既存 `agent` stringを残し、`agents` arrayを追加する。複数Agentでは新fieldを正とすることをREADMEとJSON契約に明記する。
- [skills --unusedで同名PATHの一方だけを誤ってunusedとする] -> 初回はcanonical name単位の保守的判定を維持し、physical PATH単位の判定を行わないことを仕様へ明記する。

## Migration Plan

1. `--source` 未指定の実行を従来のCodex経路へ残し、既存の `CODEX_HOME` と `--codex-home` の挙動を維持する。
2. ctx source、source固有option、multi-Agent context、JSON追加fieldを実装し、synthetic fixtureで検証する。
3. README.mdとREADME.ja.mdへsource選択、ctxのread-only前提、合算 semantics、`skills --unused` のPATH semanticsを追加する。
4. リリース後にctxが利用できない環境でもCodex sourceが独立して動作することを確認する。ctx側の変更でadapterが失敗してもCodex経路はrollback不要で継続できる。
