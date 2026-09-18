## 1. CLI command contract

- [x] 1.1 `models`と`sessions`をroot help、command dispatch、command helpへ追加し、`activity`と各resourceの`detail` subcommandのscopeを定義する。`go test ./cmd/catsift`でhelpと不正optionのvalidationを確認する
- [x] 1.2 既存のsource・期間・warning・`--json`・`--strict-input`経路を新しいviewへ接続し、root/TUIのcommand省略経路がreport optionを受け付けないことを確認する。CLI integration testでexit code、stdout、stderrを検証する

## 2. Query data access

- [x] 2.1 `activity`、`models`、`skills detail`、`sessions`が同じ`query.Input`から`query.Build`を利用する経路を追加する。synthetic inputでTUIとCLIのscope、deduplication、deterministic sortが一致するquery testを追加する
- [x] 2.2 Model、Skill、Sessionのdetail subcommandを実装し、未存在とSession IDの曖昧一致を安全なoption errorへ変換する。該当する成功・失敗ケースのunit testを通す
- [x] 2.3 `skills detail --strict`のconfirmed-only semanticsと、`--unused`・`--group-by`・`--view`とのdetail mode validationを実装する。既存のskills list/unused testが変わらず通ることを確認する

## 3. Report rendering

- [x] 3.1 `activity`のhuman-readable/JSON rendererを追加し、日付、Sessions、Turnsと既存stats summaryのscopeを同じにする。human/JSON parityと既存`stats` default outputのregression testを通す
- [x] 3.2 `models`のlist/detail human-readable/JSON rendererを追加し、Model summary、関連Session、Token usage、利用時刻を出力する。JSON schema testとterminal width testを通す
- [x] 3.3 `skills detail`のhuman-readable/JSON rendererを追加し、mode・state・methodおよび利用Sessionごとのsummaryを出力する。deduplicated evidenceとstrict detailのrenderer testを通す
- [x] 3.4 `sessions`のlist/detail human-readable/JSON rendererを追加し、Session metadataとchronological Turn summaryを出力する。TurnのModel、Token、Tools、Skills、Status、時刻のfixture testを通す
- [x] 3.5 追加rendererの公開JSON型を明示し、warningをstderr、結果をstdoutへ分離する。malformed/skip warningを含むintegration testでstdoutが単一JSON documentになることを確認する
- [x] 3.6 synthetic fixtureのprompt本文、command本文、Tool arguments、Skill本文、provider payloadがhuman-readable/JSONのどちらにも現れないことを確認するprivacy regression testを追加する

## 4. Documentation and compatibility

- [x] 4.1 `README.md`と`README.ja.md`へ新しいcommand、detail subcommand、`activity`、human/JSON parity、privacy境界をcontent-equivalentに追加する。見出し、option、example、code fenceの対応を比較する
- [x] 4.2 OpenSpec deltaと実装のcommand surface、error条件、既存default output維持を照合する。`openspec validate --change align-cli-tui-information --strict`を通す
- [x] 4.3 `mise run check`を実行し、Go test、race test、lint、build、npm testを含むrepository quality gateを通す

## 5. Detail subcommand migration

- [x] 5.1 `models --model`、`skills --skill`、`sessions --session`をそれぞれ`models detail`、`skills detail`、`sessions detail`へ移行し、list/detailで有効なoptionを分離する
- [x] 5.2 command help、README.md、README.ja.md、OpenSpecのexamplesとvalidation testを新しいdetail subcommandへ同期する
- [x] 5.3 `go test ./...`、`openspec validate align-cli-tui-information --strict`、`mise run check`を実行してdetail subcommand移行を検証する

## 6. Activity command migration

- [x] 6.1 `stats --trend`を廃止し、daily activityを単独表示する`activity` commandへ移行する。`stats`はsummary-onlyを維持する
- [x] 6.2 `activity`のhelp、human-readable/JSON renderer、integration test、README.md / README.ja.mdを同期する
- [x] 6.3 `go test ./...`、OpenSpec validation、`mise run check`を実行してcommand移行を検証する
