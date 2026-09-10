## Why

現在の `catsift` はCodexのsession JSONLだけを入力源としており、複数のAgentの履歴をctxで一元管理している環境を直接集計できない。ctxの内部DBへ依存せず、ctxが提供する読み取り専用のイベント出力を入力源として選択できれば、Codexの既存利用を保ったまま複数Agentの利用状況を同じreportで確認できる。

## What Changes

- 統計commandの入力源として `codex` または `ctx` を選択できるようにする。未指定時は従来どおり `codex` とし、1回の実行で複数sourceを合算しない。
- `codex` sourceは既存のCodex home配下のJSONL readerを継続利用する。
- `ctx` sourceはctxの公開された読み取り専用イベント出力を利用し、ctx内部の `usage.sqlite` や検索インデックスの内部schemaを直接参照しない。
- ctxに含まれる複数のAgentを検出し、human-readable reportに入力sourceとAgent一覧を表示する。
- `Sessions`、`User Prompts`、`Tool Calls`、`Skill Uses` はctxで選択されたAgent全体を合算する。通常のTool・Skill行はcanonical name単位で合算する。
- ctxイベントを既存のsession・prompt・Tool・Skillの共通正規化モデルへ変換し、取得できない情報や部分的な変換はwarningとして扱う。
- `skills --unused` は物理的なSkill directoryをrow identityとして維持し、同じcanonical nameでも異なるPATHは別rowとして出力する。使用済み判定は既存どおりcanonical name単位とする。
- source固有のpath、data-root、filter、入力診断、read-only規則をCLI help・READMEへ反映し、JSONのsource/agents契約は維持する。

## Capabilities

### New Capabilities

- `ctx-history-ingestion`: ctxの読み取り専用イベント出力を取得し、複数Agentを含む履歴を共通の利用Eventへ変換する。

### Modified Capabilities

- `usage-statistics-cli`: 入力sourceの排他的選択、ctx内の複数Agent表示、Agent横断の合算、およびsource-awareなhuman-readable/JSON reportを追加する。
- `usage-event-normalization`: Codex以外のctx由来イベントを既存のsession・prompt・Tool・Skill正規化へ接続し、Agent identityとsource identityを保持する。
- `unused-skill-report`: 選択されたCodexまたはctx履歴を使用済み判定へ利用し、同名・異PATHの物理Skill entryを個別rowとして扱う契約を明確化する。

## Impact

- CLIのsource解決と入力adapter、共通normalized model、aggregate metadata、human-readable/JSON renderer、warning処理が影響を受ける。
- 新たなctx CLI依存またはctx event contractの検証が必要になる。ctx内部DBのschemaやprovider-owned履歴を直接扱う依存は追加しない。
- 既存のCodex-only invocationはデフォルトsourceと既存の集計意味を維持する。ただし複数Agentを表現するためのreport contextおよびmachine-readable contractには追加またはversioningが必要になる。
- README.mdとREADME.ja.md、関連するOpenSpec specおよびsynthetic fixture/testが影響を受ける。実ユーザー履歴はtest dataへ保存しない。
