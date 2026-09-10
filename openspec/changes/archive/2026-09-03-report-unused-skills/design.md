## Context

proposal.mdの動機と、`unused-skill-report` および `usage-statistics-cli` の外部契約を実現する設計である。現在のCLIは `cmd/catsift` でoptionを解析し、`codex.Load` で履歴を読み、`internal/aggregate` でSkill利用を集計し、`internal/output` でhuman-readable reportまたはJSONを描画する。通常の `skills` viewは利用履歴のrowだけを返すため、filesystem上のSkill identityを表すdata modelと別viewが必要になる。

既存の `internal/usage` には、履歴からSkill evidenceを検出する処理、既知pathからのdirectory fallback、および `SKILL.md` frontmatter nameの解決がある。履歴側の名前解決と、現在のfilesystemからinventoryを作る処理を別々に実装するとfrontmatterやplugin namespaceの規則がずれるため、共有できるresolverは再利用する。一方、履歴のpath classifierには古い履歴を復元するためのdirectory fallbackが含まれるので、それをそのままinventoryの発見条件には使わず、inventory専用の厳格なlayout判定を置く。

## Goals / Non-Goals

**Goals:**

- 既存の `skills` command内に、`--unused` で選択する追加viewを実装できる境界を作る。
- `~/.agents/skills` または明示された複数のrootをrecursiveに探索し、物理pathとcanonical nameを失わないinventoryを作る。
- `SKILL.md` frontmatter、通常Skillのdirectory name、およびplugin namespaceについて既存のname解決規則を一元化する。
- 既存のCodex履歴filter、Skill deduplication、warning、TTY/JSON rendererを再利用し、同じ入力から決定的なreportを作る。
- broadなrepository scanでも、scope外のpathを読まず、Skill本文を出力せず、filesystemを変更しない。

**Non-Goals:**

- `--inventory`、全インストールSkill一覧、または新しいtop-level commandを追加すること。
- Skillのinstall/remove、name変更履歴、version単位の利用追跡、directory nameのalias解決を提供すること。
- 任意の `SKILL.md` をfilesystem全体から探すこと、Skill本文を解析して実行すること、Skill commandを実行すること。
- scan結果をcacheへ保存すること、scanを並列化すること、外部dependencyやremote inventory serviceを導入すること。

## Decisions

### 1. `skills --unused` を既存commandのviewとして追加する

`cmd/catsift` に `unused` のboolean optionとrepeatableな `roots` collectionを追加し、`kind == "skills"` の場合だけ利用する。`--root` は `--unused` と同時に指定された場合だけ有効にし、明示rootがあるときは既定rootを追加しない。`--unused` がない既存のparse、aggregation、JSON shapeには分岐を入れない。

新しいtop-level commandや `skills inventory` ではなく同じcommandのviewにするのは、historyを使ったSkill reportと対象domainが同じで、`--days`・`--strict`・`--codex-home` を自然に共有できるためである。独立commandは履歴sourceとSkill名解決の責務を重複させ、`--inventory` を既存のusage commandへ混在させる案は、履歴を参照しないsnapshotとの意味の違いを曖昧にする。

`--group-by` は既存の `skills` optionとして受け付けるが、unused viewでは「少なくとも1回利用されたか」というmembershipがgroupingで変わらないため、使用済み集合の作成には影響させない。help textにはこの意味を明記し、無視されたように見えないようにする。`--layer` など他command専用optionのvalidationは既存規則を維持する。

### 2. inventory discoveryを `internal/skillinventory` に分離する

filesystem discoveryの新しいpackageを `internal/skillinventory` とし、概念的に次のdataを返す。

```text
InventoryEntry
  Name            canonical Skill name
  Path            absolute Skill directory path
  NameSource      frontmatter または directory
  NameMismatch    frontmatterの最終name componentとdirectory basenameの不一致

InventorySnapshot
  Roots           resolved absolute roots
  InstalledCount  物理inventory entry数
  Entries         deterministicにsortされた全entry
  Warnings        recoverableなwalk warning
```

rootはflag parse後にabsoluteかつcleanなpathへ解決し、同じpathをdeduplicateする。root未指定時はuser homeの `~/.agents/skills` を1つだけ設定する。明示rootは存在しない、directoryでない、またはroot自体をreadできない場合を引数・scope errorとして返す。既定rootが存在しない場合だけは空scopeとして扱う。

scanにはGo標準libraryの `filepath.WalkDir` を使用する。指定rootがrepository parentの場合も配下をrecursiveに歩くが、candidateは次のlayoutに限定する。

- `.agents/skills/<skill>/SKILL.md`
- `.codex/skills/<skill>/SKILL.md`
- `.codex/skills/.system/<skill>/SKILL.md`
- `.codex/plugins/cache/<plugin>/<namespace>/<version>/skills/<skill>/SKILL.md`

`SKILL.md` は認識されたSkill root直下のSkill directoryにあるものだけをcandidateとする。これにより `/tmp/skills/example/SKILL.md` のような任意pathをinventoryへ混入させない。WalkDirはsymlink directoryをfollowせず、candidateのpath deduplicationとsorted outputによってroot overlapやfilesystem列挙順の差を吸収する。walk中の子directoryのread errorはrecoverable warningとして扱い、root自体のerrorとは分ける。

このpackageを `internal/usage` へ直接追加しないのは、inventoryが履歴eventではなく現在のfilesystem snapshotだからである。`internal/aggregate` はsnapshotと履歴から作ったused-name setを比較し、`internal/output` はsnapshotのrowを描画する。一方向の依存は次のようになる。

```text
cmd/catsift
  ├─ internal/codex       history source
  ├─ internal/usage       history Skill evidence/name resolver
  ├─ internal/skillinventory  filesystem snapshot
  ├─ internal/aggregate   used-name set と unused rows
  └─ internal/output      usage view / unused view renderer
```

### 3. frontmatter解決は既存resolverを拡張して共有する

inventory専用のparserを複製せず、`internal/usage` のfrontmatter解決を、解決元とmismatchを返せる内部処理へ整理する。外部から見える既存の `FrontmatterSkillName` と履歴のfallback挙動は維持し、inventoryはstrict layout classifierを通過した `SKILL.md` だけで同じ処理を呼ぶ。

frontmatterは `name` fieldの有効な値だけを利用し、本文全体をreport dataへ保存しない。readは既存の128 KiB上限を維持し、実装では上限を超えたdataを捨ててからparseできるよう、bounded readerを使用する。missing、invalid、oversized、またはreadできない場合はdirectory fallbackとし、entry自体は失わない。通常rootではdirectory basenameを使い、plugin cacheでは既存規則と同じ `namespace:directory` をfallback/canonical形にする。

`NameMismatch` は有効なfrontmatterを読めた場合だけ計算し、plugin namespaceを除いたfrontmatterの最終componentとdirectory basenameを比較する。fallback時は不一致を断定できないためfalseとする。canonical name matchingはcase-sensitiveな完全一致だけとし、frontmatterとdirectoryの別名mapは作らない。これにより、同じdirectory nameでも異なるcanonical nameを持つSkillや、同じcanonical nameの複数installを黙って統合しない。

### 4. used-name setは既存のSkill aggregationから作る

`codex.Load` の結果を通常viewと同じように一度だけ受け取り、`aggregate.SkillsBy(input, strict, SkillGroupByTurn)` で選択条件後のSkill rowsを生成する。rowsのnameだけをsetへ入れ、inventory entryの `Name` と照合する。これにより、turn単位のevidence merge、`confirmed > inferred > unconfirmed` のstate優先、`--days` のcutoff、および `--strict` のconfirmed-only規則を別実装しない。

`--group-by session` が指定されても、setの要素であるnameの存在性はturn/sessionのどちらでも変わらないため、unused membershipはturn viewを基準に固定する。`--codex-home` はこの履歴sourceだけに適用し、inventory rootの既定値には影響させない。directory nameをaliasにしないので、履歴側が古いdirectory fallback nameだけを持つケースは、canonical frontmatter nameとは別Skillとして扱われる。この制限はpath-awareな過去履歴移行を今回実装しないためである。

既存の `aggregate.Report` に通常の `Skills []SkillRow` と混在しない `UnusedSkills []skillinventory.InventoryEntry`、`InstalledSkills int` を追加する。unused branchはused-name setにないentryを抽出し、canonical name昇順、同名時はabsolute path昇順にsortする。通常branchは既存fieldだけを使うため、既存の集計値とrow順は変化しない。

### 5. outputにはview discriminatorと別row schemaを持たせる

`ReportContext` にSkill viewとresolved rootsを渡せるfieldを追加し、`internal/output` はcontextがunusedのときだけ専用rendererへ分岐する。通常 `skills` のhuman/JSON pathは変更しない。

unused JSONは既存の `schema_version`、`agent`、`period`、`generated_at`、`strict`、`group_by` を共通metadataとして維持し、次を追加する。

```text
view:            "unused"
roots:           resolved absolute roots
installed_count: inventory全体のentry数
unused_count:    rows数
rows: [
  {
    name,
    path,
    name_source,
    name_mismatch
  }
]
```

`view` を明示するのは、既存の `skills` usage rowにあるcount fieldとunused rowを同じ `rows` keyで返しても、consumerがschemaを誤認しないようにするためである。新viewだけを使うconsumerは `view` を先に確認でき、既存optionなしのJSON consumerには従来shapeがそのまま残る。rowsは必ずnon-nullのarrayとし、field順とtimestamp形式は既存JSON規則に合わせる。

human-readable outputは既存のtitle/contextを使いながらsectionを `UNUSED SKILLS` とし、period、strict、scopeを表示する。tableには最低限canonical Skill nameとabsolute pathを残し、mismatch markerを表示してfrontmatter解決の差を確認できるようにする。Skill directoryのbasenameはpathから判別できるため、独立したDirectory columnは持たない。狭いterminalではpathや補助columnをellipsisで縮めるが、nameはrow identityとして残す。0件時は空tableを描画せず、inventoryが0件の場合と全entryが使用済みの場合で説明を分ける。JSONにはcolorを通さず、inventory warningは既存のstderr warning経路へ送る。

### 6. errorとwarningの境界を既存CLIへ合わせる

explicit rootのvalidation failureはreportを描画する前に返し、stdoutを空のまま非0終了とする。default rootのmissingは正常なempty inventoryとする。recursive walk中の一部directoryだけがreadできない場合は、取得できたentryを使ってreportを生成し、warningをstderrへ要約する。inventory warningも履歴warningと同じwarning集合へ統合し、`--strict-input` が指定された場合は既存のnon-zero規則を適用する。

frontmatterのmissing/invalidはSkill directoryの通常状態としてwarningにせず、`name_source: "directory"` で可視化する。これはfrontmatterを任意に書かないSkillを大量にscanしてstderrを汚さないためである。Skill本文、履歴prompt、command textはどのoutputにも保存・出力しない。

### 7. testはinventory・比較・renderer・CLIを独立して固定する

新しいinventory packageには一時directoryを使うtable-driven testを置き、default root、repository parent、複数root、既知layout外、`.system`、plugin namespace、frontmatter mismatch、fallback、同名別path、symlink directory、列挙順の差を検証する。`internal/aggregate` ではall-time、`--days`、strict、grouping非依存、canonical exact match、重複installを検証する。

`cmd/catsift` では `--unused` のdispatch、`--root` のrepeat、rootなし時のdefault、既存 `skills` output不変、`stats/tools` でのinvalid option、history source error、warningと終了codeを検証する。`internal/output` ではunused JSONのfield、空array、human reportのscope/path、mismatch表示、狭幅、non-TTY、`NO_COLOR`、`--color`強制をgoldenまたはtable-driven testで固定する。既存のread-only testと合わせ、scanがfileを変更しないことも確認する。

## Risks / Trade-offs

- [repository parentをrecursiveにscanすると大きなtreeで遅くなる] → 既定scopeは狭い `~/.agents/skills` に限定し、broad rootは明示指定時だけ有効にする。MVPでは単純な逐次WalkDirを使い、並列化やcacheは計測後の別changeとする。
- [同じcanonical nameの複数installをpath単位で表示すると、1回の利用で複数entryが使用済みになる] → physical identityを失わず、name-only matchingの制限をspecとJSONのpathで明示する。version/path-aware trackingは将来の拡張に残す。
- [frontmatter nameと履歴の古いdirectory fallback nameが一致しない] → canonical exact matchを守り、directory・解決元・mismatchをreportへ出す。履歴pathからinstallを逆引きするalias mapは誤結合の危険があるため導入しない。
- [既定scopeが `.codex` のsystem/plugin Skillを含まないため、利用者が期待する「全インストール」と差が出る] → 初回の既定値をuser-managedな `~/.agents/skills` に限定し、`.codex` やrepository-local Skillは `--root` で明示的に追加できることをhelpとREADMEへ記載する。既定へ広げる変更は別途判断できる。
- [frontmatter parsingが巨大または悪意あるfileでmemoryを消費する] → recognized pathだけを対象にし、128 KiBのbounded read、本文の保持なし、command実行なしを守る。
- [symlink経由でscope外のfileへ到達する] → WalkDirでsymlink directoryをfollowせず、candidateのregular-file判定を行う。symlinkを正式に支援する場合は、targetがroot内にあることを検証する別設計が必要になる。
- [unused JSONのrow schemaが既存skills JSON consumerを混乱させる] → `view: "unused"` を必須のdiscriminatorとし、既存viewのJSON shapeとschema versionを変更しない。READMEにsampleを掲載する。

## Migration Plan

永続dataや履歴fileのmigrationは不要である。実装時はinventory package、name resolver共有化、aggregateのunused判定、outputのunused view、CLI wiring、test、README更新の順で追加する。既存の `catsift skills` と他commandのsnapshotを先に固定し、`--unused` のfixtureを追加した後に全体testを実行する。

release後に戻す場合は旧binaryへrollbackするだけでよい。catsiftはCodex履歴とSkill fileを変更せず、inventory cacheも作成しないため、user dataのrollback手順は発生しない。
