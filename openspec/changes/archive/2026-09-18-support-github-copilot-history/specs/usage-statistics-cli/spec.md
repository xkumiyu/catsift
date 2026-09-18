## MODIFIED Requirements

### Requirement: 期間とhistory sourceを各統計commandで指定できる

`stats`、`tools`、`skills`は共通して`--source codex|ctx|opencode|copilot`、`--days`、および`--color auto|always|never`を受け付けなければならない（SHALL）。`--source`の既定値は`codex`でなければならず（SHALL）。同一実行でCodex、ctx、OpenCode、およびGitHub Copilotを複数同時に入力へ含めてはならない（MUST NOT）。filterとsource解決はすべての出力形式で同じ結果集合へ適用しなければならない（SHALL）。

#### Scenario: source未指定時はCodexを使用する

- **WHEN** userが `catsift stats` を実行する
- **THEN** システムは既存のCodex home解決規則に従ってCodexだけを入力sourceにする

#### Scenario: GitHub Copilot sourceを明示する

- **WHEN** userが `catsift stats --source copilot`、`catsift tools --source copilot`、または`catsift skills --source copilot`を実行する
- **THEN** システムはGitHub Copilot sourceだけを入力scopeとして使用し、他sourceの履歴を暗黙に合算しない

#### Scenario: Codexとctxを同時に指定する

- **WHEN** userが複数のhistory sourceを同時に指定しようとする
- **THEN** システムはsource選択を拒否し、Codex、ctx、OpenCode、またはGitHub Copilotを合算したreportを生成しない

#### Scenario: 30日分のSkill統計を取得する

- **WHEN** userが任意のsourceで `catsift skills --days 30` を実行する
- **THEN** システムは選択されたsourceのcutoff以後の観測だけからSkill統計を生成する

#### Scenario: GitHub Copilotと別sourceを同時に指定する

- **WHEN** userがGitHub CopilotとCodex、ctx、またはOpenCodeを同時に入力へ指定しようとする
- **THEN** システムはsource選択を拒否し、異なるsourceを合算したreportを生成しない

#### Scenario: 30日分のGitHub Copilot Skill統計を取得する

- **WHEN** userが `catsift skills --source copilot --days 30` を実行する
- **THEN** システムはGitHub Copilot eventのcutoff以後の観測だけからSkill統計を生成する

## ADDED Requirements

### Requirement: GitHub Copilot sourceをreportとJSONへ反映する

GitHub Copilot sourceを選択した各統計reportは、source display name、canonical Agent ID `copilot`、適用中のperiod、および取得できた集計値を既存のhuman-readable・JSON出力規則で表示しなければならない（SHALL）。履歴が空の場合も、既存のempty-stateと終了codeの規則を維持しなければならない（SHALL）。

#### Scenario: GitHub Copilotのhuman-readable reportを表示する

- **WHEN** userが `catsift stats --source copilot` を実行し、有効なCopilot eventが存在する
- **THEN** システムはGitHub Copilotのsource表示、`copilot`に対応するAgent表示、Period、およびSessions・Turns・User Prompts・Tool Callsなどの集計を出力する

#### Scenario: GitHub CopilotのJSONを表示する

- **WHEN** userが `catsift tools --source copilot --json` または別の統計commandへ`--json`を指定する
- **THEN** stdoutは単独の有効なJSON documentとなり、`source`、`agents`、既存の集計fieldを含み、prompt本文・tool argument・warningを含まない

#### Scenario: GitHub Copilotのtoken usageをJSONへ表示する

- **WHEN** Copilotのpersisted usageにcache read、cache write、またはreasoning tokenが存在し、userがtoken統計を含むreportをJSONで出力する
- **THEN** JSONは既存のtoken usage fieldへ正規化された値を含み、raw provider payloadを含まない

#### Scenario: GitHub Copilotのproject nameを表示する

- **WHEN** userがproject metadataを含むCopilot sessionで`catsift sessions --source copilot`またはSession detailを実行する
- **THEN** human-readable reportはproject nameを表示し、JSONは`project_name`と取得可能な`project_path`をsanitized metadataとして含む

#### Scenario: GitHub Copilotの履歴が空である

- **WHEN** userが `catsift stats --source copilot` を実行し、有効な利用eventが存在しない
- **THEN** システムはsource、Agent、Period、および0件のsummaryとempty-state messageを出力して0で終了する
