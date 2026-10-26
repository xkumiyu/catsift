# Proposal

## Why

v0.6.0はCopilotの既知のmetadata eventとSkill invocation receiptを未知eventとして警告し、参照形式のSkill利用を集計できません。未知eventの統計への影響は未確認であり、警告をその事実に合わせます。

## What Changes

- 報告された12 event typeを明示的に受理し、`skill.invoked_ref`だけを既存のSkill正規化へ追加します。
- metadataからturn・prompt・Tool・token観測を生成せず、未知typeはskipと件数要約を継続します。
- 共通のunknown type警告adviceを「統計への影響は未確認」へ変更し、machine-readable warning形状を維持します。
- Copilot ParserVersionを更新し、旧cacheの警告と集計を再構築します。

## Capabilities

### New Capabilities

なし。

### Modified Capabilities

- `github-copilot-history-ingestion`: 既知eventの受理、参照形式のSkill evidence、未知eventの警告表現を明確化します。

## Impact

`internal/githubcopilot`、共通の`usage.WarningAdvice`とCLI/TUIの回帰テスト、Copilot parser cacheに影響します。依存追加・README変更・raw payload保存は不要です。
