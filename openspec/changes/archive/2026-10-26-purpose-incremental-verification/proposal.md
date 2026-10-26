# Session Purposeの増分検証

## 背景

session purpose分類は、分類が有効な`catsift sessions`とTUIの起動ごとに、message bodyを持つ全履歴を再parseしている。ingestion cacheはprivacy方針によりmessage bodyを保存しないため、分類有効時はcacheを意図的に無視しており、Codexの履歴が大きい環境では起動のたびに数十秒を要する。

一方で、既存sessionの大半は追加会話がなく、分類結果が変わることは稀である。変化の有無を確かめるために全message bodyを読む必要がある点が、実際のコストになっている。

## 変更内容

- ingestion cacheから本文なしでsession一覧を読み、保存済み結果と比較して再検証が必要なsessionだけを特定する。
- 新規sessionは既定で分類する。
- 既存sessionは次のいずれかに該当する場合だけ本文をparseして再検証する。
  - classifier versionまたはtaxonomy fingerprintが保存値と一致しない
  - title、session更新時刻、prompt数、source file revisionなどのrevision証拠が保存値と変化している
  - 1実行あたり最大50件の予算で、`verified_at`が古い順に巡回検証する対象になった
- 再検証でinput fingerprintが一致した場合はAPI requestを行わず`verified_at`だけを更新する。不一致または未保存のsessionだけを分類対象にする。
- Codexは対象sessionのfileだけを読み、OpenCode、GitHub Copilot、ctxは本文が必要な場合に限りsource全体をparseする。
- purposes storeに`schema_version`、`verified_at`、revision evidenceを追加する。classifier versionは変更しない。
- full再検証の手段は`purposes.json`の削除とする。新しいCLI optionやTUI operationは追加しない。

## 影響

- 通常の起動でmessage bodyを読む対象は、新規sessionと証拠が変化したsession、巡回検証の予算分だけになる。分類なしの起動と同程度のcache loadに、限られた本文parseが加わる。
- revision証拠はheuristicである。証拠が変化を検出できなかった場合に備え、巡回検証でstaleの上限を設ける。API requestはfingerprint不一致時だけ発生するため、巡回検証そのものは課金を伴わない。
- storeのschema versionとclassifier versionを分離する。store形式の変更は既存結果を自動的に無効化しない。
- 外部送信内容、taxonomy、API、確認しきい値は変更しない。
