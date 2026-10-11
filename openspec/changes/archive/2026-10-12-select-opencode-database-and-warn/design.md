# Design

## Context

現在のOpenCode adapterはdata root内のdatabaseを発見し、正規化済み結果を共通cacheへ保存します。channel切り替え後に複数のdatabaseが残る場合でも、履歴を自動統合せず1つを選択し、選択の曖昧さをwarningとしてrendererへ渡す必要があります。詳細な目的と対象範囲は`proposal.md`を参照してください。

## Goals / Non-Goals

**Goals:**

- primary databaseの互換性を保ちながら、primaryがない場合の選択結果を決定的にする。
- database本体とSQLite sidecarの更新を選択とcache revisionの両方で考慮する。
- cache hitとcold readで同じwarningを返し、warningをnormalized snapshotへ保存しない。
- CLIとTUIでwarningの件数をrecord skipとして誤表示しない。

**Non-Goals:**

- 複数databaseのmerge、削除、移動、または書き換え。
- OpenCode data root内のfileへの書き込み。
- channel名の意味を推測した優先順位の導入。

## Decisions

### primary優先と更新時刻によるfallback

`opencode.db`があれば常にprimaryとして選択します。primaryがない場合だけ、候補ごとのdatabase本体、`-wal`、`-shm`の最新`ModTime`を比較し、降順で選びます。同時刻はcandidate nameのlexicographic順でtie-breakします。

候補のpathをdirectoryの`.db` entryから作るため、sidecarだけが残ったpathを独立したdatabaseとは扱いません。database本体が見つからない場合は候補から除外し、読み取り対象がなくなれば空入力とします。

### warningをcache snapshotから分離

database discovery warningはsourceを読んだ結果ではなく、現在のdata rootの状態に依存します。そのためnormalized snapshotへ含めず、snapshotの生成・読み取り後にwarningを付加します。filter適用後に付加することでcold readとcache hitのcontrol flowを統一します。

### warning表示の分類

`opencode_multiple_databases`の`Count`は無視したdatabase数であり、record skip数ではありません。CLIのsummaryでは専用のdatabase countとして表示し、TUIでは単数・複数の文言を直接選択します。`Type`は既存の自由文字列warning modelの範囲で`database`を使用し、closed enumは追加しません。

### 代替案

- 全databaseをmergeする案は、重複排除とsource identityの定義が必要になるため今回のread-only修正の範囲を超えます。
- filenameの辞書順だけで選ぶ案は、channel切り替え後の最新履歴を選べないため採用しません。
- warningをcacheへ保存する案は、staleなdatabase状態を次回実行へ持ち越すため採用しません。

## Risks / Trade-offs

- [directory discoveryとdatabase openの間にfileが置換される] → source read errorとして扱い、部分結果を成功結果にしません。
- [sidecarが孤立して残る] → sidecarだけでは候補にせず、対応するdatabase本体が存在する候補だけを比較します。
- [複数databaseの一方にのみ履歴がある] → 自動mergeはせず、warningとadviceで利用者に整理を促します。

## Migration Plan

既存のcache schemaやOpenCode databaseは変更しません。既存cacheはparser version変更時に通常どおりmissとなり、新しいsnapshotを保存します。問題がある場合はPRの変更をrevertすれば、従来のdatabase discoveryへ戻せます。
