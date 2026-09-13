# history-ingestion-cache Specification

## Purpose

Codexとctxの履歴を安全に再利用し、同じ入力に対する複数の統計commandや短い期間のreportを、履歴全体の再解析なしに生成できるようにする。

## Requirements

### Requirement: 正規化済み履歴を永続cacheへ保存して再利用する
システムは、Codex、ctx、またはOpenCodeの入力を完全に読み込み、利用統計へ必要な正規化を完了した場合、report生成に必要な最小限の正規化済みデータをユーザーのcache directoryへ永続保存しなければならない（SHALL）。同じsource scopeと有効なcacheがある場合、システムはそのcacheを再利用し、freshな履歴読取と同じ結果を生成しなければならない（SHALL）。cacheは履歴source自体の一部として扱ってはならない（MUST NOT）。

#### Scenario: 初回の履歴読取をcacheへ保存する

- **WHEN** userがcacheのないCodex、ctx、またはOpenCode sourceで統計commandを実行し、履歴の読取と正規化が完全に成功する
- **THEN** システムは不完全な入力をcacheへ保存せず、report生成に必要な正規化済みデータをcacheへ保存し、入力sourceと同じreportをstdoutへ出力する

#### Scenario: 有効なcacheを再利用する

- **WHEN** userが同じsource scope、同じsource revision、および同じparser versionで統計commandを再実行する
- **THEN** システムは有効なcacheを利用し、freshな履歴読取時と同じ件数、row順序、timestamp、およびJSON値を出力する

#### Scenario: cache済み履歴へ期間filterを適用する

- **WHEN** userが有効な全期間cacheに対して`--days 1`または別の正の`--days N`を指定する
- **THEN** システムはcache済み観測のtimestampへ既存の期間filterを適用し、履歴全体を再取得・再解析した場合と同じreportを生成する

### Requirement: source revisionとparser versionでcacheの有効性を判定する
システムはcacheをsource type、source固有のscope、source revision、およびparser versionの組み合わせと関連付けなければならない（SHALL）。source revisionが一致しないcache、別source scopeのcache、または現在のparserで生成されていないcacheを現在の入力として使用してはならない（MUST NOT）。OpenCode sourceでは、選択されたdata rootとその永続履歴のrevisionをscopeおよびrevisionの判定へ含めなければならない（SHALL）。

#### Scenario: parser versionが変わる

- **WHEN** cache作成後に正規化schemaまたはparser versionが変わった状態で統計commandを実行する
- **THEN** システムは旧cacheを現在の結果へ使用せず、履歴sourceから再構築する

#### Scenario: source revisionが変わる

- **WHEN** cache作成後にCodex履歴file、ctx履歴generation、またはOpenCode永続履歴のsource revisionが変わる
- **THEN** システムは変更前のcacheをそのまま現在の完全な履歴として扱わず、変更を反映したcacheを再構築する

#### Scenario: OpenCode data rootが変わる

- **WHEN** userが別のOpenCode data rootを指定して統計commandを実行する
- **THEN** システムは元のdata rootのcacheを使用せず、指定されたdata rootのrevisionに対応するcacheだけを使用する

### Requirement: cacheの失敗を安全に扱う
システムはcacheの欠落、破損、未完了、または書込中断によって統計生成を誤った成功結果にしてはならない（MUST NOT）。無効なcacheは無視してsourceから再構築できなければならず（SHALL）、cacheの診断やwarningはmachine-readableなstdoutへ混入させてはならない（MUST NOT）。cacheの更新は既存の有効なcacheを中途半端な内容で置換してはならない（MUST NOT）。

#### Scenario: 破損したcacheを検出する

- **WHEN** cache fileが破損している、schemaが不正である、または必要な完了情報を欠いている
- **THEN** システムはそのcacheを使用せず、sourceから入力を再構築し、sourceが有効ならreportを生成する

#### Scenario: cache書込が中断される

- **WHEN** cacheの更新が完了前に中断される
- **THEN** システムは未完了cacheを有効cacheとして公開せず、次回実行時に既存の有効cacheを使用するかsourceから再構築する

### Requirement: cacheへ保存する履歴内容を最小化する
システムはreportの再生成に必要な正規化済みfieldだけをcacheへ保存し、raw prompt本文、tool引数、および不要なraw provider payloadを保存してはならない（MUST NOT）。cacheはユーザーのlocal machine内だけで扱い、履歴内容をnetworkへ送信してはならない（MUST NOT）。この規則はCodex、ctx、およびOpenCodeのcacheへ同じように適用しなければならない（SHALL）。

#### Scenario: cache内容を生成する

- **WHEN** システムが履歴sourceからcacheを作成する
- **THEN** cacheにはsession・turn・Tool・Skillの集計とfilterに必要な正規化済み情報だけが含まれ、raw prompt本文とtool引数は含まれない

#### Scenario: cacheを利用してreportを生成する

- **WHEN** userが有効なcacheを利用する統計commandを実行する
- **THEN** システムはcacheまたは履歴内容を外部networkへ送信せず、Codex home、ctx data root、OpenCode data root、およびprovider-owned履歴を変更しない
