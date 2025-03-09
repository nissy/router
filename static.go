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

// Add はパスとハンドラ関数をトライ木に追加します。
// 既に同じパスが登録されている場合はエラーを返します。
func (t *DoubleArrayTrie) Add(path string, h HandlerFunc) error {
	if len(path) == 0 {
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "empty path is not allowed",
		}
	}

	// nilハンドラのチェック
	if h == nil {
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "nil handler is not allowed",
		}
	}

	// 既存のパスかどうかをチェック（ロックの外で実行）
	var existingHandler HandlerFunc
	t.mu.RLock()
	existingHandler = t.searchWithoutLock(path)
	t.mu.RUnlock()

	if existingHandler != nil {
		// Router.Handle メソッドで既に重複チェックを行っているため、
		// ここでのエラーは通常発生しないはずですが、安全のために実装しています。
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "duplicate static route: " + path,
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// パスを文字単位で処理
	currentNode := rootNode
	for i := 0; i < len(path); i++ {
		c := path[i]
		baseVal := t.base[currentNode]

		// 現在のノードにまだ子ノードがない場合
		if baseVal == 0 {
			// 新しいbaseの値を計算
			nextNode := currentNode + int32(c) + 1

			// 必要に応じてbase配列を拡張
			if nextNode >= int32(len(t.base)) {
				// 配列サイズを計算（最低でも2倍、必要に応じてさらに大きく）
				newSize := int32(len(t.base)) * 2
				if nextNode >= newSize {
					newSize = nextNode + 1024 // 余裕を持たせる
				}

				// 配列を拡張
				if err := t.expand(newSize); err != nil {
					return err
				}
			}

			// 新しい遷移を設定
			t.base[currentNode] = nextNode - int32(c)
			t.check[nextNode] = currentNode
			currentNode = nextNode
		} else {
			// 既存のbaseの値を使用して次のノードを計算
			nextNode := baseVal + int32(c)

			// 必要に応じてbase配列を拡張
			if nextNode >= int32(len(t.base)) {
				// 配列サイズを計算（最低でも2倍、必要に応じてさらに大きく）
				newSize := int32(len(t.base)) * 2
				if nextNode >= newSize {
					newSize = nextNode + 1024 // 余裕を持たせる
				}

				// 配列を拡張
				if err := t.expand(newSize); err != nil {
					return err
				}
			}

			// 遷移先が未使用か確認
			if t.check[nextNode] == 0 {
				// 未使用なら設定
				t.check[nextNode] = currentNode
				currentNode = nextNode
			} else if t.check[nextNode] == currentNode {
				// 既に同じ親から同じ文字で遷移する場合は問題なし
				currentNode = nextNode
			} else {
				// 衝突が発生した場合は、新しいbaseの値を探す
				newBase := t.findBase([]byte(path[i:]))
				if newBase < 0 {
					return &RouterError{
						Code:    ErrInternalError,
						Message: "failed to find new base value",
					}
				}

				// 既存の子ノードを新しい位置に移動
				oldBase := t.base[currentNode]
				for ch := byte(0); ch < 128; ch++ { // ASCII文字のみサポート
					oldNext := oldBase + int32(ch)
					if oldNext < int32(len(t.check)) && t.check[oldNext] == currentNode {
						// 既存の子ノードを見つけた
						newNext := newBase + int32(ch)

						// 必要に応じて配列を拡張
						if newNext >= int32(len(t.base)) {
							newSize := int32(len(t.base)) * 2
							if newNext >= newSize {
								newSize = newNext + 1024
							}
							if err := t.expand(newSize); err != nil {
								return err
							}
						}

						// 子ノードを新しい位置に移動
						t.base[newNext] = t.base[oldNext]
						t.check[newNext] = currentNode

						// 古い位置をクリア
						t.check[oldNext] = 0
					}
				}

				// 現在のノードのbaseを更新
				t.base[currentNode] = newBase

				// 新しい遷移を追加
				nextNode = newBase + int32(c)
				t.check[nextNode] = currentNode
				currentNode = nextNode
			}
		}
	}

	// 終端ノードにハンドラを設定
	if int(currentNode) >= len(t.handler) {
		// ハンドラ配列も拡張
		newHandlers := make([]HandlerFunc, len(t.base))
		copy(newHandlers, t.handler)
		t.handler = newHandlers
	}
	t.handler[currentNode] = h

	// 使用中のノード数を更新
	if currentNode >= t.size {
		t.size = currentNode + 1
	}

	return nil
}

// searchWithoutLock はロックなしでパスを検索します。
// 内部使用のみを想定しています。
func (t *DoubleArrayTrie) searchWithoutLock(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}

	// ルートノードから開始
	currentNode := rootNode

	// パスを文字単位で処理
	for i := 0; i < len(path); i++ {
		c := path[i]

		// 次のノードを計算
		if t.base[currentNode] == 0 {
			return nil // マッチするパスなし
		}

		nextNode := t.base[currentNode] + int32(c)

		// 配列の範囲外チェック
		if nextNode >= int32(len(t.check)) || nextNode < 0 {
			return nil
		}

		// 遷移が有効かチェック
		if t.check[nextNode] != currentNode {
			return nil // マッチするパスなし
		}

		currentNode = nextNode
	}

	// 終端ノードにハンドラがあるかチェック
	if int(currentNode) < len(t.handler) && t.handler[currentNode] != nil {
		return t.handler[currentNode]
	}

	return nil
}

// Search はパスに一致するハンドラ関数を検索します。
// 一致するパスが見つからない場合はnilを返します。
func (t *DoubleArrayTrie) Search(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.searchWithoutLock(path)
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
	if t.handler != nil {
		copy(newHandler, t.handler)
	}

	// 新しい配列を設定
	t.base = newBase
	t.check = newCheck
	t.handler = newHandler

	return nil
}
