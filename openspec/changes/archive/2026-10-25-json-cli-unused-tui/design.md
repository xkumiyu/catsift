# Design

## Context

proposal.mdを参照。JSON writerとskillinventory.Discover、aggregate.BuildUnusedReportは既に存在します。

## Goals / Non-Goals

既存の集計・privacy境界を保って出力経路を統一します。version更新、新依存、履歴編集は対象外です。

## Decisions

- CLIのhuman分岐を削除し既存JSON writerを無条件で呼びます。互換boolean flagは値でなく明示指定を検出します。falseでもJSONです。TUIは値にかかわらずJSON flagを拒否します。helpは通常どおりtextです。
- color/viewは既存引数validationを維持する互換optionとして受け付け、JSON形式は変えません。
- inventoryをCLIで初回とreload時にDiscoverし、安全なmetadata snapshotをquery.Inputへ渡します。TUIにfilesystem探索実装を追加しません。
- query.Buildで既存filterを通ったTurnをBuildUnusedReportへ渡します。row検索は判定へ影響させません。strictとrootをTUI起動でも指定できるようにします。
- TUIは`u`で切替、unused rowはname/pathを表示します。観測のないrowから履歴detailを開く動作は提供しません。

## Risks / Trade-offs

- 広い明示rootは探索コストが増えます → 既存scope規則を維持しreloadでだけ再探索します。
- 履歴自体が不完全な場合unusedを断定できません → 未観測と明示しwarningを表示します。

## Migration Plan

v0.6.0でJSON既定へ移行し、`--json`はv0.7.0削除予定の警告を出します。現versionは変更しません。両READMEを更新します。配布Skill sourceの更新はPR #17で別途対応します。
