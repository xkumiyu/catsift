## MODIFIED Requirements

### Requirement: 未使用Skillを決定的なhuman-readable reportとJSONで出力する

未使用CLIは既定でJSONを出力し、人向け表示はTUIのSkills unusedを使用しなければならない（SHALL）。JSONは既存contextと`view: "unused"`、source、agents、roots、installed_count、unused_count、rowsを維持しなければならない（SHALL）。各rowはname、absolute path、name_source、name_mismatchを保持し、name昇順・同名path昇順で物理entryを数えなければならない（SHALL）。

#### Scenario: 未使用SkillをJSONで表示する
- **WHEN** userが`catsift skills --unused`を実行する
- **THEN** stdoutは単独のJSONとなり、未使用entryだけをrowsへ出力し、空でも`[]`を保持する

#### Scenario: ctxで同名・異PATHのSkillを表示する
- **WHEN** 同じcanonical nameの未観測entryが異なるpathに存在する
- **THEN** JSONとTUIはpath順の別rowとして表示し、物理entry数として数える

#### Scenario: すべて使用済みの場合にempty stateを表示する
- **WHEN** 全inventory entryに選択条件内の利用が存在する
- **THEN** JSONはinstalled_countを保持してunused_countを0とし、TUIは選択履歴に未観測entryがないと表示する

#### Scenario: 未使用Skillをhuman-readableで表示する
- **WHEN** userが人向け表示を必要とする
- **THEN** static reportでなくTUIのSkills unusedでcontextとinstall pathを表示する

#### Scenario: 出力順をfilesystem列挙順から独立させる
- **WHEN** 同一inventoryを異なる順序で列挙する
- **THEN** JSONとTUIは同じ名前/path順と件数を表示する
