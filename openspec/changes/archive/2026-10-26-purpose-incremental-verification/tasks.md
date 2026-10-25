# タスク

## 1. storeと再検証ロジック

- [x] 1.1 purposes storeへ`schema_version`、entryへ`verified_at`とrevision evidenceを追加し、旧形式を読み込めるようにする
- [x] 1.2 revision evidenceの生成関数と、version・taxonomy・evidenceによる再利用可否の判定を実装する
- [x] 1.3 検証時刻の古い順に最大50件を選ぶ巡回検証の対象決定を実装する

## 2. 二相ロード

- [x] 2.1 分類有効時のingestion cache bypassを廃止し、最初のloadをcacheから行う
- [x] 2.2 Codex cache復元時にsessionへsource pathをメモリ上だけで付与する
- [x] 2.3 Codexに指定fileだけを読み込むoptionを追加する
- [x] 2.4 再検証対象sessionの本文だけをsource別に読み込む処理を実装する
- [x] 2.5 分類処理を、保存済み再利用、fingerprint照合、API分類の3段階に再構成する

## 3. CLIとTUI

- [x] 3.1 初回loadとTUI reloadの両方で新しい分類処理を配線する
- [x] 3.2 pending数による確認しきい値と、reload時のskip挙動を維持する

## 4. テスト

- [x] 4.1 storeの後方互換、`verified_at`、evidenceの保存を検証する
- [x] 4.2 証拠一致時に本文parseとAPI requestが発生しないことを検証する
- [x] 4.3 証拠変化時と巡回検証時のfingerprint一致・不一致の分岐を検証する
- [x] 4.4 Codexの対象file限定読み込みと、source pathが永続化されないことを検証する
- [x] 4.5 既存の確認・skip・並列requestのテストを新しい処理に合わせて維持する

## 5. ドキュメントと検証

- [x] 5.1 ユーザー指定によりREADMEとREADME.ja.mdへの追加を取り消し、既存の説明を維持する
- [x] 5.2 `mise run check`と`git diff --check`を実行する
- [x] 5.3 `openspec validate 2026-10-26-purpose-incremental-verification --strict`を実行する
