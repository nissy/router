package router

import (
	"net/http"
	"strings"
)

// applyMiddlewareChain はハンドラ関数にミドルウェアチェーンを適用します。
// ミドルウェアは登録順に適用されます（最初に登録されたものが最初に実行）。
func applyMiddlewareChain(h HandlerFunc, middleware []MiddlewareFunc) HandlerFunc {
	// ミドルウェアを登録順に適用
	for i := 0; i < len(middleware); i++ {
		h = middleware[i](h)
	}
	return h
}

// Route は単一のルートを表します。
// ミドルウェアを適用するためのインターフェースを提供します。
type Route struct {
	group      *Group           // このルートが属するグループ（グループに属さない場合はnil）
	router     *Router          // このルートが属するルーター
	method     string           // HTTPメソッド
	subPath    string           // ルートのパス
	handler    HandlerFunc      // ハンドラ関数
	middleware []MiddlewareFunc // ミドルウェア関数のリスト
	applied    bool             // 既に適用されたかどうか
}

// WithMiddleware はルートに特定のミドルウェアを適用します。
// ミドルウェアはハンドラ関数に適用され、同じRouteオブジェクトが返されます。
func (r *Route) WithMiddleware(middleware ...MiddlewareFunc) *Route {
	// 既に適用されたルートの場合は、そのまま返す
	if r.applied {
		return r
	}

	// ミドルウェアを追加
	r.middleware = append(r.middleware, middleware...)

	return r
}

// build はルートを実際にルーターに登録します。
// このメソッドは明示的に呼び出す必要があります。
// 重複するルートが検出された場合はエラーを返します。
func (r *Route) build() error {
	if r.applied {
		return nil
	}

	// ミドルウェアをハンドラに適用
	handler := r.handler
	if len(r.middleware) > 0 {
		handler = applyMiddlewareChain(handler, r.middleware)
	}

	// ルートを登録
	r.applied = true

	// グループに属さないルート（router.Routeで作成されたルート）の場合
	if r.group == nil {
		// ルートを直接ルーターに登録
		return r.router.Handle(r.method, r.subPath, handler)
	}

	// グループに属するルートの場合
	fullPath := joinPath(r.group.prefix, normalizePath(r.subPath))
	return r.router.Handle(r.method, fullPath, handler)
}

type Group struct {
	router     *Router
	prefix     string
	middleware []MiddlewareFunc
	routes     []*Route
}

// Group は新しいルートグループを作成します。
// 指定されたパスプレフィックスを持つGroupを返します。
func (r *Router) Group(prefix string, middleware ...MiddlewareFunc) *Group {
	group := &Group{
		router:     r,
		prefix:     normalizePath(prefix),
		middleware: middleware,
		routes:     make([]*Route, 0),
	}

	// グループをルーターに追加
	r.groups = append(r.groups, group)

	return group
}

// Group は新しいルートグループを作成します。
// 新しいグループは親グループのパスプレフィックスとミドルウェアを継承し、
// 追加のパスプレフィックスとミドルウェアを適用します。
func (g *Group) Group(prefix string, middleware ...MiddlewareFunc) *Group {
	// 親グループのミドルウェアと新しいミドルウェアを結合
	combinedMiddleware := make([]MiddlewareFunc, len(g.middleware)+len(middleware))
	copy(combinedMiddleware, g.middleware)
	copy(combinedMiddleware[len(g.middleware):], middleware)

	return &Group{
		router:     g.router,
		prefix:     joinPath(g.prefix, normalizePath(prefix)),
		middleware: combinedMiddleware,
		routes:     make([]*Route, 0),
	}
}

// Use はグループに新しいミドルウェアを追加します。
func (g *Group) Use(middleware ...MiddlewareFunc) *Group {
	g.middleware = append(g.middleware, middleware...)
	return g
}

// Handle はrouterGroupのHandleメソッドの実装です。
// 指定されたHTTPメソッド、パターン、ハンドラ関数でルートを登録します。
// パターンにはグループのプレフィックスが自動的に追加され、
// ハンドラ関数にはグループのミドルウェアが適用されます。
func (g *Group) Handle(method, subPath string, h HandlerFunc) error {
	full := joinPath(g.prefix, normalizePath(subPath))

	// グループのミドルウェアをハンドラに適用
	h = applyMiddlewareChain(h, g.middleware)

	return g.router.Handle(method, full, h)
}

// Route は新しいルートを作成しますが、まだ登録はしません。
// 返されたRouteオブジェクトに対してWithMiddlewareを呼び出すことで、
// 特定のミドルウェアを適用できます。
func (g *Group) Route(method, subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := &Route{
		group:      g,
		router:     g.router,
		method:     method,
		subPath:    subPath,
		handler:    h,
		middleware: make([]MiddlewareFunc, 0, len(middleware)),
		applied:    false,
	}

	// ミドルウェアを追加
	if len(middleware) > 0 {
		route.middleware = append(route.middleware, middleware...)
	}

	// ルートをグループに追加
	g.routes = append(g.routes, route)

	return route
}

// Build はグループ内のすべてのルートを登録します。
// このメソッドは明示的に呼び出す必要があります。
// 重複するルートが検出された場合はエラーを返します。
// 注意: このメソッドは通常、Router.Buildから呼び出されます。
func (g *Group) Build() error {
	// ローカルな重複チェック用のマップ（グループ内の重複のみをチェック）
	routeMap := make(map[string]struct{})

	for _, route := range g.routes {
		if route.applied {
			continue
		}

		// 完全なパスを計算
		fullPath := joinPath(g.prefix, route.subPath)

		// ローカルな重複チェック
		routeKey := route.method + ":" + fullPath
		if _, exists := routeMap[routeKey]; exists {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate route definition in group: " + route.method + " " + fullPath,
			}
		}
		routeMap[routeKey] = struct{}{}

		if err := route.build(); err != nil {
			return err
		}
	}
	return nil
}

// Get はGETメソッドのルートを作成します。
// 返されたRouteオブジェクトに対してWithMiddlewareを呼び出すことで、
// 特定のミドルウェアを適用できます。
func (g *Group) Get(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodGet, subPath, h, middleware...)
	return route
}

// Post はPOSTメソッドのルートを作成します。
func (g *Group) Post(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodPost, subPath, h, middleware...)
	return route
}

// Put はPUTメソッドのルートを作成します。
func (g *Group) Put(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodPut, subPath, h, middleware...)
	return route
}

// Delete はDELETEメソッドのルートを作成します。
func (g *Group) Delete(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodDelete, subPath, h, middleware...)
	return route
}

// Patch はPATCHメソッドのルートを作成します。
func (g *Group) Patch(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodPatch, subPath, h, middleware...)
	return route
}

// Head はHEADメソッドのルートを作成します。
func (g *Group) Head(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodHead, subPath, h, middleware...)
	return route
}

// Options はOPTIONSメソッドのルートを作成します。
func (g *Group) Options(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodOptions, subPath, h, middleware...)
	return route
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 末尾が "/" で、かつルートではない場合は削除
	if len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

func joinPath(p1, p2 string) string {
	if p1 == "/" {
		return p2
	}
	return p1 + p2
}
