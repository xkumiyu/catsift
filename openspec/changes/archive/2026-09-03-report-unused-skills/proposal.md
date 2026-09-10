## Why

現在の `catsift skills` は履歴に記録されたSkill利用を集計するcommandであり、インストール済みだが利用されていないSkillを直接判定できない。そのため、利用者はSkill directoryを別途探索し、履歴のJSONとshell commandで比較する必要がある。`SKILL.md` のcanonical `name`、複数のSkill root、および `--days`・`--strict` の組み合わせを安全に扱える、opt-inの `--unused` reportを提供する。

## What Changes

- `catsift skills --unused` を追加し、指定scopeにあるインストール済みSkillのうち、選択した履歴条件で利用記録がないSkillだけを表示する。
- `--root` を任意かつrepeatableなscope指定として追加する。未指定時は既定のuser Skill rootを使い、明示指定時は指定rootだけを探索する。
- Skillの比較名は、読取可能で有効な `SKILL.md` frontmatterの `name` を優先し、利用できない場合はdirectory名へfallbackする。directory path、解決元、およびnameとdirectoryの不一致を判別できる情報をreportへ保持する。
- `--days`、`--strict`、`--codex-home`、`--json` など既存のfilter・source・output optionを `--unused` reportにも適用する。
- human-readable reportとJSONで決定的な並び順と空結果の扱いを定義する。
- `catsift skills` の既定動作は変更しない。今回のscopeには、履歴を参照しない独立した `--inventory` や全インストールSkill一覧commandを含めない。

## Capabilities

### New Capabilities

- `unused-skill-report`: インストール済みSkillの探索、canonical nameの解決、Skill利用履歴との比較、および未使用Skillreportの出力を定義する。

### Modified Capabilities

- `usage-statistics-cli`: 既存の `skills` commandへ `--unused` と `--root` を追加し、履歴filterを使った未使用Skillreportを提供する。

## Impact

- `cmd/catsift` のargument parsing、`skills` report dispatch、およびhuman-readable/JSON outputへ影響する。
- 現在のSkill利用集計と履歴sourceを再利用しつつ、filesystem上のSkill rootを探索するinventory処理とcanonical nameの比較処理を追加する。
- `--unused` 用のCLI・filesystem探索・frontmatter解決・履歴filter・出力形式のtestを追加する。新しい外部dependencyは導入しない。
- 既存の `catsift skills`、他の統計command、および既存のSkill利用名解決の挙動にはbreaking changeを入れない。
