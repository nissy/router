package router

import (
	"math"
	"sync"
)

// DoubleArrayTrie は高速な文字列マッチングを実現するデータ構造です。
// 各ノードは配列で表現され、base値とcheck値を使用して遷移を管理します。
// 静的なルートパターンの検索に特化しており、メモリ効率と検索速度を両立します。
type DoubleArrayTrie struct {
	base    []int32       // 各ノードの基準値。子ノードへの遷移に使用
	check   []int32       // 親子関係の検証に使用。0は未使用を示す
	handler []HandlerFunc // 各ノードに関連付けられたハンドラ関数
	size    int32         // 使用中のノード数
	mu      sync.RWMutex  // 並行アクセスからの保護用ミューテックス
}

// 定数定義
const (
	initialTrieSize = 1024       // トライ木の初期サイズ
	growthFactor    = 1.5        // 拡張時の成長係数
	baseOffset      = int32(256) // base配列のオフセット値
	rootNode        = int32(0)   // ルートノードのインデックス
)

// newDoubleArrayTrie は新しいDoubleArrayTrieインスタンスを初期化して返します。
// 初期サイズで配列を確保し、ルートノードのbase値を設定します。
func newDoubleArrayTrie() *DoubleArrayTrie {
	t := &DoubleArrayTrie{
		base:    make([]int32, initialTrieSize),
		check:   make([]int32, initialTrieSize),
		handler: make([]HandlerFunc, initialTrieSize),
		size:    1, // ルートノードが存在するため1から開始
	}

	// ルートノードのbase値を設定
	t.base[rootNode] = baseOffset
	return t
}

// Add は新しいパスとそのハンドラをトライ木に追加します。
// パスを文字単位で分解し、各文字に対応するノードを作成または更新します。
// 既に同じパスが登録されている場合はエラーを返します。
func (t *DoubleArrayTrie) Add(path string, handler HandlerFunc) error {
	// 入力検証
	if path == "" {
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "empty path",
		}
	}
	if handler == nil {
		return &RouterError{
			Code:    ErrNilHandler,
			Message: "nil handler",
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 既存のパスかどうかをチェック
	existingHandler := t.Search(path)
	if existingHandler != nil {
		// Router.Handle メソッドで既に重複チェックを行っているため、
		// ここでのエラーは通常発生しないはずですが、安全のために実装しています。
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "duplicate static route: " + path,
		}
	}

	// ルートノードから開始
	currentNode := rootNode

	// パスの各文字に対してノードを作成または更新
	for i := range path {
		char := path[i]
		nextNode := t.base[currentNode] + int32(char)

		// 配列サイズの拡張が必要な場合
		if nextNode >= int32(len(t.check)) {
			if err := t.expand(nextNode + 1); err != nil {
				return err
			}
		}

		// 親子関係が不一致の場合（未使用または別の親を持つ）
		if t.check[nextNode] != currentNode {
			// 新しいbase値を探索
			newBase := t.findBase([]byte(path[i:]))
			if newBase <= 0 {
				return &RouterError{Code: ErrInternalError, Message: "failed to find base value"}
			}

			// 親ノードのbase値を更新
			t.base[currentNode] = newBase
			nextNode = newBase + int32(char)
		}

		// 親子関係を設定
		t.check[nextNode] = currentNode
		currentNode = nextNode
	}

	// パスの終端にハンドラを設定
	t.handler[currentNode] = handler

	// 使用中のノード数を更新
	if currentNode >= t.size {
		t.size = currentNode + 1
	}

	return nil
}

// Search はパスに一致するハンドラを検索します。
// パスを先頭から1文字ずつ辿り、対応するノードを探索します。
func (t *DoubleArrayTrie) Search(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	// ルートノードから開始
	currentNode := rootNode

	// パスの各文字に対してノードを辿る
	for i := range path {
		nextNode := t.base[currentNode] + int32(path[i])

		// 範囲外または親子関係が不一致の場合は不一致
		if nextNode < 0 || nextNode >= int32(len(t.check)) || t.check[nextNode] != currentNode {
			return nil
		}

		currentNode = nextNode
	}

	// パスの終端に関連付けられたハンドラを返す
	return t.handler[currentNode]
}

// findBase は指定された文字セットに対して適切なbase値を探索します。
// 文字セット内のすべての文字に対して、競合のない位置を見つけるまで探索します。
func (t *DoubleArrayTrie) findBase(suffix []byte) int32 {
	// suffix内の最大文字コードを取得
	var maxCharCode int32 = 0
	for _, char := range suffix {
		if int32(char) > maxCharCode {
			maxCharCode = int32(char)
		}
	}

	// base値の候補を1から開始
	baseCandidate := int32(1)

	// 競合のないbase値を見つけるまで探索
	for {
		// 必要な配列サイズを計算
		requiredSize := baseCandidate + maxCharCode + 1

		// 配列サイズの拡張が必要な場合
		if requiredSize > int32(len(t.check)) {
			if err := t.expand(requiredSize); err != nil {
				return -1
			}
		}

		// 競合チェック
		hasCollision := false
		for _, char := range suffix {
			nextPos := baseCandidate + int32(char)
			if t.check[nextPos] != 0 { // 既に使用されている位置
				hasCollision = true
				break
			}
		}

		// 競合がなければこのbase値を使用
		if !hasCollision {
			return baseCandidate
		}

		// 次の候補を試す
		baseCandidate++
	}
}

// expand はトライ木の配列サイズを拡張します。
// 新しいサイズは現在のサイズの倍数で計算されます。
func (t *DoubleArrayTrie) expand(requiredSize int32) error {
	// 新しいサイズを計算（現在のサイズの倍数か、必要なサイズのいずれか大きい方）
	newSize := int32(math.Max(float64(len(t.base))*growthFactor, float64(requiredSize)))

	// サイズ上限チェック
	if newSize > 1<<30 { // 約10億ノード
		return &RouterError{Code: ErrInternalError, Message: "trie size limit exceeded"}
	}

	// 新しい配列を作成
	newBase := make([]int32, newSize)
	newCheck := make([]int32, newSize)
	newHandler := make([]HandlerFunc, newSize)

	// 既存データをコピー
	copy(newBase, t.base)
	copy(newCheck, t.check)
	copy(newHandler, t.handler)

	// 新しい配列を設定
	t.base = newBase
	t.check = newCheck
	t.handler = newHandler

	return nil
}
