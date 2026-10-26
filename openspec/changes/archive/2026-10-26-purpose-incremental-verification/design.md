# 設計

## 二相ロード

分類が有効でも、最初のhistory loadはingestion cacheを使う。cache snapshotはmessage bodyを含まないが、session ID、title、更新時刻、turn数、prompt数、source識別子を持つため、再検証対象の判定には十分である。

1. **判定相** — cacheから読み込んだ`query.Input`とpurposes storeを比較し、再検証が必要なsessionをsource別に求める。
2. **本文相** — 対象sessionのmessage bodyだけをsourceからparseし、input fingerprintを再計算する。fingerprintが一致すれば保存済み結果を再利用し、不一致または未保存の場合だけ分類する。

cache missで判定相が本文を取得済みの場合は、その本文を再利用する。session内の各turnでmessage数と選択済みprompt数が一致する場合だけ取得済み本文を完全とみなす。本文を持たないturnや、期間filter後のprompt数と未filterのmessage数が異なるturnを含むsessionだけを本文相で読み直す。

OpenCodeでは、1つのuser messageに属するtext partから注入されたSkill本文だけのpartを除外し、残りを改行で結合して1つの分類用messageとする。Skill evidenceの検出では元のpart単位の情報を維持する。既存storeの複数partのfingerprintとは異なるため、次回の再検証時に1回再分類される場合がある。ingestion cacheの保存内容は変わらない。

本文相ではsourceごとの失敗を集約し、他sourceの分類と保存は継続する。巡回検証で本文を取得できない場合は、revision証拠が一致する保存済み分類をreportに維持し、検証時刻を進めず次回に再試行する。

`RequireUserMessages`によるcache bypassは廃止する。分類のために全履歴の本文を最初から読み込む必要がなくなるためである。

## revision証拠

各store entryに、その結果を検証した時点の入力証拠を保存する。証拠はsource非依存の文字列とし、次の値を正規化してhash化する。

- session title
- sessionの更新時刻
- 選択されたprompt数とprompt時刻列（期間変更による入力の入れ替わりを検出する）
- source file revision（Codexのみ。sizeとmtime）

Codexのcache読み込みでは、cache snapshotのsessionに読込元file pathをメモリ上だけ付与する。pathはcacheへ保存せず、reportやstoreへも保存しない。OpenCode、GitHub Copilot、ctxではfile revisionを使わず、更新時刻と選択済みpromptのmetadataで判定する。cacheの有無でpathが変わっても証拠は変化しない。移動する`--days`境界も、選択されるpromptが同じ間は証拠を変えない。

証拠はあくまでheuristicであり、同一内容での置換やtimestampを変えない編集を検出できない場合がある。そのため、証拠一致だけに依存せず巡回検証を併用する。

## 再検証の判定

sessionごとに次の順で判定する。

1. user promptを持たないsessionは対象外とする。判定相ではcache snapshotのprompt数を使い、本文相で注入contextだけのsessionを除外する。
2. store entryがない場合は分類対象とする。
3. classifier versionまたはtaxonomy fingerprintが一致しない場合は分類対象とする。結果の互換性が失われているため、fingerprint照合では再利用しない。
4. revision証拠が保存値と一致する場合は保存済み結果を再利用する。
5. 証拠が変化した場合は本文をparseしてfingerprintを照合する。一致すれば`verified_at`と証拠を更新し、不一致なら分類対象とする。

## 巡回検証

証拠が変化を検出できなかった場合のstaleを閉じるため、`verified_at`が7日以上前のentryを対象に、1実行あたり最大50件を`verified_at`の古い順に再検証する。`verified_at`を持たないentryを最も古いものとして扱う。7日以内に本文照合済みのentryは、証拠一致の再利用を優先し、巡回対象にしない。対象は現在のhistory loadに含まれるsessionに限る。再検証は本文parseとfingerprint照合だけを行い、一致すればAPI requestを送らない。parseに失敗したsessionの`verified_at`は更新せず、次回に再試行する。

巡回予算は内部定数とし、CLI optionやconfigには露出しない。試験的機能の間は、full再検証が必要な場合に`purposes.json`を削除する運用とする。

## store schema

storeのtop-levelに`schema_version`を追加する。classifier versionとは独立に管理し、store形式の変更では既存結果を無効化しない。`schema_version`を持たない既存storeは旧形式として読み込み、次回保存時に現行versionを書き込む。

entryには既存のinput fingerprint、taxonomy fingerprint、version、mode ID、mode nameに加えて`verified_at`とrevision evidenceを保存する。いずれもraw promptやAPI keyを含まない。未知fieldは無視し、欠落fieldはゼロ値として扱うため、新旧binary間でstoreを共有しても読み込みは壊れない。

## 移行

revision evidenceを持たない既存entryは、初回だけ本文照合の対象になる。このときfingerprintが一致すればAPI requestを送らずにevidenceと`verified_at`を記録し、不一致のsessionだけを分類対象にする。したがって、導入後の初回実行だけは従来と同じ本文parseコストが発生するが、2回目以降は証拠一致のsessionを再利用できる。

## 本文parseの範囲

- **Codex** — 対象sessionのsource fileだけを読む。sessionとfileの対応はcache読み込み時に付与したpathを使う。file単位で本文をparseし、session keyごとにmessageを集約する。
- **OpenCode** — 単一databaseのため、対象sessionが1件でもあればdatabase全体をparseする。分類なしのcache loadに比べた追加コストは限定的である。
- **GitHub Copilot / ctx** — 対象sessionがある場合だけsource全体をparseする。file数と履歴規模が小さいため、対象fileの選択は行わない。

本文相のparse結果はingestion cacheへ書き込まない。message bodyをcacheへ残さない既存のprivacy方針を維持する。

## 確認と失敗時

API requestの確認しきい値は現行どおり、再検証後に残ったpending session数で判定する。巡回検証と証拠一致の再利用はAPI requestを伴わないため、確認対象に含めない。

API requestの失敗は従来どおりreport全体を失敗させず、当該sessionを未分類として扱う。失敗したsessionのstore entryは更新しない。分類結果の保存に失敗した場合もreportは継続し、warningを出す。

## テスト方針

- storeのschema version、`verified_at`、evidenceの保存と後方互換読み込み。
- 新規sessionだけが分類され、証拠一致の既存sessionでは本文parseとAPI requestが発生しないこと。
- 証拠変化時に本文parseが走り、fingerprint一致ならAPI requestなし、不一致なら分類されること。
- 巡回検証が古いentryから予算件数だけ行われ、fingerprint一致時に`verified_at`だけ更新されること。
- Codexの対象file限定読み込みと、session pathがcache・store・reportへ永続化されないこと。
- 大量pending時の確認とskipの既存挙動が維持されること。
