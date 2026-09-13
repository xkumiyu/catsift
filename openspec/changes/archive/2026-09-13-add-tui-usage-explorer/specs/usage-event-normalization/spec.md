## ADDED Requirements

### Requirement: Model identityとToken usageの帰属を保持する

システムはhistory sourceが報告するproviderとModel nameを正規化済みModel identityとして保持しなければならない（SHALL）。Token usage eventは、取得できる場合、対応するModel identityと観測時刻を保持しなければならない（SHALL）。一つのSessionまたはTurnに複数のModelが観測された場合、後から観測されたModelで既存のModelを上書きしてはならない（MUST NOT）。Modelを取得できないusageは安定したunknown identityへ分類しなければならない（SHALL）。

#### Scenario: Token usageとModelを対応付ける

- **WHEN** provider eventがModel `gpt-example`とToken usageを同時に報告する
- **THEN** 正規化済みToken usage eventはprovider・nameが対応付いたModel identityと観測時刻を保持する

#### Scenario: 一つのSessionでModelが切り替わる

- **WHEN** 同じSessionの異なるTurnでModel `model-a`と`model-b`が観測される
- **THEN** 正規化結果は両Modelを保持し、それぞれのToken usageを該当Modelへ帰属させる

#### Scenario: Model情報が欠損する

- **WHEN** Token usageはあるがprovider eventからModel identityを安全に取得できない
- **THEN** システムはusageを破棄せずunknown Modelへ分類する

### Requirement: Session detail用の共通metadataを保持する

システムは各sourceのSession metadataを、source、Agent、provider/session identity、Project、CLI version、および取得可能な作成・更新時刻を含む共通Session metadataへ変換しなければならない（SHALL）。内部のSession identityはsource、Agent、およびsource固有のSession identityを組み合わせたsource-qualified keyでなければならない（SHALL）。metadataに時刻がない場合、Sessionの利用期間は所属Turnまたは利用eventの最小・最大時刻から導出しなければならない（SHALL）。

#### Scenario: 同じIDを異なるsourceが使う

- **WHEN** CodexとOpenCodeが同じ文字列のprovider session IDを持つ
- **THEN** 正規化結果は異なるsource-qualified keyを生成し、2つのSessionを統合しない

#### Scenario: Session metadataに時刻がない

- **WHEN** sourceがSessionのcreated/updated時刻を報告せず、所属Turnには時刻がある
- **THEN** システムはTurnの時刻からSessionの表示期間を導出する

#### Scenario: Session metadataをcacheへ保存する

- **WHEN** 正規化済みSessionをcacheへ書き込む
- **THEN** source、Agent、session identity、Project、CLI version、時刻などのsanitized metadataは復元でき、prompt本文やprovider payloadは保存されない
