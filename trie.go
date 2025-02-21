package router

import (
	"sync"
)

const (
	initialTrieSize = 1024
	growthFactor    = 1.5
	// baseOffset を導入して、全体的なオフセットを確保する
	baseOffset int32 = 256
)

type DoubleArrayTrie struct {
	base    []int32
	check   []int32
	handler []HandlerFunc // 自作の HandlerFunc に統一
	size    int32
	mu      sync.RWMutex
}

func newDoubleArrayTrie() *DoubleArrayTrie {
	t := &DoubleArrayTrie{
		base:    make([]int32, initialTrieSize),
		check:   make([]int32, initialTrieSize),
		handler: make([]HandlerFunc, initialTrieSize),
		size:    1,
	}
	// ルートノード（index 0）の base を baseOffset に設定
	t.base[0] = baseOffset
	return t
}

func (t *DoubleArrayTrie) Add(path string, handler HandlerFunc) error {
	if path == "" {
		return &RouterError{Code: ErrInvalidPattern, Message: "empty path"}
	}
	if handler == nil {
		return &RouterError{Code: ErrNilHandler, Message: "nil handler"}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	curr := int32(0)

	for i := 0; i < len(path); i++ {
		c := path[i]
		next := t.base[curr] + int32(c)
		if next >= int32(len(t.check)) {
			if err := t.expand(next + 1); err != nil {
				return err
			}
		}

		if t.check[next] != curr {
			baseCandidate := t.findBase(curr, []byte(path[i:]))
			if baseCandidate <= 0 {
				return &RouterError{Code: ErrInternalError, Message: "failed to find base value"}
			}

			t.base[curr] = baseCandidate // ここで追加のオフセットは不要
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

func (t *DoubleArrayTrie) Search(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := int32(0)
	for i := 0; i < len(path); i++ {
		next := t.base[curr] + int32(path[i])
		if next < 0 || next >= int32(len(t.check)) || t.check[next] != curr {
			return nil
		}
		curr = next
	}
	return t.handler[curr]
}

func (t *DoubleArrayTrie) findBase(parent int32, suffix []byte) int32 {
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

func (t *DoubleArrayTrie) expand(size int32) error {
	newSize := int32(float64(len(t.base)) * growthFactor)
	if newSize < size {
		newSize = size
	}
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
