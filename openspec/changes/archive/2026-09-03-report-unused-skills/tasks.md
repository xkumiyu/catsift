## 1. Skill inventoryのdomainとscopeを実装する

- [x] 1.1 `internal/skillinventory` にinventory entry/snapshotのdata modelと、`.agents/skills`・`.codex/skills`・`.system`・plugin cacheだけを認識するstrict layout classifierを追加し、既知layoutと任意の `skills/<name>/SKILL.md` を区別するtable-driven testを通す
- [x] 1.2 defaultの `~/.agents/skills` とrepeatableなexplicit `--root` のpath解決、absolute/clean/deduplicate処理、missing default root、invalid explicit rootのerror境界を実装し、root解決とvalidationのunit testを通す
- [x] 1.3 `filepath.WalkDir` によるrecursive discovery、candidateの物理path deduplication、symlink directoryの非追跡、子directory read errorのwarning化を実装し、default root・repository parent・複数root・overlap・empty scope・read-onlyのtemporary-directory testを通す

## 2. canonical nameと履歴比較を実装する

- [x] 2.1 既存のSkill frontmatter resolverをinventoryと履歴検出で共有できる形へ整理し、128 KiB bounded read、valid `name` の優先、directory fallback、plugin namespace、name mismatch metadataを実装して既存の `internal/usage` testと新しいfrontmatter testを通す
- [x] 2.2 `internal/aggregate` にinventory entryと既存 `SkillsBy` のused-name setを比較するunused判定を追加し、turn/session groupingに依存しないmembership、`--days`、`--strict`、canonical exact match、同名別pathのtestを通す
- [x] 2.3 aggregate reportへunused entry、installed count、resolved rootsを通常のSkill usage rowと分離して保持し、`skills` の既存集計結果とJSON dataが変更されない回帰testを通す

## 3. unused viewのoutputを実装する

- [x] 3.1 `ReportContext` とJSON rendererへunused view discriminatorを追加し、`view: "unused"`、roots、installed/unused count、`name`・`path`・`name_source`・`name_mismatch` rowを決定的に出力するtestを通す
- [x] 3.2 human-readable rendererへ `UNUSED SKILLS` section、period/strict/scope context、name/path/mismatch表示、inventoryなしと全使用済みのempty state、narrow terminal layoutを追加し、固定width・non-TTY・`NO_COLOR`・color modeのgoldenまたはtable-driven testを通す
- [x] 3.3 unused JSONでANSIやwarningがstdoutへ混入しないよう既存warning経路とrenderer分離を確認し、warning発生時の有効JSON・stderr要約・`--strict-input` の終了codeをintegration testで通す

## 4. CLIへ安全に接続する

- [x] 4.1 `cmd/catsift` のskills helpとflag parserへ `--unused` とrepeatable `--root` を追加し、root単独、他commandでのunused、既存optionとのvalidation errorをCLI testで確認する
- [x] 4.2 `skills --unused` のhistory load、inventory discovery、strict used-name比較、report contextへのroots設定、root errorとwarningの終了codeを既存pipelineへ接続し、fixtureを使ったhuman/JSON end-to-end testを通す
- [x] 4.3 `--unused` なしの `skills`、`stats`、`tools` の既存report、row順、JSON shape、Codex home解決を変更していないことを全CLI regression testで確認する

## 5. 利用者向けdocumentationを更新する

- [x] 5.1 `README.md` と `README.ja.md` のUsage/Tipsへ `catsift skills --unused`、既定scope、repeatable `--root`、repository parentを指定する例、frontmatter nameとstrict/daysの意味を追加し、旧shell比較例と矛盾しない説明を `rg` で確認する

## 6. 完了検証を実施する

- [x] 6.1 変更対象をgofmtし、`go test ./...`、`go vet ./...`、`go build ./cmd/catsift` を実行して全test・静的検査・buildが成功することを確認する
- [x] 6.2 fixtureと一時rootでdefault scope、all-repository scope、frontmatter mismatch、plugin namespace、strict/days、empty stateを実際のbinaryから確認し、stdout JSONのparse成功とfilesystem未変更を確認する
