package router

import (
	"math"
	"sync"
)

// DoubleArrayTrie は高速な文字列マッチングを実現するデータ構造です。
// 各ノードは配列で表現され、base値とcheck値を使用して遷移を管理します。
// 静的なルートパターンの検索に特化しており、メモリ効率と検索速度を両立します。
type DoubleArrayTrie struct {
	base    []int32
	check   []int32
	handler []HandlerFunc
	size    int32
	mu      sync.RWMutex
}

// 定数定義
const (
	initialTrieSize       = 1024
	growthFactor          = 1.5
	baseOffset      int32 = 256
)

// newDoubleArrayTrie は新しいDoubleArrayTrieインスタンスを初期化して返します。
// 初期サイズで配列を確保し、未使用ノードを示す-1で初期化します。
func newDoubleArrayTrie() *DoubleArrayTrie {
	t := &DoubleArrayTrie{
		base:    make([]int32, initialTrieSize),
		check:   make([]int32, initialTrieSize),
		handler: make([]HandlerFunc, initialTrieSize),
		size:    1,
	}

	t.base[0] = baseOffset
	return t
}

// Add は新しいパスとそのハンドラをトライ木に追加します。
// パスを文字単位で分解し、各文字に対応するノードを作成または更新します。
// 既存のパスと競合する場合は、base値を調整して衝突を回避します。
func (t *DoubleArrayTrie) Add(path string, handler HandlerFunc) error {
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

	curr := int32(0)

	for i := range path {
		c := path[i]
		next := t.base[curr] + int32(c)
		if next >= int32(len(t.check)) {
			if err := t.expand(next + 1); err != nil {
				return err
			}
		}

		if t.check[next] != curr {
			baseCandidate := t.findBase([]byte(path[i:]))
			if baseCandidate <= 0 {
				return &RouterError{Code: ErrInternalError, Message: "failed to find base value"}
			}

			t.base[curr] = baseCandidate
			next = t.base[curr] + int32(c)
		}

		t.check[next] = curr
		curr = next
	}

	t.handler[curr] = handler
	if curr >= t.size {
		t.size = curr + 1
	}

	return nil
}

// Search はパスに一致するハンドラを検索します。
// パスを先頭から1文字ずつ辿り、対応するノードを探索します。
// 完全一致した場合のみハンドラを返し、途中で不一致や未使用ノードに到達した場合はnilを返します。
func (t *DoubleArrayTrie) Search(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := int32(0)
	for i := range path {
		next := t.base[curr] + int32(path[i])
		if next < 0 || next >= int32(len(t.check)) || t.check[next] != curr {
			return nil
		}
		curr = next
	}
	return t.handler[curr]
}

// findBaseValue は指定された文字セットに対して適切なbase値を探索します。
// 文字セット内のすべての文字に対して、競合のない位置を見つけるまで探索を続けます。
// 見つかったbase値に対して、すべての文字の遷移先が未使用である必要があります。
func (t *DoubleArrayTrie) findBase(suffix []byte) int32 {
	// suffix 内の最大文字コードを取得
	var maxVal int32 = 0
	for _, c := range suffix {
		if int32(c) > maxVal {
			maxVal = int32(c)
		}
	}
	base := int32(1)
OUTER:
	for {
		// 必要なサイズ: base + maxVal + 1
		requiredSize := base + maxVal + 1
		if requiredSize > int32(len(t.check)) {
			if err := t.expand(requiredSize); err != nil {
				return -1
			}
		}
		collision := false
		for _, c := range suffix {
			next := base + int32(c)
			if t.check[next] != 0 {
				collision = true
				break
			}
		}
		if !collision {
			break OUTER
		}
		base++
	}
	return base
}

// relocate は親ノードのbase値を変更し、子ノードを再配置します。
// 競合が発生した場合に呼び出され、新しいbase値を見つけて子ノードを移動します。
func (t *DoubleArrayTrie) relocate(parent int, newChar byte) {
	// 現在の子ノードの文字を収集
	chars := make([]byte, 0, 8)
	base := t.base[parent]
	for c := byte(0); c < 255; c++ {
		pos := int(base) + int(c)
		if pos < len(t.check) && t.check[pos] == int32(parent) {
			chars = append(chars, c)
		}
	}
	chars = append(chars, newChar)

	// 新しいbase値を探索
	newBase := t.findBase(chars)
	oldBase := t.base[parent]
	t.base[parent] = newBase

	// 既存の子ノードを新しい位置に移動
	for _, c := range chars {
		if c == newChar {
			continue
		}
		oldPos := int(oldBase) + int(c)
		newPos := int(newBase) + int(c)

		t.base[newPos] = t.base[oldPos]
		t.check[newPos] = t.check[oldPos]
		t.handler[newPos] = t.handler[oldPos]

		// 古い位置をクリア
		t.base[oldPos] = 0
		t.check[oldPos] = -1
		t.handler[oldPos] = nil

		// 子ノードの親参照を更新
		for c := byte(0); c < 255; c++ {
			pos := int(t.base[oldPos]) + int(c)
			if pos < len(t.check) && t.check[pos] == int32(oldPos) {
				t.check[pos] = int32(newPos)
			}
		}
	}
}

// expand はトライ木の配列サイズを拡張します。
// 新しいサイズは現在のサイズの倍数で、check配列は-1で初期化されます。
func (t *DoubleArrayTrie) expand(size int32) error {
	newSize := int32(math.Max(float64(len(t.base))*growthFactor, float64(size)))
	if newSize > 1<<30 {
		return &RouterError{Code: ErrInternalError, Message: "trie size limit exceeded"}
	}
	newBase := make([]int32, newSize)
	newCheck := make([]int32, newSize)
	newHandler := make([]HandlerFunc, newSize)
	copy(newBase, t.base)
	copy(newCheck, t.check)
	copy(newHandler, t.handler)
	t.base = newBase
	t.check = newCheck
	t.handler = newHandler
	return nil
}
