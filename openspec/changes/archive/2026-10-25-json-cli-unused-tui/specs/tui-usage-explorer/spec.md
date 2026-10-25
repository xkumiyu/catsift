## ADDED Requirements

### Requirement: Skillsでusageとunusedを切り替える

Skills画面はusage/unusedを簡単な操作で切り替えられなければならない（SHALL）。unusedは選択履歴で未観測であり、生涯未使用を意味してはならない（MUST NOT）。名前とinstall path、scan roots、選択期間/source、strict状態、inventory件数を確認できなければならない（SHALL）。同名の異なるpathは別entryとして維持しなければならない（SHALL）。

#### Scenario: unusedへ切り替える
- **WHEN** userがSkillsで`u`を押す
- **THEN** 選択履歴で未観測のinstall entryが名前/path順で表示され、再度`u`を押すとusageへ戻る

#### Scenario: filterと検索を変える
- **WHEN** userが期間/source/Agent/Project/Model/Skillまたはstrict条件を変更する
- **THEN** unused判定は同じ選択履歴へ再適用され、row検索は名前とpathを対象に表示だけを絞る

### Requirement: TUIのinventoryを安全に読み込む

TUIは既存unused CLIと同じinventory探索・canonical name照合・warning規則を使用しなければならない（SHALL）。既定root欠落は空inventory、明示root不正はerrorとし、reload時もinventoryを更新しなければならない（SHALL）。履歴・Skill本文は表示せず、filesystemはread-onlyでなければならない（MUST）。

#### Scenario: inventory warningと欠落root
- **WHEN** inventory探索でrecoverable warningまたは既定root欠落がある
- **THEN** warningをTUIで確認でき、欠落時は空inventoryを表示し、strict-input終了規則を維持する

#### Scenario: reloadに失敗する
- **WHEN** 新しい履歴または明示inventory rootの読取に失敗する
- **THEN** 現在のsnapshotを保持し、reload errorを表示する
