## Context

既存の履歴adapterは`usage.Turn`と`usage.Session`を生成し、`internal/query`はそれらからOverview、Model、Skill、Session、TurnのsafeなRead Modelを構築しています。TUIはこのRead Modelを直接使用しますが、CLIは`internal/aggregate`のOverview・Tool・Skill summaryだけを使用しています。

既存の`stats`、`tools`、`skills`のdefault reportとJSON schemaは利用者のautomationに影響するため、TUI対応のために既存summaryへdetailを無条件で追加しません。新しいCLI viewは、TUIと同じnormalized inputとQuery semanticsを使い、必要な場合だけ専用commandまたはdetail subcommandで選択します。

## Goals / Non-Goals

**Goals:**

- TUIのOverview daily trend、Model summary/detail、Skill detail、Session/Turn detailをCLIから取得可能にする。
- human-readable reportとJSONで同じlogical dataを扱い、既存のsource・期間・warning・privacy規則を共有する。
- 既存の`stats`、`tools`、`skills`のdefault outputと既存JSON fieldを維持する。
- 既存の`internal/query`をsingle source of truthとして利用し、TUIとCLIで集計・deduplication・sortを再実装しない。

**Non-Goals:**

- TUIとCLIのlayout、navigation、sort key、検索入力、viewportを同一にしない。
- rootの`--json`、`export` command、Web API、外部dashboardを追加しない。
- raw prompt、command、Tool arguments、Skill本文、provider payloadをCLIへ公開しない。
- 新しい履歴adapter、cache schema、外部dependencyを追加しない。

## Decisions

### 1. CLIのviewを既存summaryと専用detailへ分ける

次のcommand surfaceを追加する。

```text
catsift stats [common report options]
catsift activity [common report options]
catsift models [common report options]
catsift models detail provider/name [common report options]
catsift skills [common report options]
catsift skills detail NAME [common report options]
catsift sessions [common report options]
catsift sessions detail ID [common report options]
```

`activity`はOverviewのdaily activityだけを表示する独立report commandとし、`stats`はsummaryのみを表示する。Model、Skill、Sessionのdetailは一覧commandから分離したnested subcommandとし、各resourceのlistとdetailで有効なoptionを明確に分ける。これにより、既存JSONへTUI全体のRead Modelを混在させず、各commandのscopeが明確になる。

`models detail`の`provider/name`はnormalizedなModelをcase-insensitiveに解決する。`skills detail`のNAMEはnormalized Skill nameをcase-insensitiveに解決する。`sessions detail`のIDは現在のsource・期間scope内のSession IDを解決し、一意でない場合は候補を示して終了する。内部のNUL区切りRead Model keyをCLI引数として公開しない。

### 2. History loadingは既存経路を再利用し、detailだけQueryへ接続する

`cmd/catsift`のsource選択、`--days`、`--from`、`--to`、cache、warning、`--strict-input`は既存の`loadSelectedHistory`経路を使う。新しいcommandはloaderが返すsanitizedな`query.Input`を`query.Build`へ渡し、次のように取得する。

- `activity`: `ReadModel.Overview.Trend`
- `models`: `ReadModel.Models`、`models detail`では`ReadModel.ModelDetail`
- `skills`: `ReadModel.Skills`、`skills detail`では`ReadModel.SkillDetail`
- `sessions`: `ReadModel.Sessions`、`sessions detail`では`ReadModel.SessionDetail`

TUIのstateやBubble Tea型をCLIへ持ち込まない。CLI rendererは`internal/query`の型を入力にし、TUI rendererのtable helperを共有しない。これにより、表示形式は分離しつつ集計結果は同一になる。

### 3. 既存commandとdetail modeのvalidationを分ける

共通optionはsource、期間、color、verbose、strict-input、jsonとする。daily activityは`activity` commandで表示し、Model・Skill・Sessionのdetail optionはそれぞれの`detail` subcommandだけで有効にする。rootおよびTUIのcommand省略経路では、これらのreport optionを受け付けない。

`skills detail`は通常のusage detailを対象とし、`--unused`、`--group-by`、`--view`はdetailの固定schemaと別modeのため受け付けない。`--strict`は既存のconfirmed-only semanticsをdetailにも適用する。既存の`skills` listと`skills --unused`の挙動は変更しない。

### 4. Rendererはcommandごとの明示的なschemaを持つ

`query.ReadModel`をそのままJSON marshalしない。privateなdetail map、内部filter、将来の実装fieldが公開されることを防ぐため、`internal/output`にcommandごとの公開用JSON型を定義する。

- `activity`: standalone JSON envelopeの`rows`としてdaily activityを返す。各rowはUTC date、Sessions、Turnsを持つ。`stats`の既存summary JSONは変更しない。
- `models`: listはModel summary rows、`detail`はsummaryと関連Session rowsを持つ。
- `skills detail`: detailはSkill summaryと関連Session rowsを持つ。既存list JSONのshapeは維持する。
- `sessions`: listはSession rows、`detail`はSession summaryとchronological Turn rowsを持つ。

Timestampは既存JSONのmachine timestamp formatter、human reportは既存のlocal time・compact count・color capabilityを再利用する。JSONのwarningはstderrへ出し、stdoutは常に単一documentとする。

### 5. Session detailはTurn detailを内包する

TUIではSession detailからTurn detailへ遷移するが、CLIにはnavigation stateを持ち込まない。`sessions detail ID`のTurn rowへ、TUIのTurn detailで表示するModel、Token usage、canonical Tool name、Skill name、Status、Started、Endedを含める。これで別の`--turn` optionを追加せず、Session detail一回で同じ情報へ到達できる。

Session IDが複数候補に一致する場合は、source、Agent、provider、およびIDを候補としてstderrへ表示する。候補を黙って選択せず、source optionでscopeを狭めて再実行できるようにする。

### 6. Privacyと決定性をQuery境界で保証する

追加rendererはQueryが返すbounded dataだけを参照する。prompt本文、command本文、Tool arguments、Skill本文、provider payloadは型・field選択の両方で出力対象から除外する。Model、Skill、Session、Turnの順序はQueryが生成した順序を使用し、renderer側でmap iteration順に依存するsortを追加しない。

`activity`のdaily activityは`stats`と同じloader scope、reference time、UTC日付bucketを使う。既存aggregate statsとの互換性は、default statsのgolden/regression testを維持し、activityのQuery trend fixture testを追加して確認する。

### 7. CLIの利用面をhelpとREADMEで明示する

root helpに`models`と`sessions`を追加し、各command helpにlist/detail subcommandと`--json`の意味を記載する。README.mdとREADME.ja.mdは同じcommand、option、privacy、human/JSON parityを説明する。TUIのnavigation操作やCLIで再現しないviewportは、情報差分ではなく表示形式の差分として扱う。

## Risks / Trade-offs

- **Queryと既存aggregateの集計値が将来ずれる** → `activity`のfixtureでTUI Overview、Query、既存statsのscopeとdaily activityを比較し、default statsのregression testを維持する。
- **同じSession IDが複数sourceまたはAgentに存在する** → source scopeで絞り、なお曖昧な場合は候補を表示して非0終了する。任意の候補を選ばない。
- **Session detailが大きくなる** → detail commandで明示的にSessionを選択した場合だけ全Turn summaryを出力し、raw履歴や本文は含めない。TUIのviewportはCLIへ移植しない。
- **既存`skills` optionとの組合せが複雑になる** → detail modeで意味を持たない`--unused`、`--group-by`、`--view`を明示的に拒否し、list modeの互換性を維持する。
- **JSON consumerが追加fieldを想定しない** → 既存stats/tools/skillsの既定schemaは変更せず、activityは独立したschemaとして提供する。
- **detail outputからsensitive dataが漏れる** → rendererの公開型を明示し、synthetic fixtureにprompt・arguments・Skill本文のsentinelを入れ、stdoutに現れないことを検証する。

## Migration Plan

1. `cmd/catsift`のcommand/help/option validationを拡張し、既存commandのdefault pathと新detail pathを分離する。
2. `internal/output`へOverview trend、Model、Skill、Session、Turnのhuman-readable/JSON rendererを追加する。
3. `internal/query`を新commandから呼び出し、Model/Skill/Session selector、strict detail semantics、Session ambiguity errorを接続する。
4. synthetic inputでQueryとrendererのdeterministic/privacy/JSON parityを検証し、既存CLIのregression testを実行する。
5. help、README.md、README.ja.md、OpenSpecのcommand一覧とexampleを更新する。
6. `mise run check`を実行する。

Rollbackは新しいcommandとoptionのdispatchを除去すればよく、既存の履歴file、cache、既存report schemaのmigrationは発生しない。
