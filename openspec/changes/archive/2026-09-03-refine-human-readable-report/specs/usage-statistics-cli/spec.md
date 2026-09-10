## MODIFIED Requirements

### Requirement: overview統計を表示する

`catsift stats` は内容を示す `USAGE OVERVIEW` headingに続けて、対象Agentと期間をラベル付きのcontext行として表示し、Sessions、User Prompts、Tool Calls、およびSkill Usesを視覚的に区切られたsummaryとして表示しなければならない（SHALL）。グローバルな実行ファイル名または製品名だけのtitle（例: `CatSift`、`catsift stats`）をreportのheadingとして表示してはならない（MUST NOT）。Tool Callsはeffective Tool view、Skill Usesはturn単位で重複排除した全確認状態の利用を使用しなければならない（SHALL）。

#### Scenario: 履歴が存在する

- **WHEN** userが有効なCodex履歴に対して `catsift stats` を実行する
- **THEN** システムは `USAGE OVERVIEW` heading、`Agent: Codex` および対象期間を示すcontext行、label付きの4つの集計値を既定のhuman-readable report形式でstdoutへ出力する

#### Scenario: 対象履歴が空である

- **WHEN** 読取対象に有効な利用Eventが存在しない
- **THEN** システムは `USAGE OVERVIEW` headingと対象Agent・選択期間のcontextを表示し、各集計値を0として出力するとともに、選択期間に利用がないことを説明するempty-state messageを表示して0で終了する

### Requirement: Tool統計を表示する

`catsift tools` は `TOOL USAGE` headingに続けて、対象Agent、選択中の期間、およびlayerをそれぞれラベル付きのcontext行として表示し、canonical Tool名ごとのCalls、Failures、およびLast Usedを見出し付きtableとして表示しなければならない（SHALL）。footerは対象Tool数と総Callsをdomain用語で表示し、`Rows`という実装用語や中点区切りを使用してはならない（MUST NOT）。既定ではeffective layerを集計し、`--layer effective|runtime|model` により集計layerを選択できなければならない（SHALL）。

#### Scenario: 既定のTool統計を表示する

- **WHEN** userがlayer指定なしで `catsift tools` を実行する
- **THEN** システムは `TOOL USAGE` heading、Agent・Period・Layerのcontext、effective Tool利用をCalls降順、同数の場合はTool名昇順で、選択layerが分かるhuman-readable tableとして出力する

#### Scenario: Tool footerを表示する

- **WHEN** userが対象Toolのあるhuman-readable `catsift tools` reportを表示する
- **THEN** システムはfooterに `N tools, M calls total` 相当の、対象Tool数と総Callsが分かる表現を表示する。1件の場合は `tool` と `call`、0件の場合は `tools` と `calls` のように自然な単数・複数形を使用する

#### Scenario: model layerを指定する

- **WHEN** userが `catsift tools --layer model` を実行する
- **THEN** システムはruntime actionではなくmodel layerのTool callだけを集計する

#### Scenario: 無効なlayerを指定する

- **WHEN** userが定義外の `--layer` 値を指定する
- **THEN** システムは引数errorをstderrへ出力し、非0の終了codeを返す

### Requirement: Skill統計を表示する

`catsift skills` は `SKILL USAGE` headingに続けて、対象Agent、選択中の期間、grouping、およびstrict状態をそれぞれラベル付きのcontext行として表示し、Skill名ごとのExplicit、Implicit、Confirmed、Inferred、Unconfirmed、Total、およびLast Usedを集計しなければならない（SHALL）。human-readable reportは利用可能な幅に応じて内訳columnを調整できるが、Skill名とTotalを常に表示しなければならない（SHALL）。footerは対象Skill数と総Usesをdomain用語で表示し、`Rows`という実装用語や中点区切りを使用してはならない（MUST NOT）。JSONはすべての集計fieldを保持しなければならない（SHALL）。`--strict` が指定された場合はstateが `confirmed` の利用だけをTotalと各mode集計の対象にしなければならない（SHALL）。

#### Scenario: 全Skill観測を表示する

- **WHEN** userが `catsift skills` を実行する
- **THEN** システムは `SKILL USAGE` heading、Agent・Period・Group by・Strictのcontext、全確認状態のSkill利用をTotal降順、同数の場合はSkill名昇順で、確認状態の内訳が判別できるhuman-readable tableとして出力する

#### Scenario: Skill footerを表示する

- **WHEN** userが対象Skillのあるhuman-readable `catsift skills` reportを表示する
- **THEN** システムはfooterに `N skills, M uses total` 相当の、対象Skill数と総Usesが分かる表現を表示する。1件の場合は `skill` と `use`、0件の場合は `skills` と `uses` のように自然な単数・複数形を使用する

#### Scenario: strict modeを使用する

- **WHEN** userが `catsift skills --strict` を実行する
- **THEN** システムは `confirmed` でないSkill利用を件数とLast Usedの算出から除外する

#### Scenario: 同一利用に複数の証拠がある

- **WHEN** 1回の重複排除済みSkill利用が複数の検出方式を持つ
- **THEN** システムはその利用をTotalへ1回だけ加算する

### Requirement: human-readable report・JSONを提供する

各統計commandは既定で、commandの実行ファイル名ではなくreport内容を示すheading、対象Agent・適用中のfilterをラベル付きcontext行、明確なsectionまたはcolumn heading、整列した値、および必要なfooterを持つhuman-readable static reportを出力しなければならない（SHALL）。context行は項目ごとに改行して表示し、`·`（中点）で項目を連結してはならない（MUST NOT）。countは桁区切りして右揃えにし、tableのLast Usedはtimezoneを含む簡潔なlocal日時で表示しなければならない（SHALL）。`--json` でmachine-readable出力へ切り替えられなければならず（SHALL）、JSONはhuman-readable reportと同じfilter・集計結果を表してfield順をcommandごとに安定させ、timestampをRFC 3339で出力しなければならない（SHALL）。この変更ではJSONのfield名・構造・値の意味を変更してはならない（MUST NOT）。

#### Scenario: 既定reportを表示する

- **WHEN** userが出力形式を指定せず任意の統計commandを実行する
- **THEN** システムはreport内容を示すheading、対象Agentとfilterのcontext、指標名と値の関係を一読できるsummaryまたはtable、およびdomain用語で表現されたfooterを持つstatic reportをstdoutへ出力する

#### Scenario: contextを読みやすく表示する

- **WHEN** userが `catsift tools` または `catsift skills` を実行する
- **THEN** システムは `Agent: ...`、`Period: ...`、およびcommand固有のfilterを別々のlabel付き行へ出力し、項目間の区切りに中点や実装用の`Rows`を使用しない

#### Scenario: JSONを出力する

- **WHEN** userが任意の統計commandへ `--json` を指定する
- **THEN** stdout全体は単独の有効なJSON documentとなり、warningや進捗messageを含まず、human-readable heading・context・footerの文言を含まない

### Requirement: terminal capabilityに応じて安全に装飾する

既定の `--color auto` では、システムはstdoutがTTYであり `NO_COLOR` が設定されていない場合だけhuman-readable reportへANSI styleを適用しなければならない（SHALL）。`--color always` は明示的にstyleを有効化し、`--color never` は無効化しなければならず、この2つの強制modeは `NO_COLOR` より優先しなければならない（SHALL）。JSONにはoptionやterminal状態にかかわらずANSI escape sequenceを含めてはならない（MUST NOT）。色は補助表現に限り、label、text、数値、または配置なしに意味を伝えてはならない（MUST NOT）。colorが有効なhuman-readable reportでは、report headingを共通のaccent styleで目立たせ、table headerをheadingと区別できるstyleで表示しなければならない（SHALL）。通常のlabel・table row・count・Failures・Skillのevidence status値は既定色を基本とし、table cellをerror colorやwarning colorで強調してはならない（MUST NOT）。通常状態を示すためだけの一律なsuccess colorを全rowへ適用してはならない（MUST NOT）。

#### Scenario: TTYへ既定reportを出力する

- **WHEN** stdoutがcolor対応TTYで、`--color` が未指定か `auto` であり、`NO_COLOR` が設定されていない
- **THEN** システムはreport headingとtable headerを補助styleで判別しやすく表示する

#### Scenario: reportをredirectする

- **WHEN** stdoutがTTYでなく、`--color` が未指定か `auto` である
- **THEN** システムはANSI escape sequenceを含まない、整列したplain-text reportを出力する

#### Scenario: NO_COLORを尊重する

- **WHEN** `NO_COLOR` が設定され、`--color` が明示されていない
- **THEN** システムはhuman-readable reportへANSI styleを適用しない

#### Scenario: colorを明示指定する

- **WHEN** userが `--color always` または `--color never` を指定する
- **THEN** システムはTTY判定や `NO_COLOR` より明示指定を優先してstyleを有効化または無効化する

#### Scenario: machine-readable出力でcolorを強制する

- **WHEN** userが `--json --color always` を指定する
- **THEN** システムはANSIを含まない有効なJSONを出力する

### Requirement: terminal幅に応じてreportをcompact化する

human-readable reportは取得可能なterminal幅へ収まるようにlayoutを選択しなければならない（SHALL）。狭い幅では補助的な内訳columnを省略またはcompact化できるが、report内容を示すheading、対象Agent・期間を示すcontext、row identity、および主要countを保持しなければならない（SHALL）。長いnameはcolumn境界内で省略記号付きに短縮し、異なるrowの値に見える折返しをしてはならない（MUST NOT）。

#### Scenario: 十分に広いterminalで表示する

- **WHEN** terminal幅が全columnを表示できる
- **THEN** システムはToolまたはSkillの定義済み内訳とLast Usedを含むwide layoutを表示する

#### Scenario: 狭いterminalで表示する

- **WHEN** terminal幅がwide layoutに不足する
- **THEN** システムはreport heading、Agent・Periodのcontext、主要nameとcountを残したcompact layoutを表示し、table rowを誤解を招く形で折り返さない
