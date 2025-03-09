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

// ParamsPool はParamsオブジェクトのプールを管理します。
// 各ルーターインスタンスが独自のプールを持つことで、テスト間の干渉を防ぎます。
type ParamsPool struct {
	pool sync.Pool
}

// NewParamsPool は新しいParamsPoolを作成します。
func NewParamsPool() *ParamsPool {
	return &ParamsPool{
		pool: sync.Pool{
			New: func() any {
				return &Params{
					data: make([]paramEntry, 0, initialParamsCapacity),
				}
			},
		},
	}
}

// Get は新しいParamsインスタンスをプールから取得します。
func (pp *ParamsPool) Get() *Params {
	return pp.pool.Get().(*Params)
}

// Put はParamsインスタンスをリセットしてプールに返却します。
func (pp *ParamsPool) Put(ps *Params) {
	ps.reset()
	pp.pool.Put(ps)
}

// reset はParamsインスタンスをリセットします。
func (ps *Params) reset() {
	// スライスをクリア（容量は維持）
	ps.data = ps.data[:0]
}

// Add は新しいパラメータを追加します。
func (ps *Params) Add(key, val string) {
	ps.data = append(ps.data, paramEntry{key, val})
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

// 後方互換性のために残しておく関数
// 新しいコードでは使用しないでください
func NewParams() *Params {
	return &Params{
		data: make([]paramEntry, 0, initialParamsCapacity),
	}
}

// 後方互換性のために残しておく関数
// 新しいコードでは使用しないでください
func PutParams(ps *Params) {
	ps.reset()
}

// GetParams はコンテキストからParamsインスタンスを取得します。
func GetParams(ctx context.Context) *Params {
	if ctx == nil {
		return &Params{
			data: make([]paramEntry, 0, initialParamsCapacity),
		}
	}

	ps, _ := ctx.Value(paramsKey{}).(*Params)
	if ps == nil {
		ps = &Params{
			data: make([]paramEntry, 0, initialParamsCapacity),
		}
	}
	return ps
}
