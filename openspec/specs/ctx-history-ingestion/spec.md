# ctx-history-ingestion Specification

## Purpose

ctxが保持する複数Agentの履歴を、内部DB schemaに依存せず、catsiftの利用統計へ取り込むための読み取り専用入力契約を定義する。

## Requirements

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

ctx sourceは、ctxが解決したdata rootの現在の履歴scopeに含まれる全Agentのイベントを入力へ含めなければならない（SHALL）。各イベントには取得できる範囲でcanonicalなAgentまたはprovider identity、provider session identity、ctx session identity、ctx event identity、およびtimestampを関連付けなければならない（SHALL）。異なるAgentの同名sessionを同一sessionとして扱ってはならない（MUST NOT）。

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

#### Scenario: ctxのdata rootを読み取れない

- **WHEN** ctxのdata rootが存在しない、読み取れない、またはctxが履歴を列挙できない
- **THEN** システムは対象pathまたはsourceを含むerrorをstderrへ出力し、非0の終了codeを返す

### Requirement: ctxのcomplete generation cacheを再利用する
ctx sourceは、公開されたイベント列挙が完了しimmutable generationが確定した場合だけ、そのgenerationの正規化済みcacheを有効にしなければならない（SHALL）。同じctx data root、同じgeneration、および同じparser versionではcacheを再利用できなければならず（SHALL）、異なるgenerationのcacheを現在の履歴として使用してはならない（MUST NOT）。

#### Scenario: complete generationをcacheへ保存する

- **WHEN** ctxの複数pageにわたるイベント列挙がcompletion recordまで正常に完了する
- **THEN** システムは全pageを含む正規化済みデータだけをgeneration cacheへ保存し、途中pageまでのデータをcomplete cacheとして公開しない

#### Scenario: 同じgenerationを再利用する

- **WHEN** userが同じctxのdata rootとimmutable generationに対して統計commandを再実行する
- **THEN** システムはgeneration cacheを再利用し、全pageをfreshに列挙した場合と同じAgent一覧、件数、row順序、およびJSON値を出力する

#### Scenario: generationが更新される

- **WHEN** ctxが前回cacheと異なるimmutable generationを返す
- **THEN** システムは旧generation cacheを現在の履歴へ混在させず、新しいgenerationを完全に列挙してcacheとreportを更新する

#### Scenario: 列挙が完了しない

- **WHEN** ctxのevent streamがcompletion recordなしでEOFまたは異常終了する
- **THEN** システムは不完全なevent列をcacheへ保存せず、既存のctx完全性規則に従ってsource errorまたは明確なwarningを報告する

### Requirement: ctx cacheへ期間filterを適用する
ctx sourceで有効なgeneration cacheを利用する場合、システムはcache済みeventのtimestampへ`--days N`のcutoffを適用しなければならない（SHALL）。期間filterによってctxのgeneration完全性を省略したり、pageの一部だけをcacheへ保存したりしてはならない（MUST NOT）。

#### Scenario: ctxの直近1日をcacheから集計する

- **WHEN** complete generation cacheが存在し、userが`catsift stats --source ctx --days 1`を実行する
- **THEN** システムはgeneration cache全体から直近24時間のeventだけを集計し、freshな全page列挙後にfilterした場合と同じ結果を出力する

#### Scenario: cacheがない状態でctxの期間指定する

- **WHEN** userがcacheのないctx sourceへ`--days 1`を指定する
- **THEN** システムは公開イベント列挙を完全に完了し、生成したgeneration cacheとreportへ同じcutoff規則を適用する
