## Why

現在のhuman-readable reportは、`CatSift · CODEX`、`·`区切り、`Rows`など、集計結果そのものより実装・ブランディング寄りの表現を含んでいる。ToolとSkillの利用状況を日常的に確認する出力として、何のレポートか、どの条件で集計したか、どの値に注目すべきかをより自然に読み取れる表示へ整える。

## What Changes

- グローバルな`CatSift · CODEX`形式のタイトルを廃止し、`USAGE OVERVIEW`、`TOOL USAGE`、`SKILL USAGE`のような内容指向のreport headingを使用する。
- Agent、Period、Layer、Group by、Strictなどのcontextを`·`で連結せず、ラベル付きの行として表示する。Agentはhuman-readable reportでは`Codex`と表示し、JSONの`agent` fieldは変更しない。
- `Rows`を廃止し、`3 tools, 879 calls total`および`12 skills, 87 uses total`のように、利用者が意味を理解できるdomain用語のfooterへ変更する。対象件数と合計値は維持する。
- human-readable reportの色を、headingとtableの階層を補助する範囲に限定する。tableのlabel・row・count・Failures・Skillのevidence statusは既定色を保ち、色がなくても意味が伝わる構造を維持する。
- `--color auto|always|never`、`NO_COLOR`、TTY判定、terminal幅対応、JSONの無装飾出力、集計結果とJSON schemaは変更しない。

## Capabilities

### New Capabilities

なし。

### Modified Capabilities

- `usage-statistics-cli`: human-readable reportのheading、context表示、footer文言、および補助的なcolor stylingの要件を変更する。

## Impact

- `internal/output`のhuman renderer、style helper、context/footer formatterを変更する。
- `internal/output/report_test.go`およびCLIのreport/golden testを新しい表示契約に合わせて更新・追加する。
- `openspec/specs/usage-statistics-cli/spec.md`の既存のhuman-readable report要件へdeltaを追加する。
- JSON renderer、集計domain model、CLI option/API、履歴データ、外部依存は変更しない。
