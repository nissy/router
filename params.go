package router

import (
	"context"
	"sync"
)

type paramsKey struct{}

// Params はURLパラメータを格納する構造体です。
// スライスを使用して任意の数のパラメータを格納できます。
type Params struct {
	data []paramEntry // パラメータエントリのスライス
}

type paramEntry struct {
	key   string
	value string
}

// 初期容量（パフォーマンスのため）
const initialParamsCapacity = 8

var paramsPool = sync.Pool{
	New: func() any {
		return &Params{
			data: make([]paramEntry, 0, initialParamsCapacity),
		}
	},
}

// NewParams は新しいParamsインスタンスをプールから取得します。
func NewParams() *Params {
	return paramsPool.Get().(*Params)
}

// PutParams はParamsインスタンスをリセットしてプールに返却します。
func PutParams(ps *Params) {
	ps.reset()
	paramsPool.Put(ps)
}

// reset はParamsインスタンスをリセットします。
func (ps *Params) reset() {
	// スライスをクリア（容量は維持）
	ps.data = ps.data[:0]
}

// Add は新しいパラメータを追加します。
// 常にtrueを返します（スライスを使用しているため容量制限がない）。
func (ps *Params) Add(key, val string) bool {
	ps.data = append(ps.data, paramEntry{key, val})
	return true
}

// Get はキーに対応する値を取得します。
func (ps *Params) Get(key string) (string, bool) {
	for i := range ps.data {
		if ps.data[i].key == key {
			return ps.data[i].value, true
		}
	}
	return "", false
}

// Len はパラメータの数を返します。
func (ps *Params) Len() int {
	return len(ps.data)
}

// GetParams はコンテキストからParamsインスタンスを取得します。
func GetParams(ctx context.Context) *Params {
	ps, _ := ctx.Value(paramsKey{}).(*Params)
	if ps == nil {
		ps = &Params{
			data: make([]paramEntry, 0, initialParamsCapacity),
		}
	}
	return ps
}
