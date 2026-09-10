## Purpose

ctxが保持する複数Agentの履歴を、内部DB schemaに依存せず、catsiftの利用統計へ取り込むための読み取り専用入力契約を定義する。

## ADDED Requirements

### Requirement: ctx履歴を公開された読み取り専用イベント列挙から取得する

`ctx` sourceが選択された場合、システムはctxが提供する公開された読み取り専用の履歴イベント列挙を入力として使用しなければならない（SHALL）。ctxの `usage.sqlite`、検索インデックス、またはその他の内部schemaを統計入力として直接参照してはならず（MUST NOT）、ctxまたはprovider-owned履歴を変更してはならない（MUST NOT）。

#### Scenario: ctx sourceを選択する

- **WHEN** userが統計commandへ `--source ctx` を指定する
- **THEN** システムはctxの読み取り専用イベント列挙から入力を取得し、Codex home配下のJSONLを入力へ含めない

#### Scenario: ctxの集計用SQLiteだけが存在する

- **WHEN** ctxの `usage.sqlite` にctx自身の集計行が存在するが、履歴イベント列挙を利用できない
- **THEN** システムは集計行をsession・prompt・Tool・Skillの履歴として解釈せず、入力source errorとして非0で終了する

#### Scenario: ctx入力を実行する

- **WHEN** userがctx sourceで任意の統計commandを実行する
- **THEN** ctxのstorage、provider-owned履歴、および外部networkの状態は実行前後で変化しない

### Requirement: ctxに含まれる複数Agentを履歴scopeへ含める

ctx sourceは、選択されたctx data rootの現在の履歴scopeに含まれる全Agentのイベントを入力へ含めなければならない（SHALL）。各イベントには取得できる範囲でcanonicalなAgentまたはprovider identity、provider session identity、ctx session identity、ctx event identity、およびtimestampを関連付けなければならない（SHALL）。異なるAgentの同名sessionを同一sessionとして扱ってはならない（MUST NOT）。

#### Scenario: CodexとOpenCodeの履歴がctxに存在する

- **WHEN** ctxの選択scopeにCodexとOpenCodeの履歴が存在する
- **THEN** システムは両Agentのイベントを入力へ含め、report用のAgent一覧にCodexとOpenCodeを含める

#### Scenario: ctxの履歴scopeが空である

- **WHEN** ctx sourceは利用可能だが選択scopeに利用統計へ変換可能なイベントが存在しない
- **THEN** システムは空入力として正常終了し、Agent一覧と各集計値は空または0として表現する

### Requirement: ctxイベント列挙の完全性と決定性を維持する

システムはctxが返す同一のimmutable generation、同一filter、および同一基準時刻に対して同一の入力結果を生成しなければならない（SHALL）。継続cursorまたは複数pageが返される場合は、完了を確認するまで全pageを処理しなければならず（SHALL）、途中で切り捨てられた結果を完全な履歴として集計してはならない（MUST NOT）。

#### Scenario: ctxイベントが複数pageに分割される

- **WHEN** ctxが履歴イベントを複数pageと継続cursorで返す
- **THEN** システムは全pageを順序どおりに処理し、全イベントを集計対象へ含める

#### Scenario: ctxイベント列挙が完了recordなしで終了する

- **WHEN** ctxのイベントstreamがcompletionを示さずにEOFまたは異常終了する
- **THEN** システムは不完全な入力を成功した完全履歴として出力せず、source errorまたは明確なwarningを報告する

### Requirement: ctxの部分的な履歴問題から回復する

システムはctxの個別イベントが未知、破損、または現在の正規化対象外である場合、他の変換可能なイベントの処理を継続しなければならない（SHALL）。skip理由と件数はstderrのwarningへ出力し、`--strict-input` が指定された場合だけそのwarningを非0終了条件へできなければならない（SHALL）。ctx data rootの解決失敗、イベント列挙の開始失敗、または完全性を検証できないsource-level failureはerrorとして非0で終了しなければならない（SHALL）。

#### Scenario: 未知のprovider event typeがある

- **WHEN** ctx event列挙に未知のevent typeが含まれる
- **THEN** システムはそのeventをwarning対象としてskipし、既知のeventからreportを生成する

#### Scenario: ctx data rootを読み取れない

- **WHEN** userが指定したctx data rootが存在しない、読み取れない、またはctxが履歴を列挙できない
- **THEN** システムは対象pathまたはsourceを含むerrorをstderrへ出力し、非0の終了codeを返す
