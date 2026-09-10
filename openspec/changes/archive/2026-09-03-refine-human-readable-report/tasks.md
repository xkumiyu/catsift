## 1. Report headingとcontextの更新

- [x] 1.1 `internal/output`のhuman rendererを、command固有の`USAGE OVERVIEW`・`TOOL USAGE`・`SKILL USAGE` heading、グローバルな`CatSift`/実行command名を含まない構造へ変更し、`stats`のsummary前にもheadingを表示する。`ColorNever`のreport testで新headingが出力され、旧titleが出力されないことを確認する
- [x] 1.2 `Agent`、`Period`、`Layer`、`Group by`、`Strict`、`Skill grouping`を固定順のlabel付き改行contextとして描画し、human-readable Agent名を`Codex`とする。stats/tools/skillsそれぞれのplain report testでcontext行、順序、中点区切りの不在を確認する

## 2. Domain用語footerの導入

- [x] 2.1 Tool reportのfooterを対象Tool数と総Callsのdomain用語へ変更し、0・1・複数の件数に対して自然な単数・複数形を描画する。plain report testで`Rows`と旧中点区切りがなく、`tools`・`calls`の値が確認できることを検証する
- [x] 2.2 Skill reportのfooterを対象Skill数と総Usesのdomain用語へ変更し、0・1・複数の件数に対して自然な単数・複数形を描画する。plain report testで`Rows`と旧中点区切りがなく、`skills`・`uses`の値が確認できることを検証する

## 3. 控えめなsemantic styling

- [x] 3.1 `internal/output`のstyle helperをheadingの共通accent、table headerのbold、summary主要値のbold、context/footer/Last Usedの補助styleへ整理する。FailuresとSkillのevidence status値は通常色で表示し、通常rowへ一律のsuccess colorを付けないことを`ColorAlways`のreport testで確認する
- [x] 3.2 `ColorNever`、non-TTYの`auto`、`NO_COLOR`、`ColorAlways`の既存優先順位を維持し、plain reportとJSONにANSIが漏れないことを既存および追加testで確認する

## 4. 表示契約とドキュメントの更新

- [x] 4.1 `internal/output/report_test.go`と`cmd/catsift/main_test.go`の旧title/footer期待値を更新し、stats/tools/skillsの60・80・120 column、empty state、Unicode name、JSON field不変を検証する。`go test ./...`が成功することを確認する
- [x] 4.2 `README.md`と`README.ja.md`のhuman-readable report説明または出力例を新heading、改行context、domain footer、色の方針に合わせて更新し、JSON optionの説明が変わっていないことを確認する

## 5. 完了検証

- [x] 5.1 変更対象がhuman renderer・関連test・READMEに限定され、集計値・JSON schema・CLI optionに差分がないことをdiffで確認したうえで、`gofmt`、`go vet ./...`、`go test ./...`、`go build ./cmd/catsift`、`openspec validate refine-human-readable-report --strict`を実行してすべて成功することを確認する
