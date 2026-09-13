## Context

現状の`catsift`は、`internal/codex`、`internal/ctx`、`internal/opencode`の各adapterが履歴を`usage.Turn`とsource固有の`SessionMetadata`へ正規化し、`internal/aggregate`が期間全体のstatic reportを生成する構成である。`internal/cache`には、prompt本文やprovider payloadを含めないsource-neutralな`Snapshot`が保存されている。一方、現在のaggregate modelは、Model、個別Skillの検出根拠、Session内のTurn推移を一覧から詳細へ辿るためのread modelではない。

本変更では、source adapterと画面を直接結合せず、正規化済み履歴から問い合わせ可能なread modelを作る層を追加する。既存の`stats`、`tools`、`skills`の出力契約とcacheのprivacy方針は維持し、同じデータ契約をstatic CLIと将来のTUIで共有する。動機と利用者向けの変更範囲は`proposal.md`を参照する。

## Goals / Non-Goals

**Goals:**

- Model情報、利用状況、個別Skill詳細、Session詳細を、同じnormalized snapshotから取得できるUI-independentなQuery / Read Modelを定義する。
- CLIとTUIで情報の意味・scope・集計結果を一致させつつ、CLIはstatic report、TUIはdetailを含む対話的な探索として表示量を役割に応じて分ける。
- ModelがSessionまたはTurnの途中で変わる履歴を失わず、Token usageとModelの帰属を追跡できるようにする。
- source、Agent、期間、Project、Model、Skillのfilterと、画面ごとの決定的なsortを共通化する。
- Sessionをsource-qualifiedなidentityで扱い、source間のID衝突を防ぐ。
- 大量の履歴でもTUIが一覧をwindowingしながら表示できるよう、画面へraw履歴を直接渡さない。
- 既存のcache、warning、read-only ingestion、および既存static CLIのhuman-readable/JSON互換性を維持する。

**Non-Goals:**

- Web dashboard、HTTP server、browser frontend、webview、multi-user access、認証を追加しない。
- prompt本文、command本文、Tool arguments、Skill本文、provider payloadをQuery / Read Modelやcacheに含めない。
- 履歴をリアルタイム監視したり、外部サービスへ同期したり、Model catalog・pricing APIと接続したりしない。
- TUIから履歴の編集、再実行、削除、Skillの実行を行わない。
- 既存の`stats`、`tools`、`skills`のJSON fieldやstatic reportの表示仕様を、TUI対応のために変更しない。

## Decisions

### 1. adapterとrendererの間にQuery / Read Model層を置く

履歴の読み込み、正規化、問い合わせ、描画を次の境界で分離する。

```text
+-----------------------+
| codex / ctx / opencode|
| history adapters       |
+-----------+-----------+
            |
            v
+-----------------------+
| usage normalization    |
| + sanitized cache      |
+-----------+-----------+
            |
            v
+-----------------------+
| Query / Read Model     |
| filter / sort / detail |
+-----------+-----------+
            |                         |
            v                         v
+------------------+       +------------------+
| static CLI report|       | TUI state/view   |
+------------------+       +------------------+
```

`internal/query`（名称は実装時に既存package構成と調整可能）を、normalized dataからOverview、Model、Skill、Sessionのread modelを構築する責務のpackageとする。Query層はsource adapterやterminal APIを直接呼び出さず、既にcache可能なbounded dataだけを入力にする。既存の`internal/aggregate`の集計ロジックは再利用し、static CLIは従来の`aggregate.Report`を生成するadapterを介してJSON schemaを維持する。TUIは`aggregate.Report`の文字列や表示行を再parseせず、型付きread modelを直接利用する。

この構成により、将来Webを検討する場合も同じQuery契約を再利用できるが、本変更ではWeb用のtransportやrendererは作らない。TUI固有のselection、focus、terminal lifecycleは`internal/tui`へ閉じ込め、Query層へ持ち込まない。

### 2. Model identityはTurn単位の観測として保持する

Modelは次のような正規化済みidentityとして扱う。

```text
ModelRef
  provider       表示および識別用のprovider
  name           providerが報告したcanonical model name
```

Modelが観測された時刻とsourceを必要とするため、`TokenUsageEvent`に`ModelRef`を付与し、Token usageを伴わないModel観測も必要な場合はTurn内のModel observationとして保持する。TurnのModel集合はこれらのobservationから導出可能にし、同一Sessionに複数Modelが現れるケースを表現する。Model情報をSessionへ1つだけ置く設計や、TUIがsource固有のraw eventからModelを再推定する設計は採用しない。

Modelが履歴に記録されない場合もeventを捨てず、`provider=unknown`、`name=unknown`に相当する安定したunknown bucketへ帰属する。providerやnameの正規化は空白除去など識別に必要な最小限に留め、表示名そのものを外部catalogで置換しない。これにより、Modelが切り替わったTurnでもTokenとModelの対応、最初・最後の利用時刻、Model別のSession/Turn数を集計できる。

### 3. Session identityと共通metadataを正規化する

各sourceのSession metadataを共通のSession read modelへ変換する。共通metadataには少なくとも次を含める。

- source、Agent、provider、およびsourceが提供するprovider/session ID
- Project path、CLI version（提供される場合）
- sourceが提供するcreated/updated時刻、またはTurnから導出したstarted/ended時刻
- 表示用の短いSession IDと、内部参照用のsource-qualified Session key
- Codexでは`<CODEX_HOME>/session_index.jsonl`の`id`と最新の非空`thread_name`から取得した表示名称

Session keyは単一の`ID`だけで作らず、source、Agent、source固有のSession IDを組み合わせて作る。ctxのprovider session ID、ctx session ID、providerが欠ける場合のfallbackもadapter側で一貫して解決し、Query層はこのkeyを参照する。既存の`Turn.SessionID`とsource-local IDは互換性のため保持し、Query層でSession keyへ解決する。

Session metadataに時刻がない場合は、そのSessionに属するTurnの最小`StartedAt`と最大`EndedAt`（欠損時は利用イベント時刻）を表示用の期間として導出する。これによりadapterごとのmetadata差を画面側で吸収しない。

Codexのrollout JSONLにはSession名称がない場合があるため、Codexのresume pickerが利用する`session_index.jsonl`を補助metadata sourceとして読む。indexはnormalized Sessionへ名称を結合するが、Session自体はrollout履歴から発見したものだけを対象にする。per-file cacheの後段でindexを再読し、名称変更で履歴cache全体を無効化しない。

### 4. 画面ではなくread modelを先に定義する

Query層は次のread modelを返す。実際のGo type名やfieldの細部はspecで固定するが、raw contentではなく集計値と参照情報だけを持つ方針は固定する。

- `OverviewView`: 対象期間、Session数、Turn数、prompt数、Token usage、Tool数、Skill use数、warning、および日単位などの利用状況trend。
- `ModelSummary`: `ModelRef`、Session/Turn数、prompt数、Token usage、first/last used、関連するTool/Skillの集計。
- `SkillSummary`: Skill名、deduplicated use数、Session/Turn数、mode・state・method別の内訳、first/last used。
- `SkillDetail`: `SkillSummary`に加えて、利用Session単位に集約したuses数、distinct Turn数、first/last used、mode・state・検出methodの内訳。
- `SessionSummary`: source-qualified key、Agent、Project、時刻、Model集合、Turn/prompt/Token/Tool/Skillの集計、およびabortedなどの状態。
- `SessionDetail`: `SessionSummary`に加えて、時系列に並べた`TurnSummary`、TurnごとのModel、Token、Tool名の集計、Skillの参照。
- `TurnSummary`: ID、ordinal、時刻、状態、Model集合、利用量と、canonical Tool nameおよびSkillのboundedな集計。promptやTool argumentsは持たない。

Skill detailの利用一覧は、既存のdeduplicated `SkillUse`をsource-qualified Session単位に集約し、Sessionごとのuses数、distinct Turn数、時刻、mode、state、検出methodの内訳を保持する。同一Turnで複数methodが得られた場合はmethodsを保持する。Session detailでは同じSkill useを再推定せず、normalized evidenceを参照する。Model別とOverviewのToken合計は、Model付きToken usage eventの合計を基本とし、Model不明分はunknownへ入れて全体合計との不一致を避ける。

### 5. Filterはingestion filterとQuery filterに分ける

履歴を読む段階では、既存のsource選択と期間filterを利用して不要なsource/file/databaseの読み込みを抑える。snapshotを読み込んだ後のQuery filterは次の共通意味論を持つ。

- `source`、`agent`、`project`、期間はSessionとTurnを絞り込む。
- `model`はModel observationおよびToken usage eventを絞り込み、そのModelを含むTurnだけを残す。
- `skill`はdeduplicated Skill useを絞り込み、そのSkillを含むTurnだけを残す。
- ModelまたはSkill filterでTurnを絞った場合、関連しないSessionは一覧から除外する。ただしOverviewの対象期間とwarningは元のsource/filter contextを表示する。
- TUI内の検索はModel、Skill、Agent、Session ID、Projectなど表示用metadataに限定し、prompt本文を検索対象にしない。

既存CLIと同様、初期の一回のinvocationではsourceの混在可否を既存のsource option規則に従わせる。複数sourceを扱う場合もidentityはsource-qualifiedとし、表示上のfilterは同じ契約を使う。sortは、集計一覧ではcount降順の後にcanonical name、Session一覧では最終利用時刻降順の後にkey、Turn一覧では時刻とordinalの昇順を基本とし、同値時も安定したkeyで決定する。

### 6. Usage Explorerをcanonical入口とし、TUIは現在のfrontendとする

利用者向けのcanonicalな入口は`catsift`とする。stdinとstdoutがinteractive terminalの場合、commandを省略した`catsift`は既存のsource、期間、cache関連optionを受け取ってUsage Explorerを起動する。`--help`と`--version`は常にtop-level CLIとして処理し、非TTYでは静的出力へfallbackせず、interactive terminalが必要であることをstderrへ示して終了する。

起動時にsnapshotを一度読み込み、Query filterを適用してからTUI event loopへ渡す。初期版ではlive watcherを設けず、`r`などの明示的なreloadだけで再読み込みする。

画面遷移はOverview、Models、Skills、Sessionsをtop-level viewとし、各一覧からEnterでModel、個別Skill、Session detailへ進み、Escapeで戻る。最低限の操作は上下移動、一覧内検索またはfilter、reload、help、quitとし、具体的なkey bindingやthemeは既存CLIのterminal表示と衝突しない範囲で実装時に決める。狭いterminalでは複数paneを強制せず、一つの一覧とdetailを切り替える。

TUIのstateは、現在のroute、filter、selected key、viewport、read model、loading/error/warning stateだけを保持する。履歴adapter、JSON encoder、cache writerをview event handlerから呼ばない。長い一覧はviewport/windowingし、画面サイズ変更時に表示範囲を再計算する。warningは画面を壊すfatal errorにせず、status/diagnostics領域と詳細画面から確認できるようにする。

TUIはinteractive output専用とし、`--json`との同時利用は明示的なエラーにする。非TTYやredirect先での静的出力へのfallbackは行わず、既存のstatic commandとの責務を混ぜない。

### 7. CLIとTUIは意味論を共有し、表示量は役割で分ける

CLIとTUIは別の履歴読み込みやModel・Skill・Sessionの再推定を行わず、同じnormalized input、scope filter、warning、Query / Read Modelの集計結果を利用する。少なくともOverviewのSession、Turn、prompt、Tool、Skill、Tokenの合計は、CLIの`stats`とTUIで同じscopeなら一致しなければならない。Model不明分やdeduplicated Skill useの扱いも共通化する。

一方、CLIとTUIを同じ情報量・同じレイアウトにする必要はない。CLIはpipe、JSON、golden test、automationで扱いやすい安定したsummaryを優先し、TUIはOverviewからModel、個別Skill、Session、Turn detailへ移動できる探索性を優先する。TUIだけに存在するdetailは、CLI側に別の利用目的が生じた時点で明示的なoptionまたはsubcommandとして追加する。TUIの表示を減らしてCLIに合わせたり、TUIのdetailを既存CLI JSONへ混ぜたりはしない。

この境界により、CLIとTUIの差分は「情報の定義」ではなく「表示密度、操作性、探索深度」となる。将来Web frontendを追加する場合も、同じQuery / Read Modelを利用し、frontendごとに表示を最適化できる。

Sessionsのtop-level listは横幅を優先し、Session名称、Project、最終利用時刻だけを表示する。名称がない場合は括弧付きかつmuted表示のSession ID先頭8文字を表示し、ID fallbackであることを示す。完全なSession ID、Session nameの空値、Turn数、Token usage、およびaborted状態はSession detailで表示する。TUI内検索は一覧に表示しない完全なSession IDも対象とする。

Models、Skills、Sessionsのtop-level listおよびSkill detailのSession一覧では、最終利用時刻を`now`、`Nm ago`、`Nh ago`、`Nd ago`、`Nmo ago`、または`Ny ago`の相対表記で表示する。詳細metadataの時刻は正確な日時を維持する。

### 8. cacheはnormalized snapshotの保存形式として拡張する

Model observation、Token usageとのModel帰属、Session metadataを`internal/cache.Snapshot`へ追加する。既存どおりcacheにはprompt、Tool arguments、raw source path/line、provider payloadを保存しない。in-memory adapter dataに一時的に存在するraw fieldはcache境界で落とし、Query / Read Modelはcacheまたは同等にsanitizedされたsnapshotだけを入力とする。

新field追加時はcache schemaまたはparser versionを更新し、旧cacheを暗黙に部分利用しない。旧cacheは安全にmissとして扱い、sourceを再読み込みして新形式を作る。新形式を読めない場合も既存のwarning/error分離に従い、破損したsnapshotをTUIで推測表示しない。外部履歴ファイルやOpenCode SQLiteを変更するmigrationは行わない。

### 9. TUI frameworkはGo-nativeな単一依存へ閉じ込める

TUI event loop、terminal input、viewport描画には、Go ecosystemで保守されているTUI frameworkを一つ選ぶ。既存の`lipgloss`による表示styleは共有可能な部分だけ再利用し、frameworkの型を`internal/usage`、`internal/query`、adapterへ漏らさない。browser runtimeやWeb stackを依存に追加しない。

framework選定をUI状態とデータ契約から分離することで、後からlibraryを変更してもQuery / Read Model、cache、既存CLIを変更せずに済む。具体的なlibrary、version、theme、key bindingは実装前の小さなspikeで確定する。

### 10. テストは合成snapshotで境界を検証する

実装時は実ユーザー履歴をfixtureにせず、repository-localのsynthetic snapshotで次を検証する。

- Model変更、unknown Model、source間の同名Session、Token usage eventの帰属。
- filter後のSession/Turn包含規則、Session単位に集約したSkill detail、sortの決定性、warningの保持。
- cache round trip、旧versionのmiss、privacy fieldの除外。
- TUI state machineのroute遷移、selection、filter、reload、狭いterminalでの表示制約。
- 既存`stats`、`tools`、`skills`のhuman-readable/JSON outputが従来と同じであること。

既存adapterのparser testでは、各sourceが共通ModelとSession metadataへ正しくmapされることを追加検証する。実データへの依存や、developer環境の履歴を読むtestは追加しない。

## Risks / Trade-offs

- **Modelが履歴にない、または一つのTurnで切り替わる** → Sessionへ単一Modelを上書き保存せず、観測eventとunknown bucketを保持する。unknownの存在はOverviewとModel一覧で明示する。
- **sourceごとにSession IDの意味が異なり衝突する** → source、Agent、source固有IDからsource-qualified keyを作り、表示用IDと内部keyを分離する。
- **履歴量が多く、TUIの描画やメモリが重くなる** → ingestion/Queryで先にfilterし、集計済みread modelを渡し、一覧はviewport/windowingする。raw eventをviewへ渡さない。
- **新しいTUI依存がbinary sizeや保守負担を増やす** → Go-native frameworkを一つに限定し、UI境界に隔離する。Web stackは採用しない。
- **Session detailでprivacy情報が表示される** → prompt、arguments、provider payloadを型とcacheから除外し、表示対象を既存のsanitized metadata、集計値、canonical nameに限定する。
- **起動中に履歴が更新され、画面とsourceがずれる** → 起動時snapshotを一貫した表示単位とし、初期版は明示的reloadだけを許可する。
- **warningがfull-screen UIで見落とされる** → warning count/statusを常時表示し、warning一覧をOverviewまたはdiagnosticsから開けるようにする。
- **引数なしの`catsift`がscriptやredirectで意図せずinteractive動作になる** → TTY時だけUsage Explorerを起動し、非TTYでは履歴を読み込まず簡潔なerrorとstatic commandへのhintをstderrへ出す。automationでは`stats`、`tools`、`skills`を明示する。
- **Query層追加で既存CLIの集計が変わる** → 既存reportへのadapterを残し、既存outputのgolden/regression testを通過させる。TUI専用の詳細化を既存JSONへ逆流させない。

## Migration Plan

1. `usage`にModel identity、Model observation、共通Session metadataを追加し、各adapterからのmappingを実装する。
2. `cache.Snapshot`とround tripを拡張し、schema/parser versionを更新する。旧cacheはmissとして再生成する。
3. Query / Read Modelを実装し、既存aggregate reportと同じnormalized/filter/warning結果を使う経路を確立する。ここで既存static CLIの互換性を確認する。
4. `internal/tui`と`catsift`の既定dispatchを追加し、Overview、Models、Skills、Sessionsとdetail遷移をread modelへ接続する。
5. synthetic fixtureによるadapter、query、cache、TUI state、既存CLIのregression testを追加し、通常の`mise run check`で検証する。

rollback時は`catsift`の既定dispatchとTUI packageを無効化または削除し、既存static commandの経路を残す。cacheはversion miss後に再生成されるため、履歴ファイルの移行や逆変換は不要である。TUI実装前の段階では、proposal/designだけを取り消してもruntimeへの影響はない。

## Open Questions

- 採用するGo-native TUI frameworkと、そのversionを実装前のdependency spikeで確定する。
- terminal幅ごとのtable列、color theme、最終的なkey bindingをusability確認で決める。
- 利用状況trendの粒度（日、週、期間内自動）と、最初のTUIでchartを使うかcompact tableにするかを決める。
- Model detailを独立routeとして持つか、Models一覧からsummary/detail paneへ遷移するかを決める。Model identityとQuery契約はどちらでも変えない。
