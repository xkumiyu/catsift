## MODIFIED Requirements

### Requirement: sessionとuser promptを識別する

システムはhistory source、Agent identity、およびprovider session identityの組み合わせごとに1つのsessionを数えなければならない（SHALL）。同じprovider session IDの文字列が異なるAgentまたはsourceに存在しても、別sessionとして扱わなければならない（MUST NOT）。user promptは実際のuser入力だけを数え、developer・system message、Skill本文の注入、tool outputをuser promptとして数えてはならない（MUST NOT）。この規則はCodex raw record、OpenCode raw履歴、およびctx normalized eventへ同じように適用しなければならない（SHALL）。

#### Scenario: 同一sessionに複数recordがある

- **WHEN** 同じsource、Agent、session identityに属する複数の履歴recordを処理する
- **THEN** session数は1となり、実際のuser入力だけがuser prompt数へ加算される

#### Scenario: 異なるAgentが同じsession IDを使う

- **WHEN** CodexとOpenCodeの履歴に同じprovider session ID文字列が存在する
- **THEN** システムは2つのsessionとして扱い、session数を誤って1へ統合しない

#### Scenario: 注入されたSkill本文がuser roleで保存される

- **WHEN** contextual inputとして`<skill>` blockを含むuser role messageが保存されている
- **THEN** システムはそのmessageをSkill検出の証拠として扱い、user prompt数には加算しない

#### Scenario: OpenCodeのuser promptとTool outputが混在する

- **WHEN** OpenCodeの1つのsessionに実際のuser message、assistant message、およびTool outputが保存されている
- **THEN** システムは実際のuser messageだけをuser promptとして数え、assistant messageとTool outputをpromptへ加算しない

### Requirement: Tool観測layerを区別する

システムはCodex raw record、OpenCode raw履歴、またはctx normalized eventから取得できるTool callと実行結果を、modelまたはruntime layerのTool観測として扱わなければならない（SHALL）。model/runtimeの区別、raw name、canonical name、callまたはitem ID、完了status、およびsource位置を取得できる範囲で保持しなければならない（SHALL）。layerを確定できないctx eventは、推測でmodelとruntimeを二重計上せず、定義されたfallbackまたはwarningの対象としなければならない（MUST NOT）。OpenCodeの実行済みTool partは、同じcallについてmodel wrapperを別に生成せずruntime layerへ正規化しなければならない（SHALL）。

#### Scenario: modelがcode-mode execを選択する

- **WHEN** `custom_tool_call`またはctxのmodel Tool eventのnameが`exec`である
- **THEN** システムはcanonical name `exec`、layer `model`のTool観測を生成する

#### Scenario: command実行が完了する

- **WHEN** `ItemCompleted`、OpenCodeのcommand Tool part、またはctxのcommand completion eventがcommand executionを含む
- **THEN** システムはcanonical name `shell`、layer `runtime`のTool観測を生成する

#### Scenario: MCP Toolが完了する

- **WHEN** `ItemCompleted`、OpenCodeのMCP Tool part、またはctx normalized activityがMcpToolCallを含む
- **THEN** システムはMCP server名とTool名を失わないcanonical name、layer `runtime`のTool観測を生成する

#### Scenario: その他の既知runtime actionが完了する

- **WHEN** Item、OpenCode Tool part、またはctx eventがFile変更、Web検索、image閲覧、image生成、またはcollaboration actionを含む
- **THEN** システムはaction種別を表す安定したcanonical nameとlayer `runtime`のTool観測を生成する

#### Scenario: ctx eventにlayer情報がない

- **WHEN** ctx eventがTool名を含むがmodel/runtimeを区別できる情報を含まない
- **THEN** システムは外側のmodel wrapperとruntime actionを同一利用として二重計上せず、定義されたfallbackまたはwarningを適用する

### Requirement: Skill利用を証拠種別とともに検出する

システムはCodex raw record、OpenCode raw履歴、またはctx normalized eventで取得できる証拠を対象に、Skill専用recordの存在だけを前提としてはならない（MUST NOT）。注入されたstructured `<skill>` blockを`explicit-injected`、structured Skill toolまたは同等のOpenCode・ctx activityを`structured-tool`、canonicalな先頭`$skill-name` user入力を`explicit-request`、既知Skillの`SKILL.md`または`scripts`配下への実行時accessを`implicit-access`として識別しなければならない（SHALL）。各Skill観測はSkill名、利用mode、検出方式、確認状態、timestamp、session、turn、およびsource位置を取得できる範囲で保持しなければならない（SHALL）。ctxが根拠を保持していない通常文からSkill利用を推測してはならない（MUST NOT）。

#### Scenario: Skillがcontextへ正常に注入される

- **WHEN** contextual user inputに有効な`<skill>` blockとSkill名が含まれる
- **THEN** システムはmode `explicit`、method `explicit-injected`、state `confirmed`のSkill観測を生成する

#### Scenario: structured Skill toolが実行される

- **WHEN** native `Skill` tool、namespaced `skills.read`、OpenCodeのstructured Skill Tool、またはctx eventのstructured activityが特定Skillを対象とするSkill tool executionを示す
- **THEN** システムはmethod `structured-tool`、state `confirmed`のSkill観測を生成する

#### Scenario: userがcanonical markerを入力する

- **WHEN** 実際のuser入力が先頭の`$skill-name`で始まり、対応する注入またはstructured Toolの証拠がない
- **THEN** システムはmethod `explicit-request`、state `unconfirmed`のSkill観測を生成する

#### Scenario: Skill fileへ実行時accessする

- **WHEN** runtime command、OpenCode Tool arguments、またはctx activityが具体的な既知Skillの`SKILL.md`またはそのSkillの`scripts`配下へのaccessを示す
- **THEN** システムはmode `implicit`、method `implicit-access`、state `inferred`のSkill観測を生成する

#### Scenario: prose内でSkillらしい文字列に言及する

- **WHEN** userまたはassistantの通常文中に`$skill-name`や`SKILL.md`という文字列が現れるだけで、上記の構造または実行証拠を満たさない
- **THEN** システムはSkill観測を生成しない

### Requirement: 正規化済みEventにsourceとAgent identityを保持する

各正規化済みsession、turn、Tool観測、およびSkill観測は、取得元sourceとcanonical Agent identityを追跡できなければならない（SHALL）。Codex local history、OpenCode local history、およびctx eventは互いに異なるsource scopeとして保持しなければならない（SHALL）。Agent identityを取得できないeventは既定の`unknown` scopeへ隔離し、既知Agentの集計へ黙って混入させてはならない（MUST NOT）。

#### Scenario: ctx eventにprovider identityがある

- **WHEN** ctx eventがCodexまたはOpenCodeのprovider identityを持つ
- **THEN** システムはそのidentityを正規化済み観測へ保持し、reportのAgent一覧と集計scopeへ反映する

#### Scenario: provider identityを取得できない

- **WHEN** 履歴eventにAgent identityがなく、sourceからも安全に解決できない
- **THEN** システムはeventを`unknown` Agent scopeへ分類し、既知Agent名のrowへ合算せず、warningを記録する

#### Scenario: OpenCode local historyを直接読む

- **WHEN** userがOpenCode local historyを入力sourceとして選択する
- **THEN** システムは正規化済み観測のsourceをOpenCode、Agentをcanonical ID `opencode`として保持する
