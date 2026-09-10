## ADDED Requirements

### Requirement: skills commandでunused viewを選択できる

`catsift skills` は `--unused` optionを受け付け、指定された場合は通常のSkill利用集計ではなく、`unused-skill-report` capabilityで定義された未使用Skillreportを生成しなければならない（SHALL）。`--unused` がない場合の `catsift skills` の集計結果とoutput形式は変更してはならない（MUST NOT）。`--root` は `--unused` と同時に指定された場合だけ有効で、repeatableに受け付けなければならない（SHALL）。

#### Scenario: unused viewを明示的に選択する

- **WHEN** userが `catsift skills --unused` を実行する
- **THEN** システムは通常の利用rowではなく、選択されたscopeの未使用Skillだけをreportする

#### Scenario: rootを複数指定する

- **WHEN** userが `catsift skills --unused --root ~/src --root ~/work` を実行する
- **THEN** システムは2つのrootをscopeとして未使用Skillを判定する

#### Scenario: rootをunused viewなしで指定する

- **WHEN** userが `catsift skills --root ~/src` を実行する
- **THEN** システムは `--root` が `--unused` と共にだけ使えることを示す引数errorをstderrへ出力し、非0で終了する

#### Scenario: 他の統計commandへunused optionを指定する

- **WHEN** userが `catsift stats --unused` または `catsift tools --unused` を実行する
- **THEN** システムはoption errorをstderrへ出力し、既存のstatsまたはtools reportを生成しない

### Requirement: unused viewが既存の履歴filterとoutput optionを継承する

`skills --unused` は `--days`、`--codex-home`、`--strict`、`--group-by`、`--color`、`--json`、`--verbose`、および `--strict-input` を既存の `skills` commandと同じvalidation、履歴source、warning、終了codeの規則で受け付けなければならない（SHALL）。`--codex-home` は履歴sourceだけを変更し、Skill inventoryの既定scopeを暗黙に変更してはならない（MUST NOT）。

#### Scenario: days filterをunused判定へ適用する

- **WHEN** userが `catsift skills --unused --days 30` を実行する
- **THEN** システムは直近30日間の履歴だけを使用済み判定へ使い、report contextにもその期間を示す

#### Scenario: strict filterをunused判定へ適用する

- **WHEN** userが `catsift skills --unused --strict` を実行する
- **THEN** システムは `confirmed` の履歴だけを使用済みとして扱う

#### Scenario: JSON outputのwarningを分離する

- **WHEN** `--json` で履歴の一部をskipするwarningが発生する
- **THEN** stdoutは有効なJSONのまま、warning要約はstderrだけに出力され、既存の `--strict-input` 規則が適用される
