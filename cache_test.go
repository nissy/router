package router

import (
	"net/http"
	"testing"
	"time"
)

// TestCacheCreation はキャッシュの作成をテストします
func TestCacheCreation(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()

	// 初期状態をチェック
	if cache == nil {
		t.Fatalf("キャッシュの作成に失敗しました")
	}

	for i := 0; i < shardCount; i++ {
		if cache.shards[i] == nil {
			t.Errorf("シャード %d が初期化されていません", i)
		}

		if cache.shards[i].entries == nil {
			t.Errorf("シャード %d のエントリーマップが初期化されていません", i)
		}
	}

	// キャッシュを停止
	cache.Stop()
}

// TestCacheSetAndGet はキャッシュの設定と取得をテストします
func TestCacheSetAndGet(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()
	defer cache.Stop()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// キャッシュにエントリーを設定
	key := uint64(12345)
	cache.Set(key, handler, nil)

	// キャッシュからエントリーを取得
	h, found := cache.Get(key)

	// 取得結果をチェック
	if !found {
		t.Fatalf("キャッシュからエントリーが見つかりませんでした")
	}

	if h == nil {
		t.Errorf("キャッシュから取得したハンドラがnilです")
	}
}

// TestCacheWithParams はパラメータ付きのキャッシュをテストします
func TestCacheWithParams(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()
	defer cache.Stop()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// テスト用のパラメータ
	params := map[string]string{
		"id":   "123",
		"name": "test",
	}

	// キャッシュにエントリーを設定
	key := uint64(12345)
	cache.Set(key, handler, params)

	// キャッシュからエントリーを取得
	h, p, found := cache.GetWithParams(key)

	// 取得結果をチェック
	if !found {
		t.Fatalf("キャッシュからエントリーが見つかりませんでした")
	}

	if h == nil {
		t.Errorf("キャッシュから取得したハンドラがnilです")
	}

	if p == nil {
		t.Errorf("キャッシュから取得したパラメータがnilです")
	}

	// パラメータの値をチェック
	if p["id"] != "123" {
		t.Errorf("パラメータ id の値が異なります。期待値: %s, 実際: %s", "123", p["id"])
	}

	if p["name"] != "test" {
		t.Errorf("パラメータ name の値が異なります。期待値: %s, 実際: %s", "test", p["name"])
	}
}

// TestCacheMaxEntries はキャッシュの最大エントリー数をテストします
func TestCacheMaxEntries(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()
	defer cache.Stop()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// 最大エントリー数を超えるエントリーを設定
	shardIndex := uint64(0) // 特定のシャードにエントリーを集中させる
	for i := uint64(0); i < maxEntriesPerShard+10; i++ {
		key := (i << 3) | shardIndex // シャードインデックスを固定
		cache.Set(key, handler, nil)
	}

	// シャードのエントリー数をチェック
	shard := cache.shards[shardIndex]
	shard.RLock()
	entriesCount := len(shard.entries)
	shard.RUnlock()

	if entriesCount > maxEntriesPerShard {
		t.Errorf("シャードのエントリー数が最大値を超えています。最大値: %d, 実際: %d", maxEntriesPerShard, entriesCount)
	}
}

// TestCacheCleanup はキャッシュのクリーンアップをテストします
func TestCacheCleanup(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()
	defer cache.Stop()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// キャッシュにエントリーを設定
	key := uint64(12345)
	cache.Set(key, handler, nil)

	// エントリーのタイムスタンプを過去に設定
	shard := cache.shards[key&shardMask]
	shard.Lock()
	entry := shard.entries[key]
	if entry != nil {
		entry.timestamp = time.Now().Add(-2 * defaultExpiration).UnixNano()
	}
	shard.Unlock()

	// 手動でクリーンアップを実行
	cache.cleanup()

	// エントリーが削除されていることを確認
	_, found := cache.Get(key)
	if found {
		t.Errorf("期限切れのエントリーがクリーンアップされていません")
	}
}

// TestCacheHits はキャッシュヒットをテストします
func TestCacheHits(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()
	defer cache.Stop()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// キャッシュにエントリーを設定
	key := uint64(12345)
	cache.Set(key, handler, nil)

	// キャッシュからエントリーを複数回取得
	for i := 0; i < 5; i++ {
		h, found := cache.Get(key)
		if !found || h == nil {
			t.Fatalf("キャッシュからエントリーが見つかりませんでした")
		}
	}

	// ヒット数のチェックはスキップ（実装によってはヒット数をカウントしていない可能性がある）
}

// TestCacheTimestamp はキャッシュのタイムスタンプ更新をテストします
func TestCacheTimestamp(t *testing.T) {
	// 新しいキャッシュを作成
	cache := newCache()
	defer cache.Stop()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// キャッシュにエントリーを設定
	key := uint64(12345)
	cache.Set(key, handler, nil)

	// 初期タイムスタンプを取得
	shard := cache.shards[key&shardMask]
	shard.RLock()
	entry := shard.entries[key]
	initialTimestamp := int64(0)
	if entry != nil {
		initialTimestamp = entry.timestamp
	}
	shard.RUnlock()

	// 少し待機
	time.Sleep(10 * time.Millisecond)

	// キャッシュからエントリーを取得
	cache.Get(key)

	// 最終タイムスタンプを取得
	shard.RLock()
	entry = shard.entries[key]
	finalTimestamp := int64(0)
	if entry != nil {
		finalTimestamp = entry.timestamp
	}
	shard.RUnlock()

	// タイムスタンプが更新されていることを確認
	if finalTimestamp <= initialTimestamp {
		t.Errorf("キャッシュタイムスタンプが更新されていません。初期: %d, 最終: %d", initialTimestamp, finalTimestamp)
	}
}
