## ADDED Requirements

### Requirement: 分類用の本文parseを指定fileへ限定する

Codex adapterは、分類のために本文parseが必要な場合、指定されたfileだけを読み込めなければならない（SHALL）。指定fileの読み込みでは通常のhistory発見を省略し、session titleの補完とtimestamp filterの既存挙動を維持しなければならない（SHALL）。fileが指定されていない場合の既存の全file読み込みとcache利用を変更してはならない（MUST NOT）。

#### Scenario: 指定fileだけを読む

- **WHEN** 分類が再検証対象のCodex sessionに対応するfileを指定する
- **THEN** adapterはそのfileだけをparseし、他のhistory fileを開かない

#### Scenario: 指定がない場合は全fileを読む

- **WHEN** file指定なしでCodex historyを読み込む
- **THEN** adapterは既存どおり発見した全fileとcacheを使用する
