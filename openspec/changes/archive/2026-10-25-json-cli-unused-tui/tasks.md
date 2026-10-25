# Tasks

## 1. JSON interface

- [x] 1.1 CLI report/detailをJSON既定にし、互換flag警告・help・無効引数の回帰テストを通す
- [x] 1.2 両READMEのreport案内を更新し、diffで整合性を確認する（配布Skill sourceはPR #17へ分離）

## 2. TUI inventory

- [x] 2.1 初回/reloadのinventory探索とquery filter連動を実装し、合成inventoryでstrict/期間/sourceをテストする
- [x] 2.2 Skills usage/unused切替、名前/path/roots/未観測表示を実装し、検索・empty・warning・reloadのテストを通す
- [x] 2.3 両READMEへTUI unusedの案内を追加し、整合性を確認する

## 3. 統合検証

- [x] 3.1 mise run fmt、mise run check、git diff --check、openspec validate json-cli-unused-tui --strictを成功させる
- [x] 3.2 code reviewとOpenSpec verifyで仕様・実装・テストを照合し、発見事項を修正する
