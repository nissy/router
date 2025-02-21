package router

import (
	"context"
	"sync"
)

type paramsKey struct{}

type Params struct {
	data  [8]paramEntry
	count int
}

type paramEntry struct {
	key   string
	value string
}

var paramsPool = sync.Pool{
	New: func() interface{} {
		return &Params{}
	},
}

func NewParams() *Params {
	return paramsPool.Get().(*Params)
}

func PutParams(ps *Params) {
	ps.reset()
	paramsPool.Put(ps)
}

func (ps *Params) reset() {
	for i := 0; i < ps.count; i++ {
		ps.data[i] = paramEntry{}
	}
	ps.count = 0
}

func (ps *Params) Add(key, val string) bool {
	if ps.count >= len(ps.data) {
		return false
	}
	ps.data[ps.count] = paramEntry{key, val}
	ps.count++
	return true
}

func (ps *Params) Get(key string) (string, bool) {
	for i := 0; i < ps.count; i++ {
		if ps.data[i].key == key {
			return ps.data[i].value, true
		}
	}
	return "", false
}

func GetParams(ctx context.Context) *Params {
	ps, _ := ctx.Value(paramsKey{}).(*Params)
	if ps == nil {
		ps = &Params{}
	}
	return ps
}
