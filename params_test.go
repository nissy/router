package router

import (
	"context"
	"testing"
)

// TestParamsCreation はParamsの作成をテストします
func TestParamsCreation(t *testing.T) {
	// 新しいParamsを作成
	params := NewParams()

	// 初期状態をチェック
	if params == nil {
		t.Fatalf("Paramsの作成に失敗しました")
	}

	if params.Len() != 0 {
		t.Errorf("Paramsの初期サイズが異なります。期待値: %d, 実際: %d", 0, params.Len())
	}

	// パラメータをプールに返却
	PutParams(params)
}

// TestParamsAddAndGet はパラメータの追加と取得をテストします
func TestParamsAddAndGet(t *testing.T) {
	// 新しいParamsを作成
	params := NewParams()

	// パラメータを追加
	params.Add("id", "123")
	params.Add("name", "test")

	// パラメータの数をチェック
	if params.Len() != 2 {
		t.Errorf("パラメータの数が異なります。期待値: %d, 実際: %d", 2, params.Len())
	}

	// パラメータの値をチェック
	if val, ok := params.Get("id"); !ok || val != "123" {
		t.Errorf("パラメータ id の値が異なります。期待値: %s, 実際: %s", "123", val)
	}

	if val, ok := params.Get("name"); !ok || val != "test" {
		t.Errorf("パラメータ name の値が異なります。期待値: %s, 実際: %s", "test", val)
	}

	// 存在しないパラメータをチェック
	if _, ok := params.Get("notfound"); ok {
		t.Errorf("存在しないパラメータが見つかりました")
	}

	// パラメータをプールに返却
	PutParams(params)
}

// TestParamsReset はパラメータのリセットをテストします
func TestParamsReset(t *testing.T) {
	// 新しいParamsを作成
	params := NewParams()

	// パラメータを追加
	params.Add("id", "123")
	params.Add("name", "test")

	// パラメータの数をチェック
	if params.Len() != 2 {
		t.Errorf("パラメータの数が異なります。期待値: %d, 実際: %d", 2, params.Len())
	}

	// パラメータをリセット
	params.reset()

	// リセット後のパラメータの数をチェック
	if params.Len() != 0 {
		t.Errorf("リセット後のパラメータの数が異なります。期待値: %d, 実際: %d", 0, params.Len())
	}

	// パラメータをプールに返却
	PutParams(params)
}

// TestParamsPool はパラメータプールをテストします
func TestParamsPool(t *testing.T) {
	// 複数のParamsを作成して返却
	for range make([]struct{}, 10) {
		params := NewParams()
		params.Add("id", "123")
		PutParams(params)
	}

	// プールから再利用されたParamsを取得
	params := NewParams()

	// 再利用されたParamsが空であることを確認
	if params.Len() != 0 {
		t.Errorf("再利用されたParamsが空ではありません。サイズ: %d", params.Len())
	}

	// パラメータをプールに返却
	PutParams(params)
}

// TestParamsCapacity はパラメータの容量をテストします
func TestParamsCapacity(t *testing.T) {
	// 新しいParamsを作成
	params := NewParams()

	// 多数のパラメータを追加
	for i := 0; i < 100; i++ {
		params.Add("key"+string(rune('0'+i%10)), "value"+string(rune('0'+i%10)))
	}

	// パラメータの数をチェック
	if params.Len() != 100 {
		t.Errorf("パラメータの数が異なります。期待値: %d, 実際: %d", 100, params.Len())
	}

	// パラメータをプールに返却
	PutParams(params)
}

// TestContextWithParams はコンテキストにパラメータを追加するテストです
func TestContextWithParams(t *testing.T) {
	// 新しいParamsを作成
	params := NewParams()
	params.Add("id", "123")

	// コンテキストにパラメータを追加
	ctx := context.Background()
	ctx = contextWithParams(ctx, params)

	// コンテキストからパラメータを取得
	retrievedParams := GetParams(ctx)

	// パラメータをチェック
	if retrievedParams == nil {
		t.Fatalf("コンテキストからパラメータを取得できませんでした")
	}

	if val, ok := retrievedParams.Get("id"); !ok || val != "123" {
		t.Errorf("パラメータ id の値が異なります。期待値: %s, 実際: %s", "123", val)
	}
}

// TestGetParamsWithNilContext はnilコンテキストからのパラメータ取得をテストします
func TestGetParamsWithNilContext(t *testing.T) {
	// nilコンテキストの代わりにcontext.TODO()を使用
	params := GetParams(context.TODO())

	// パラメータが新しく作成されていることを確認
	if params == nil {
		t.Fatalf("空のコンテキストからパラメータを取得できませんでした")
	}

	if params.Len() != 0 {
		t.Errorf("新しく作成されたパラメータが空ではありません。サイズ: %d", params.Len())
	}
}

// TestGetParamsWithEmptyContext は空のコンテキストからのパラメータ取得をテストします
func TestGetParamsWithEmptyContext(t *testing.T) {
	// 空のコンテキストからパラメータを取得
	ctx := context.Background()
	params := GetParams(ctx)

	// パラメータが新しく作成されていることを確認
	if params == nil {
		t.Fatalf("空のコンテキストからパラメータを取得できませんでした")
	}

	if params.Len() != 0 {
		t.Errorf("新しく作成されたパラメータが空ではありません。サイズ: %d", params.Len())
	}
}
