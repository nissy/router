package router

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestDoubleArrayTrieCreation はDoubleArrayTrieの作成をテストします
func TestDoubleArrayTrieCreation(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// 初期状態をチェック
	if trie.size != 1 {
		t.Errorf("トライのサイズが異なります。期待値: %d, 実際: %d", 1, trie.size)
	}

	if len(trie.base) < initialTrieSize {
		t.Errorf("トライのベース配列のサイズが小さすぎます。期待値: %d以上, 実際: %d", initialTrieSize, len(trie.base))
	}

	if len(trie.check) < initialTrieSize {
		t.Errorf("トライのチェック配列のサイズが小さすぎます。期待値: %d以上, 実際: %d", initialTrieSize, len(trie.check))
	}

	if len(trie.handler) < initialTrieSize {
		t.Errorf("トライのハンドラ配列のサイズが小さすぎます。期待値: %d以上, 実際: %d", initialTrieSize, len(trie.handler))
	}

	if trie.base[rootNode] != baseOffset {
		t.Errorf("ルートノードのベース値が異なります。期待値: %d, 実際: %d", baseOffset, trie.base[rootNode])
	}
}

// TestStaticRouteAdditionAndSearch は静的ルートの追加と検索をテストします
func TestStaticRouteAdditionAndSearch(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// テスト用のハンドラ関数
	handler1 := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	handler2 := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// ルートを追加
	if err := trie.Add("/", handler1); err != nil {
		t.Fatalf("ルートパスの追加に失敗しました: %v", err)
	}

	if err := trie.Add("/users", handler2); err != nil {
		t.Fatalf("ユーザーパスの追加に失敗しました: %v", err)
	}

	// ルートを検索
	h1 := trie.Search("/")
	if h1 == nil {
		t.Fatalf("ルートパスが見つかりませんでした")
	}

	h2 := trie.Search("/users")
	if h2 == nil {
		t.Fatalf("ユーザーパスが見つかりませんでした")
	}

	// 存在しないパスを検索
	h3 := trie.Search("/notfound")
	if h3 != nil {
		t.Fatalf("存在しないパスが見つかりました")
	}
}

// TestDuplicateRouteAddition は重複ルートの追加をテストします
func TestDuplicateRouteAddition(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// 最初のルートを追加
	if err := trie.Add("/users", handler); err != nil {
		t.Fatalf("ルートの追加に失敗しました: %v", err)
	}

	// 同じルートを再度追加
	err := trie.Add("/users", handler)
	if err == nil {
		t.Fatalf("重複ルートの追加が成功しました")
	}

	// エラーの種類をチェック
	routerErr, ok := err.(*RouterError)
	if !ok {
		t.Fatalf("期待されるエラータイプではありません: %T", err)
	}

	if routerErr.Code != ErrInvalidPattern {
		t.Errorf("エラーコードが異なります。期待値: %d, 実際: %d", ErrInvalidPattern, routerErr.Code)
	}
}

// TestLongPathAddition は長いパスの追加をテストします
func TestLongPathAddition(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// 長いパスを追加
	longPath := "/users/profile/settings/notifications/email/daily"
	if err := trie.Add(longPath, handler); err != nil {
		t.Fatalf("長いパスの追加に失敗しました: %v", err)
	}

	// パスを検索
	h := trie.Search(longPath)
	if h == nil {
		t.Fatalf("長いパスが見つかりませんでした")
	}
}

// TestTrieExpansion はトライ木の配列拡張をテストします
func TestTrieExpansion(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// 複数のパスを追加して配列の拡張をテスト
	paths := []string{
		"/patho" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"/pathx" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"/pathy" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"/pathz" + fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	for _, path := range paths {
		if err := trie.Add(path, handler); err != nil {
			t.Fatalf("ルートの追加に失敗しました: %v", err)
		}
	}

	// 追加したパスを検索
	for _, path := range paths {
		h := trie.Search(path)
		if h == nil {
			t.Errorf("パス %s が見つかりませんでした", path)
		}
	}
}

// TestEmptyPathAddition は空のパスの追加をテストします
func TestEmptyPathAddition(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// 空のパスを追加
	err := trie.Add("", handler)
	if err == nil {
		t.Fatalf("空のパスの追加が成功しました")
	}

	// エラーの種類をチェック
	routerErr, ok := err.(*RouterError)
	if !ok {
		t.Fatalf("期待されるエラータイプではありません: %T", err)
	}

	if routerErr.Code != ErrInvalidPattern {
		t.Errorf("エラーコードが異なります。期待値: %d, 実際: %d", ErrInvalidPattern, routerErr.Code)
	}
}

// TestNilHandlerAddition はnilハンドラの追加をテストします
func TestNilHandlerAddition(t *testing.T) {
	// 新しいDoubleArrayTrieを作成
	trie := newDoubleArrayTrie()

	// nilハンドラを追加
	err := trie.Add("/test-nil-handler", nil)

	// エラーが発生することを確認
	if err == nil {
		t.Fatalf("nilハンドラの追加が成功しました")
	}

	// エラータイプを確認
	routerErr, ok := err.(*RouterError)
	if !ok {
		t.Fatalf("期待されるエラータイプではありません: %T", err)
	}

	// エラーコードを確認
	if routerErr.Code != ErrInvalidPattern {
		t.Errorf("エラーコードが異なります。期待値: %d, 実際: %d", ErrInvalidPattern, routerErr.Code)
	}

	// エラーメッセージを確認
	expectedMsg := "nil handler is not allowed"
	if routerErr.Message != expectedMsg {
		t.Errorf("エラーメッセージが異なります。期待値: %s, 実際: %s", expectedMsg, routerErr.Message)
	}
}
