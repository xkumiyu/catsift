## Why

Codex のローカル履歴から「どの Tool と Skill が実際に使われたか」を軽量に把握できる CLI が不足している。特に Codex は Skill 利用を専用の `skill_call` として保存しないため、ログ上の複数の証拠を区別して正規化し、検出根拠が分かる形で集計する必要がある。

## What Changes

- Codex のローカル session JSONL を読み取り、session・user prompt・Tool実行・Skill利用を抽出する。
- model が選択した外側の Tool call と、実際に完了した runtime action を区別し、通常の Tool統計では runtime action を優先する。
- 注入された `<skill>`、structured Skill tool、`SKILL.md` または Skill配下scriptへのアクセスから Skill利用を検出し、明示利用と推定利用を区別する。
- `catsift stats`、`catsift tools`、`catsift skills` を提供する。
- 期間filterと、人が集計条件・主要数値・内訳を把握しやすいstatic report・JSON出力を提供し、Skill統計には確認可能な利用証拠だけを対象にする strict mode を提供する。
- human-readable reportはterminal capabilityに応じて見出し、整列、色、compact layoutを使い、redirect時やcolor無効時も装飾なしで読みやすく表示する。
- source record、検出方式、観測layerを保持する共通Eventへ正規化し、未知または破損したrecordを安全に扱う。
- MVPは Codex のみに対応し、OpenCode、他Agent、token/cost集計、DB、interactive TUI、Web UIは対象外とする。

## Capabilities

### New Capabilities

- `codex-history-ingestion`: Codex historyの探索、streaming読取、期間filter、schema差異と破損recordの処理を定義する。
- `usage-event-normalization`: session、prompt、Tool、Skillの観測結果を、layer・検出根拠・重複排除規則を含む共通Eventへ変換する。
- `usage-statistics-cli`: stats・tools・skillsコマンド、filter、human-readable static report・JSON出力、およびCLIの失敗時動作を定義する。

### Modified Capabilities

なし。

## Impact

- 新規の Go module、CLI entry point、Codex adapter、正規化・集計・出力package、test fixtureを追加する。
- human-readable reportの描画とterminal capability検出のために、非interactiveなGo library依存を追加する。
- 読取対象は既定で Codex home配下のローカルsession履歴とし、Agentの履歴や設定は変更しない。
- 外部network serviceや永続DBへの依存は追加しない。
