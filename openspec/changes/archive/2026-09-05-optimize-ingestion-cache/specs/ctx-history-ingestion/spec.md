## ADDED Requirements

### Requirement: ctxのcomplete generation cacheを再利用する

ctx sourceは、公開されたイベント列挙が完了しimmutable generationが確定した場合だけ、そのgenerationの正規化済みcacheを有効にしなければならない（SHALL）。同じctx data root、同じgeneration、および同じparser versionではcacheを再利用できなければならず（SHALL）、異なるgenerationのcacheを現在の履歴として使用してはならない（MUST NOT）。

#### Scenario: complete generationをcacheへ保存する

- **WHEN** ctxの複数pageにわたるイベント列挙がcompletion recordまで正常に完了する
- **THEN** システムは全pageを含む正規化済みデータだけをgeneration cacheへ保存し、途中pageまでのデータをcomplete cacheとして公開しない

#### Scenario: 同じgenerationを再利用する

- **WHEN** userが同じctx data rootとimmutable generationに対して統計commandを再実行する
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
