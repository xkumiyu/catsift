# unused-skill-report Specification

## Purpose

インストール済みSkillの現在のfilesystem snapshotと、選択したCodex履歴の利用集合を比較し、scopeとcanonical nameを確認できる未使用Skill reportを提供する。

## Requirements

### Requirement: 指定scopeからインストール済みSkillを検出する

`catsift skills --unused` は、既定では実行ユーザーの `~/.agents/skills` をscopeとして、指定された `--root` がある場合はそのrootだけをscopeとして、配下をrecursiveに探索しなければならない（SHALL）。認識対象は `.agents/skills` または `.codex/skills` のSkill root直下にあるSkill directory、および既知の `.codex/plugins/cache/.../skills` layoutにあるSkill directoryとし、任意の場所にある同名の `SKILL.md` をSkillとして扱ってはならない（MUST NOT）。`.codex/skills/.system` はscope directoryであり、その直下をSkill directoryとして扱わなければならない（SHALL）。

#### Scenario: 既定scopeのSkillを検出する

- **WHEN** `~/.agents/skills/report/SKILL.md` が存在し、`--root` が指定されていない
- **THEN** システムは `report` をインストール済みSkillとしてinventoryへ含める

#### Scenario: repository parentをrecursiveに探索する

- **WHEN** `--root ~/src` が指定され、`~/src/repo/.agents/skills/review/SKILL.md` と `~/src/repo/.codex/skills/build/SKILL.md` が存在する
- **THEN** システムは両方を検出し、`~/src` 配下の認識対象外の `SKILL.md` は検出結果へ含めない

#### Scenario: 複数rootと重複scopeを扱う

- **WHEN** `--root A --root B` が指定され、AとBの配下に同じSkill directoryが重複して見える
- **THEN** システムは重複する物理pathを1つのinventory entryとして扱い、未指定時の既定rootを暗黙に追加しない

#### Scenario: インストール済みSkillが存在しない

- **WHEN** 指定scopeに認識対象の `SKILL.md` が1つも存在しない
- **THEN** システムは成功して空のinventoryを生成し、未使用Skillのrowsも空arrayとする

### Requirement: Skillのcanonical nameと物理identityを保持する

各inventory entryは、読取可能で有効な `SKILL.md` frontmatterの `name` をcanonical nameとして優先しなければならず（SHALL）、frontmatterがない、無効である、または読取できない場合はSkill directoryのbasenameをcanonical nameへfallbackしなければならない（SHALL）。ただしplugin cache layoutでは、既存のSkill name解決規則に従ってplugin namespaceとdirectory basenameを組み合わせたcanonical nameを使用しなければならない（SHALL）。Reportはcanonical `name`、Skill directoryを指すabsolute `path`、nameの解決元、およびfrontmatter nameの最終componentとpathのbasenameの不一致を判別できる情報を保持しなければならない（SHALL）。同じcanonical nameを持つ異なる物理pathは、別々のinventory entryとして保持しなければならない（SHALL）。

#### Scenario: frontmatter nameをcanonical nameとして使う

- **WHEN** `dir-name/SKILL.md` のfrontmatterに `name: canonical-name` がある
- **THEN** entryの `name` は `canonical-name`、directory nameは `dir-name`、解決元は `frontmatter` として表現され、不一致を判別できる

#### Scenario: frontmatterが利用できない場合にfallbackする

- **WHEN** `fallback/SKILL.md` に有効な `name` がない、またはfileを読み取れない
- **THEN** entryの `name` は `fallback`、解決元は `directory` として表現される

#### Scenario: plugin namespaceをfallbackで維持する

- **WHEN** plugin cacheの `data-analytics/1.0.0/skills/router/SKILL.md` に有効なfrontmatter nameがない
- **THEN** entryの `name` は `data-analytics:router`、directory nameは `router`、解決元は `directory` として表現される

#### Scenario: 同名Skillを異なるpathから検出する

- **WHEN** 2つのrepositoryにそれぞれ `shared/SKILL.md` があり、どちらも同じcanonical nameを持つ
- **THEN** システムは各absolute pathを持つ2つのentryを生成し、片方を黙って統合しない

### Requirement: 履歴の利用集合とinventoryを比較する

未使用判定は、選択されたCodexまたはctx history sourceのSkill利用検出・turn単位の重複排除・canonical name解決を通過した履歴を対象にし、inventory entryのcanonical nameと履歴のSkill nameが完全一致する利用が選択条件内に1回もない場合だけ、そのentryを未使用としなければならない（SHALL）。ctx sourceで複数Agentが対象になる場合は、全Agentの利用集合をunionして判定しなければならない（SHALL）。`--days` は履歴の対象期間を制限し、既定では選択sourceから読み込める履歴全体を対象としなければならない（SHALL）。`--strict` が指定された場合は `confirmed` の利用だけを使用済み判定へ含めなければならない（SHALL）。`--group-by` の値は、少なくとも1回利用されたかどうかという未使用判定を変えてはならない（MUST NOT）。nameのcase変換やdirectory nameをcanonical nameの別名として暗黙に統合してはならない（MUST NOT）。

#### Scenario: 選択期間内に利用されたSkillを除外する

- **WHEN** `report` が選択期間内に1回利用され、`unused` が同じscopeにインストールされている
- **THEN** `report` は未使用rowsへ含まれない

#### Scenario: ctxの複数Agentの利用をunionする

- **WHEN** ctxのCodexまたはOpenCodeのいずれかが `report` を選択期間内に利用し、inventoryに同名entryがある
- **THEN** `report` は全Agent共通の未使用rowsへ含まれない

#### Scenario: 期間外の利用だけがあるSkillを列挙する

- **WHEN** `report` の利用が `--days 30` のcutoffより前にだけ存在する
- **THEN** `report` は直近30日間の未使用Skillとして列挙される

#### Scenario: strict modeでinferred usageを除外する

- **WHEN** `review` に `inferred` の利用だけが選択期間内に存在し、`--strict` が指定されている
- **THEN** `review` は未使用rowsへ含まれる

#### Scenario: canonical nameで利用済み判定する

- **WHEN** directory nameが `dir-name` でfrontmatter nameが `canonical-name` のSkillに、履歴上 `canonical-name` の利用がある
- **THEN** entryは未使用として列挙されない

### Requirement: 未使用Skillを決定的なhuman-readable reportとJSONで出力する

未使用viewは、inventory全体ではなく未使用entryだけをrowsへ出力しなければならない（SHALL）。human-readable reportは未使用viewであること、選択history source、Agent一覧、選択期間、strict状態、およびscopeが分かるcontextを表示し、各rowのSkill nameとpathを表示しなければならない（SHALL）。同じcanonical nameを持つ異なる物理pathは別rowとして表示しなければならない（SHALL）。JSONは既存のreport contextに加えて `view: "unused"`、source、Agent一覧、解決済みroot一覧、inventory総数、未使用総数、および `rows` arrayを持たなければならない（SHALL）。各JSON rowは少なくとも `name`、Skill directoryを示すabsolute `path`、`name_source`、および `name_mismatch` を保持しなければならない（SHALL）。rowsはcanonical name昇順、同名の場合はabsolute path昇順で並べなければならない（SHALL）。inventoryと未使用件数は論理Skill名ではなく物理entry数として数えなければならない（SHALL）。

#### Scenario: 未使用Skillをhuman-readableで表示する

- **WHEN** `catsift skills --unused` をhuman-readable modeで実行し、未使用Skillが存在する
- **THEN** システムは未使用view、source、Agent一覧、期間、strict状態、scope、Skill name、およびpathを含むstatic reportを出力する

#### Scenario: ctxで同名・異PATHのSkillを表示する

- **WHEN** ctx sourceを選択し、同じcanonical nameの未使用Skillが2つの異なるabsolute pathに存在する
- **THEN** システムは同じnameを持つ2つのrowをpath順に出力し、未使用件数を2として扱う

#### Scenario: 未使用SkillをJSONで表示する

- **WHEN** userが `catsift skills --unused --json` を実行する
- **THEN** stdoutは単独の有効なJSON documentとなり、`view` が `unused`、`rows` は未使用entryだけ、空の場合も `[]` となる

#### Scenario: すべて使用済みの場合にempty stateを表示する

- **WHEN** scope内の全inventory entryに選択条件内の利用が存在する
- **THEN** human-readable reportは空tableではなく、source・Agent・scope・履歴filterに未使用Skillがないことを説明し、JSONは `installed_count` を保持したまま `unused_count: 0` と `rows: []` を出力する

#### Scenario: 出力順をfilesystem列挙順から独立させる

- **WHEN** 同一内容のSkillが異なるfilesystem列挙順で発見される
- **THEN** human-readable reportとJSONのrows順、件数、および各identityは一致する

### Requirement: unused viewが既存の履歴filterとoutput optionを継承する

`skills --unused` は `--source codex|ctx`、`--days`、`--strict`、`--group-by`、`--color`、`--json`、`--verbose`、および `--strict-input` を通常の `skills` commandと同じvalidation、履歴source、warning、終了codeの規則で受け付けなければならない（SHALL）。`--source` の既定値は `codex` でなければならず（SHALL）。source固有でない `--root` はSkill inventoryのscopeだけを変更し、履歴sourceやAgent一覧を暗黙に変更してはならない（MUST NOT）。

#### Scenario: days filterをunused判定へ適用する

- **WHEN** userが `catsift skills --source ctx --unused --days 30` を実行する
- **THEN** システムはctxの直近30日間の履歴だけを使用済み判定へ使い、report contextにもその期間を示す

#### Scenario: strict filterをunused判定へ適用する

- **WHEN** userが `catsift skills --source ctx --unused --strict` を実行する
- **THEN** システムはctx履歴の `confirmed` 利用だけを使用済みとして扱う

#### Scenario: JSON outputのwarningを分離する

- **WHEN** `--json` で履歴の一部をskipするwarningが発生する
- **THEN** stdoutは有効なJSONのまま、warning要約はstderrだけに出力され、既存の `--strict-input` 規則が適用される

### Requirement: filesystemをread-onlyかつscope内で安全に扱う

Skill inventoryの生成は指定scope外のpathを探索してはならず（MUST NOT）、Skillのcommandを実行したり `SKILL.md` の本文をreportへ出力したりしてはならない（MUST NOT）。既定rootが存在しない場合は空scopeとして扱い成功できなければならない（SHALL）。明示されたrootが存在しない、directoryでない、またはroot自体を読み取れない場合は、簡潔なerrorをstderrへ出力して非0で終了しなければならない（SHALL）。

#### Scenario: report生成でfilesystemを変更しない

- **WHEN** userが任意のscopeに対して `--unused` reportを実行する
- **THEN** システムはSkill fileやdirectoryを作成、変更、削除せず、commandも実行しない

#### Scenario: 明示rootのerrorを報告する

- **WHEN** userが存在しないpathを `--root` に指定する
- **THEN** システムはstdoutへ部分的なreportを出力せず、stderrへerrorを出力して非0で終了する
