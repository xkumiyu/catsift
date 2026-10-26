# Design

## Context

動機はproposal.mdを参照。readerの明示的なknownEventType、normalizerのSkill処理、turn内のmethod/nameによる既存重複排除を使います。CLI詳細とTUIは共通のWarningAdviceを使い、CLI通常要約も同関数を参照します。

## Goals / Non-Goals

既存のturn単位のSkill利用とwarning JSON契約を維持します。新しいsubagent集計、abortによるturn境界の変更、本文参照の展開、任意のsession.*受理は対象外です。

## Decisions

- `skill.invoked_ref`を`skill.invoked`と同じ分岐へ追加し、既存のname/path正規化を再利用します。contentIdは本文identityでありinvocation identityではないため重複排除に使いません。invokedAtTurnはchat-message数であり既存turnIdとして解釈しません。
- Skill名の解決をturn生成・更新より先に行い、identityのないinline eventと参照形式eventを共通処理で無視します。参照形式のmodelは既存inline eventと同様に観測せず、未確認のturn fieldも推測しません。
- 11 metadata eventは受理のみ。human_responseはruntime質問への応答であり新しいuser.messageとして数えません。subagent.failedのoptional totalTokensは親のusageと重複し得る合計であり加算しません。subagent.configuredのmodelも親モデルへ反映しません。
- unknown_typeの共通adviceを統計への影響未確認へ変更します。隣接するctx専用reasonの従来adviceは維持します。
- 帰属を確認できるactive turnがない`skill.invoked_ref`はturnを新設せず、session/turn時刻やtoken帰属を変更せずskipします。既存turnへの推測による紐付けはしません。`copilot_unattributed_skill`のtype/source/countを既存warning経路で要約し、cacheにも保持します。identityのない参照は従来どおり無視します。参照しかないfileではsessionも登録しません。inline側で既に集計済みの重複receiptである可能性があるため、診断はSkill欠落を断定しません。同名Skillだけでは同一invocationと確認できないため警告の抑制や推測による再帰属はせず、帰属不能の参照件数を保持します。turn別統計の欠落の可能性を知らせるwarning levelを維持します。
- metadataだけのfileも既存のmetadata eventと同様にsession identityと時刻を保持し、期間filterなしの利用実績のないsession一覧へ表示可能にします。期間filterがある場合は既存のturnに基づくsession選択を維持し、metadata-only sessionは表示しません。turnや利用観測は作りません。帰属不能receiptはsessionの活動を確定しないため例外として登録しません。
- inline Skill identityの既存alias・scalar変換は互換性のため維持します。新規の参照形式は公開schemaのstring name/pathだけに限定し、numeric identityは受理しません。
- ParserVersionをv7/v8/v9からv10へ更新します。検証中に作成された中間cacheも無効化するため、versionを再利用しません。

## 公開schema確認

公式github/copilot-sdkのcommit `c57638f4a7ce2255a34192d5035932d0e109bbdd`を確認しました。

- [Node.js生成型](https://github.com/github/copilot-sdk/blob/c57638f4a7ce2255a34192d5035932d0e109bbdd/nodejs/src/generated/session-events.ts): abortは中断理由、human_response.recordedはrequestに対応する応答、subagent.selectedはagent選択、subagent.configuredはruntime設定、session.binary_assetは参照用binary、session.mode_changedはmode遷移、session.task_completeは完了通知、session.mode_notice_deliveredはmodelへのmode通知配送、session.permissions_changedはpermission設定、session.warningはtimeline警告、subagent.failedは失敗と任意の合計metricsです。
- [Java生成型](https://github.com/github/copilot-sdk/blob/c57638f4a7ce2255a34192d5035932d0e109bbdd/java/sdk/src/generated/java/com/github/copilot/generated/SkillInvokedRefEvent.java): skill.invoked_refはdurable invocation receiptで、name/pathを保持し、本文だけを以前のinline eventのcontentIdで参照します。
- Context7の公式SDK/CLI docsでもskill.invokedとmetadataの説明を確認しました。Node.js生成型にはskill.invoked_refが存在せず、Java型はexperimentalです。実履歴のpayload照合は行わず、公開fieldだけを使います。

## Risks / Trade-offs

- 将来のschema変更 → 未知typeは警告を維持し、wildcardで受理しません。
- abortによるturn終了は今回変更しません。reasonだけのeventからturn境界を決める契約が未確認であり、既存の終了event処理を維持します。
- metadataの合計metrics → 既存の詳細usageとの二重計上を避け、親turnの統計には足しません。
- 同一turn内で同じSkillを複数回使う → 既存のturn単位Skill利用契約を維持します。別turnは独立して集計します。

## Migration Plan

新parserでsourceが読める場合にcacheを再構築します。履歴fileは変更せず、raw payloadは永続化しません。rollback時もparser version不一致で再構築します。
