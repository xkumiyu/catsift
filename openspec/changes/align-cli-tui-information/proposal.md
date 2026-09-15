## Why

TUIのQuery / Read Modelには、Overviewのdaily trend、Model別の利用状況、Skill detail、SessionとTurn detailが既に存在します。一方、CLIからは既存のoverview・Tool・Skill summaryしか取得できず、同じ正規化済み履歴をscriptや非interactive環境から調査できません。

CLIとTUIの表示形式や操作性は分けたまま、TUIで確認できる安全な情報をCLIからも到達可能にします。

## What Changes

- `activity` subcommandを追加し、TUI Overviewのdaily activityを単独のhuman-readable reportとJSONの両方で取得できるようにする。`stats`はsummaryのみを表示する。
- `models` subcommandを追加し、Model一覧と`models detail provider/name`によるModel detailを表示できるようにする。
- 既存の`skills` subcommandに`skills detail NAME`を追加し、Skill summaryと利用Sessionごとのdetailを表示できるようにする。既存の`--unused` viewは維持する。
- `sessions` subcommandを追加し、Session一覧と`sessions detail ID`によるSession detailを表示できるようにする。Session detailにはTUIのTurn detailに相当する安全なTurn summaryを含める。
- 追加するcommandとoptionは既存のsource・期間・warning・`--json`の規則を継承し、human-readable reportとJSONが同じ情報を表すようにする。
- CLIのdetail出力は既存のQuery / Read Modelを利用し、prompt本文、Tool arguments、Skill本文、provider payloadを表示しない。
- 既存の`stats`、`tools`、`skills`の既定summary、rootのoption境界、および`export`を追加しない方針は維持する。

## Capabilities

### New Capabilities

なし。既存のusage statistics CLI capabilityを拡張する。

### Modified Capabilities

- `usage-statistics-cli`: TUIに存在するtrend、Model、Skill、Session、Turn summaryへCLI subcommandまたはdetail subcommandで到達できる要件を追加する。

## Impact

- `cmd/catsift`に`activity`・`models`・`sessions` commandと各resourceの`detail` subcommandの引数処理を追加する。
- `internal/query`の既存Read ModelをCLI rendererから利用し、Model・Skill・Session detailのhuman-readable/JSON rendererを追加する。
- `internal/output`に追加reportの表示とmachine-readable schemaを追加する。既存report schemaと既定出力は維持する。
- synthetic fixtureを使ったCLI integration test、Queryとrendererのtest、README.md / README.ja.md、OpenSpecを更新する。
- 新規の外部依存、cache schema変更、履歴source adapterの変更は行わない。
