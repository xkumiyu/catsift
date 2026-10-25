# Session Purpose Classification

## 背景

CatSiftはsessionの利用状況を表示するが、作業目的による分類は提供していない。ユーザーが明示的に有効化した場合に限り、session titleとuserが入力したmessageをTypeSafe System Oneへ送信してpurposeを分類する。

## 変更内容

- Jev分類を既定で無効にし、`TYPESAFE_API_KEY`がある場合だけ設定で有効化できるようにする。
- 固定された既定taxonomyで分類し、sessionごとのclassificationを専用の永続データとして保存する。
- Session detailに分類結果を表示する。

## 影響

分類を有効にした場合、選択されたsessionのtitleと実際のuser-authored messageだけを外部APIへ送信する。requestは8,000 Unicode code pointsに制限し、raw promptは永続化・loggingしない。
