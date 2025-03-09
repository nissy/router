package router

import (
	"context"
	"sync"
)

type paramsKey struct{}

// Params is a structure for storing URL parameters.
// It can store any number of parameters using a slice.
type Params struct {
	data []paramEntry // Slice of parameter entries
}

type paramEntry struct {
	key   string
	value string
}

// Initial capacity (for performance)
const initialParamsCapacity = 8

// ParamsPool manages a pool of Params objects.
// Each router instance has its own pool to prevent interference between tests.
type ParamsPool struct {
	pool sync.Pool
}

// NewParamsPool creates a new ParamsPool.
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

// Get retrieves a new Params instance from the pool.
func (pp *ParamsPool) Get() *Params {
	return pp.pool.Get().(*Params)
}

// Put resets a Params instance and returns it to the pool.
func (pp *ParamsPool) Put(ps *Params) {
	ps.reset()
	pp.pool.Put(ps)
}

// reset resets a Params instance.
func (ps *Params) reset() {
	// Clear the slice (maintain capacity)
	ps.data = ps.data[:0]
}

// Add adds a new parameter.
func (ps *Params) Add(key, val string) {
	ps.data = append(ps.data, paramEntry{key, val})
}

// Get retrieves the value corresponding to a key.
func (ps *Params) Get(key string) (string, bool) {
	for i := range ps.data {
		if ps.data[i].key == key {
			return ps.data[i].value, true
		}
	}
	return "", false
}

// Len returns the number of parameters.
func (ps *Params) Len() int {
	return len(ps.data)
}

// Functions kept for backward compatibility
// Do not use in new code
func NewParams() *Params {
	return &Params{
		data: make([]paramEntry, 0, initialParamsCapacity),
	}
}

// Functions kept for backward compatibility
// Do not use in new code
func PutParams(ps *Params) {
	ps.reset()
}

// GetParams retrieves a Params instance from the context.
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
