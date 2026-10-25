# Proposal

## Why

人向けの対話探索をTUI、機械向けのreportをJSONに統一し、出力形式の選択を不要にします。TUIでもインストール済みSkillと選択履歴の差分を確認できるようにします。

## What Changes

- **BREAKING**: v0.6.0向けに全report subcommandとdetailの既定出力をJSONへ変更し、static human report modeを廃止します。
- `--json`は互換flagとして残し、明示指定（falseを含む）時のみstderrに不要であることとv0.7.0での削除予定を警告します。
- 引数なしのinteractive TUIを維持し、Skillsでusage/unusedを切り替えます。
- inventoryの名前・path・scan roots、選択期間/source、warningを安全に表示します。

## Capabilities

### New Capabilities

なし。

### Modified Capabilities

- `usage-statistics-cli`: JSON既定とflag移行。
- `tui-usage-explorer`: Skillsのunused切替とfilter連動。
- `unused-skill-report`: static reportをJSON/TUIへ移行。

## Impact

CLI出力分岐、query read model、TUI、回帰テスト、両言語READMEを変更します。配布Skill sourceの更新はPR #17で別途対応します。依存追加・version変更・履歴書換えは行いません。
