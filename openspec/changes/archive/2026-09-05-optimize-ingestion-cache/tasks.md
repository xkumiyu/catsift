## 1. Cache基盤

- [x] 1.1 `os.UserCacheDir()`配下のversion付き`catsift` cache directory、source namespace、hash化keyを実装し、Codexとctxの同一名scopeが衝突しないことをcache基盤のunit testで確認する
- [x] 1.2 cache envelopeのschema version、source kind、scope、source revision、parser version、complete markerを実装し、version・scope・revision不一致をcache missとして扱うunit testで確認する
- [x] 1.3 temporary fileへのflush・close後のatomic rename、`0700` directory、`0600` file、破損・未完了cacheの無視を実装し、書込中断後も既存の有効cacheが残るtestで確認する

## 2. Compact normalized snapshot

- [x] 2.1 Codexとctxのreport生成に必要なsession・turn・Tool・Skill・warningだけを表すcache DTOを定義し、raw prompt本文、tool引数、raw provider payload、不要なsource位置を保存しないserialization testで確認する
- [x] 2.2 cache DTOから既存の`usage.Turn`とreport inputを復元する処理を実装し、cold readとwarm readでstats・tools・skillsのaggregate結果とJSON値が一致するfixture testで確認する
- [x] 2.3 cache read/write failureをsource処理とreport stdoutから分離し、cacheが利用できない場合もcacheなしと同じ結果になり`--json` stdoutが有効なJSONのままであるtestで確認する

## 3. Codex streamingとfile cache

- [x] 3.1 Codexのfresh readでeventごとのpayload decodeとnormalizationを一度に行い、履歴全体のraw payloadを保持せずcompact snapshotへ渡す処理を実装し、既存Codex ingestion testと大きなsynthetic JSONLのstreaming testが通ることを確認する
- [x] 3.2 Codexのsession fileごとにsize・nanosecond mtimeを含むsource revisionを判定し、未変更fileのcache hit、新規・変更・truncate fileのcache miss、削除fileのcache除外を実装し、file操作を含むintegration testで確認する
- [x] 3.3 Codexのcache missでは全期間の正規化済みfile snapshotを作成し、`--days N`をsnapshot取得後に適用する処理を実装し、`--days 1`と全期間のfresh/warm結果が一致するtestで確認する

## 4. ctx streamingとpage取得

- [x] 4.1 ctx JSONL page parserをline単位のcompact event処理へ変更し、page全体のraw event mapと履歴全体の後段sortを保持せずactive turn・session metadata・dedupe IDだけで処理し、複数Agent・複数pageの既存testが通ることを確認する
- [x] 4.2 ctx event queryのbounded limitを`100,000`へ変更し、100,000件超ではcursor継続、completion、terminal/truncated、generation一致、cursor進行を維持するtestで確認する
- [x] 4.3 ctx command stdoutを可能な経路でline streamingし、full historyのsingle invocationとcursor paginationの双方で不完全入力やgeneration変更を成功結果として扱わないtestで確認する

## 5. ctx generation cache

- [x] 5.1 ctx data rootごとのcomplete generation snapshotを保存し、cache miss時は全page完了後だけatomicに公開する処理を実装し、途中EOF・破損・generation変更でcacheが更新されないtestで確認する
- [x] 5.2 cache hit判定用に`ctx list events --limit 1 --content none`のcompletionからgeneration IDをprobeし、同一generationだけを再利用して異なるgenerationを再取得するtestで確認する
- [x] 5.3 complete generation cacheへlocal `--days N` filterを適用し、cacheなしの`--days`では既存の`--since`/`--until` queryを維持してpartial resultをcomplete cacheとして保存しないtestで確認する

## 6. CLI統合と互換性

- [x] 6.1 `codex.Load`と`ctx.Load`へcache-aware ingestionを接続し、既存のCLI option、source scope、Agent、session、warningの契約を変更せずに`stats`・`tools`・`skills`から利用できるintegration testで確認する
- [x] 6.2 cacheなし・cold cache・warm cache・無効cacheについてhuman-readable report、JSON report、`--strict`、`--unused`、`--strict-input`の結果とstderr/stdout分離を比較するintegration testで確認する
- [x] 6.3 Codex home、ctx data root、provider-owned履歴が読み込み前後で変更されず、外部networkを利用しないread-only testで確認する

## 7. 性能検証と完了確認

- [x] 7.1 repository-localのsynthetic fixtureでCodex/ctxのcold・warm、全期間・`--days 1`のbenchmarkを追加し、wall time・allocations・peak memoryの改善を記録してcache hitが全履歴再解析を避けることを確認する
- [x] 7.2 `mise run check`を実行し、format、lint、vet、race test、build、npm wrapper test、package contentsがすべて成功することを確認する
- [x] 7.3 `openspec validate optimize-ingestion-cache --strict`を実行し、全artifactとdelta specが有効であることを確認する
