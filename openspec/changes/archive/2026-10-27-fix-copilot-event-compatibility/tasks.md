# Tasks

## 1. Copilot event互換性

- [x] 1.1 12 typeの受理、metadata非集計、Skill参照形式と重複排除・privacyの回帰テストを先に失敗させ、最小修正で通す。
- [x] 1.2 ParserVersionを更新し、v7 cache無効化・fresh/cold/warm一致・session.usage_record維持をsynthetic入力で検証する。

## 2. Warning表現

- [x] 2.1 共通unknown_type adviceを変更し、CLI通常/verboseとTUI、JSON形状、未知type件数を回帰テストで検証する。

## 3. 統合確認

- [x] 3.1 mise run fmt、mise run check、git diff --check、OpenSpec strict validateを通し、quality/verify skillに沿ってdiffとscenario対応をreviewする。

- [x] 3.2 reviewで判明したidentityのないSkill eventによる空turn生成・既存turn更新を共通処理で防ぎ、回帰テストと統合checkで確認する。

- [x] 3.3 active turnのない参照を時刻更新より先に除外し、終了後/shutdown後のturn数・token帰属と帰属不能の診断をfresh/cold/warm cache・CLI/TUIで検証する。v8 cacheも無効化する。

- [x] 3.4 帰属不能diagnosticの文言を可能性に修正し、重複の可能性があるreceiptの診断件数を維持する。参照だけのsessionを登録せず、usage helperとcacheの回帰を検証する。

- [x] 3.5 metadata-only sessionとinline identityの既存互換性を仕様・テストに明記し、compatibility/diagnostic/receipt-only cache回帰でwarm hitを直接検証する。
