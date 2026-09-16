## Why

CatSiftはCodex、ctx、OpenCodeの利用履歴を集計できますが、GitHub Copilot CLIのsession履歴は対象外です。GitHub Copilot CLIはローカルにsession eventを保存しているため、既存の共通集計へ取り込めば、複数のcoding agentの利用状況を同じreportで確認できます。

## What Changes

- `copilot` を新しいhistory sourceとして追加する。
- GitHub Copilot CLIがローカルに保存したsession eventをread-onlyで発見・読取し、session、turn、user prompt、Tool、Skill、Model、およびtoken usageを共通Eventへ正規化する。
- Copilotの`session.shutdown.data.modelMetrics[*].usage`に保存されたinput、output、cache read/write、およびreasoning tokenを共通TokenUsageへ正規化する。
- session directoryの`workspace.yaml`から、保存されているsession nameをread-onlyで取得し、Session detailへ反映する。
- `skill.invoked` eventからSkill nameだけをread-onlyで取得し、既存のSkill統計へ反映する。Skill pathやcontentは保存しない。
- `session.context_changed` eventまたはsession directoryの`workspace.yaml`からrepositoryをproject nameとして取得し、repositoryがない場合は`gitRoot`または`cwd`の末尾をproject nameへfallbackする。既存のproject pathも保持する。
- Copilotの診断logやraw provider payloadを利用統計の入力・cache保存対象にせず、個別の不正または未知eventが有効な履歴全体を中断しないようにする。
- `stats`、`tools`、`skills` のsource選択、human-readable report、JSON、およびAgent表示で `copilot` を扱えるようにする。既存の既定sourceと既存利用者向けの挙動は変更しない。
- source revisionとparser versionを使ったcache再利用をCopilot sourceへ適用し、cacheには集計に必要なsanitized metadataだけを保存する。
- 実データを含まないsynthetic fixtureでparser、正規化、filter、cache、およびCLI出力を検証する。

## Capabilities

### New Capabilities

- `github-copilot-history-ingestion`: GitHub Copilot CLIのローカルsession eventを安全かつ再現可能に探索・読取する。

### Modified Capabilities

- `usage-event-normalization`: GitHub Copilotのsession eventを共通session、turn、prompt、Tool、Skill、Model、およびtoken usageへ正規化する。
- `usage-statistics-cli`: `copilot` sourceの選択と統計report・JSON表示を既存のsource規則へ追加する。
- `history-ingestion-cache`: GitHub Copilot sourceのscope、revision、およびparser versionをcacheの有効性判定へ追加する。

## Impact

- 新しいGitHub Copilot source adapter、synthetic fixture、およびpackage testを追加する。
- source selection、共通usage model、cache、CLI report/TUI入力、およびREADMEのsource説明を更新する。
- 外部依存は追加せず、ユーザーのlocal machine上のCopilot履歴をread-onlyで処理する。履歴内容はnetworkへ送信しない。
