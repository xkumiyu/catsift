## Context

現状のCLIはsourceの`Load`結果を共通の`[]usage.Turn`へ変換してから、すべてのreportを同じ集計処理へ渡している。CodexのJSONLはline単位で読んでいるが、cacheはなく、ctxはpageごとのraw event mapを全pageの読取完了まで保持している。ctxの公開event queryはimmutable generation、completion record、cursor、および決定的なevent順序を提供する。

See `proposal.md` for the motivation and the delta specs for the behavior contract. 実装はctx内部SQLiteや検索indexを直接読まず、既存の公開CLIだけを使う。

## Goals / Non-Goals

**Goals:**

- cacheの有無でreportの件数、順序、filter、warning、JSON結果を変えない。
- raw provider payloadを履歴全体分保持せず、eventごとの解析結果とactive turnだけを保持する。
- Codexはfile単位、ctxはcomplete immutable generation単位でcacheを再利用する。
- `--days`をcache済みの正規化データへ適用し、cache hit時に履歴全体を再取得・再解析しない。
- 標準libraryだけでcacheを扱い、sourceをread-onlyのままにする。

**Non-Goals:**

- SQLite、独自daemon、外部network service、またはproviderの内部storageへの依存を追加しない。
- ctxの既存の時間範囲queryを、cacheのない`--days`初回だけ全履歴queryへ置き換えない。これにより現在のctxの短い期間queryを遅くしない。
- cacheの差分更新やCodex fileのtail-only読取は初版では行わない。変更されたfileは安全性を優先して再処理する。
- `--no-cache`などの新しいuser-facing option、cache管理command、圧縮、バックグラウンドwarmingは追加しない。

## Decisions

### 1. cacheは標準libraryのopaque JSON fileとする

`internal/cache`に、user cache directoryの解決、source namespaceとkeyのhash化、schema envelopeの検証、atomic writeを集約する。既定の保存先は`os.UserCacheDir()`配下の`catsift`とし、cache directoryは`0700`、cache fileは`0600`で作成する。

cache fileはsource adapterが作るpayloadをopaqueに保持し、envelopeには少なくとも以下を含める。

- cache schema versionとnormalizer version
- source kindとsource scope
- source revision
- `complete` marker
- 正規化済みsnapshot

JSONを選ぶ理由は、標準libraryだけで実装でき、破損時に検査しやすく、外部DB dependencyを増やさないためである。SQLiteはqueryや差分更新には強いが、今回の単純なkey lookupには過剰で、依存・migration・lockの保守コストを増やすため採用しない。

cacheは最適化用のbest-effort stateとし、読取不能・作成不能でもsourceからの通常処理とreport生成を失敗させない。

### 2. cache keyはCodex file単位、ctx data root単位で分ける

Codexではcanonical absolute pathをhashした固定cache fileを使用し、payloadにfileの`size`、nanosecond `mtime`、および必要なmetadataを保存する。stat値が一致するfileだけを再利用し、追加・更新・truncateされたfileはcache missとしてfile全体を再処理する。fileの読取前後でstatが変わった場合、そのsnapshotはcacheへ公開しない。

ctxではcanonical data rootをhashしたcache fileを使用し、payloadにcomplete generation IDを保存する。再実行時は公開event queryを`--limit 1 --content none`でprobeし、completionのgeneration IDがcacheと一致した場合だけcacheを使う。probeが失敗した場合は古いcacheへ黙ってfallbackせず、通常のsource readへ進む。

この方式は共有manifestやSQLite lockを必要としない。複数processが同時にcacheを更新しても、各fileのatomic renameにより不完全payloadを公開せず、最悪の場合は次回に再構築する。

### 3. cache payloadはreport用のcompact DTOに限定する

source adapterはraw mapやraw provider payloadではなく、session metadata、turn境界、prompt count、Toolのcanonical name・layer・status・timestamp、Skill evidence、およびreportに必要なwarningだけをsnapshotへ変換する。Toolの`Arguments`、prompt本文、activity全体、不要な`SourceRef`のraw path/lineはcache payloadへ含めない。

fresh read時のnormalizerはeventを一度だけdecodeし、同じeventのtext・tool・skill判定で同じnested JSONを再decodeしない。ctxはpage全体の`[]event`と`Raw map`を保持せず、lineからcompact eventを生成してassemblerへ渡す。assemblerが保持するのはactive turn、session metadata、dedupe用ID、および最終的な正規化turnだけとする。Codexは既存のline streamingを維持し、fileごとのcompact snapshotを生成する。

cacheから復元したDTOは、既存のaggregateが必要とする`usage.Turn`へ変換する。cache hitとfresh readでaggregateへ渡る意味情報を一致させるが、cache hitで利用できないraw引数をreport用途へ持ち込まない。

### 4. ctxのpage取得は上限を上げつつ、完全性検証を維持する

ctxの通常event queryは、現行の`10,000`からboundedな`100,000`へlimitを引き上げ、現在の履歴規模ではsubprocess起動を複数回から1回へ減らす。100,000件を超える場合はcursorで継続し、全pageのcompletion、同一generation、cursor進行、dedupe、およびterminal/truncated状態を従来どおり検証する。limitを上げる前提として、command stdoutは可能な経路でline streamingし、履歴全体を1つのraw byte bufferとして保持しない。

ctx event queryの順序をcanonical orderとしてassemblerへ渡す。これにより、全raw eventを後からsortするためのmemoryを不要にしながら、ctxが定義するtimestamp・sequence・event identityの決定的順序を維持する。

### 5. 期間filterはsnapshot利用後に適用する

Codexはcache miss時も可能な範囲で全fileの正規化済みsnapshotを作り、`--days`はsnapshotから返すturnを選ぶ段階で適用する。これにより同じfile cacheを全期間と任意の期間で共有できる。

ctxはcache済みcomplete generationを使う場合、全generation snapshotへlocal cutoffを適用する。cacheがなく`--days`が指定された場合は、現在の`--since`/`--until`付きpublic queryを使用し、初回の短期間実行を全履歴取得へ悪化させない。この範囲queryの結果はcomplete generation cacheとして保存せず、全期間queryが成功したときだけgeneration cacheを更新する。

### 6. writeはatomic、cache errorは結果から分離する

snapshotはtarget directory内のtemporary fileへ書き、flush・close後にrenameする。未完了・破損cacheはvalidationで除外する。既存の有効cacheは、新しいwriteが失敗しても残す。

cacheのread/write errorはsourceのvalidityやreportのstdoutを変更しない。必要なdiagnosticを出す場合もstderrだけに出し、`--json`のstdoutを汚染しない。cacheが使えない場合の結果は、同じsourceをcacheなしで処理した結果と一致させる。

## Risks / Trade-offs

- Codex fileが同じsizeと同じmtimeへ戻る稀な更新 → nanosecond mtimeとsizeを必須fingerprintとし、metadataを取得できない場合はcacheを使わない。content hashは毎回全fileを読むため初版では採用しない。
- Codex fileが読取中に追記される → 読取前後のstat一致を確認し、不一致のsnapshotをcacheへ公開しない。
- ctx generationがprobe直後に更新される → probeで取得したimmutable generationを1回のreport snapshotとみなし、full fetchではcursorがgenerationをpinする。page間でgenerationが変わった場合は既存どおりerrorにする。
- cache hitでもctx probeのsubprocessは1回必要 → full event enumerationを省略できるため、全履歴のJSONL取得より十分小さい。generation専用APIが追加された場合は将来置換できる。
- cache初回warmingはsource種類により時間が残る → Codexは初回に全fileを読む必要があり、ctxのcacheなし`--days`は現在の範囲queryを維持する。性能評価はcold/warmとall-time/`--days 1`を分けて行う。
- 正規化DTOのfield漏れ → cold readとwarm readのaggregate結果、human report、JSON reportをfixtureで比較し、parser version変更時にcacheを無効化する。
- local履歴由来のsession identityやpathがcacheに残る → cache directory/file permissionsを制限し、prompt本文・tool引数・raw payloadを保存しない。cacheはユーザーが削除可能な再生成データとして扱う。

## Migration Plan

1. 新しいversion付きcache directoryを作成し、既存のCodex/ctx sourceや既存のuser dataは変更しない。
2. 初回実行はcache missとして現行のsource readを完了し、成功したsnapshotだけをatomicに保存する。
3. warm readでcold readとの結果比較を行い、問題があればcache directoryを削除するかparser versionを更新してcacheを無効化する。
4. rollback時は旧binaryが新cacheを参照しないため、sourceのread-only動作へ戻る。cacheは手動削除可能で、migrationは不要とする。

## Open Questions

- なし。cacheのkey、完全性、期間filterの初回動作、および失敗時のfallbackは実装前に確定している。
