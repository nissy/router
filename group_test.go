package router

import (
	"net/http"
	"testing"
)

// TestGroupCreation はグループの作成をテストします
func TestGroupCreation(t *testing.T) {
	// 新しいルーターを作成
	r := NewRouter()

	// グループを作成
	g := r.Group("/api")

	// グループの初期状態をチェック
	if g == nil {
		t.Fatalf("グループの作成に失敗しました")
	}

	if g.prefix != "/api" {
		t.Errorf("グループのプレフィックスが異なります。期待値: %s, 実際: %s", "/api", g.prefix)
	}

	if len(g.middleware) != 0 {
		t.Errorf("グループのミドルウェアが初期化されていません")
	}

	if len(g.routes) != 0 {
		t.Errorf("グループのルートが初期化されていません")
	}

	if g.router != r {
		t.Errorf("グループのルーターが正しく設定されていません")
	}
}

// TestGroupWithMiddleware はミドルウェア付きのグループの作成をテストします
func TestGroupWithMiddleware(t *testing.T) {
	// 新しいルーターを作成
	r := NewRouter()

	// テスト用のミドルウェア関数
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// ミドルウェア付きのグループを作成
	g := r.Group("/api", middleware1, middleware2)

	// グループのミドルウェアをチェック
	if len(g.middleware) != 2 {
		t.Errorf("グループのミドルウェアの数が異なります。期待値: %d, 実際: %d", 2, len(g.middleware))
	}
}

// TestNestedGroups はネストされたグループの作成をテストします
func TestNestedGroups(t *testing.T) {
	// 新しいルーターを作成
	r := NewRouter()

	// テスト用のミドルウェア関数
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// 親グループを作成
	parent := r.Group("/api", middleware1)

	// 子グループを作成
	child := parent.Group("/v1", middleware2)

	// 子グループのプレフィックスをチェック
	if child.prefix != "/api/v1" {
		t.Errorf("子グループのプレフィックスが異なります。期待値: %s, 実際: %s", "/api/v1", child.prefix)
	}

	// 子グループのミドルウェアをチェック
	if len(child.middleware) != 2 {
		t.Errorf("子グループのミドルウェアの数が異なります。期待値: %d, 実際: %d", 2, len(child.middleware))
	}
}

// TestGroupUse はグループへのミドルウェア追加をテストします
func TestGroupUse(t *testing.T) {
	// 新しいルーターを作成
	r := NewRouter()

	// グループを作成
	g := r.Group("/api")

	// テスト用のミドルウェア関数
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// ミドルウェアを追加
	g.Use(middleware1)
	g.Use(middleware2)

	// グループのミドルウェアをチェック
	if len(g.middleware) != 2 {
		t.Errorf("グループのミドルウェアの数が異なります。期待値: %d, 実際: %d", 2, len(g.middleware))
	}
}

// TestGroupRoute はグループのRouteメソッドをテストします
func TestGroupRoute(t *testing.T) {
	// 新しいルーターを作成
	r := NewRouter()

	// グループを作成
	g := r.Group("/api")

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// テスト用のミドルウェア関数
	middleware := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// ルートを作成
	route := g.Route(http.MethodGet, "/users", handler, middleware)

	// ルートをチェック
	if route == nil {
		t.Fatalf("ルートの作成に失敗しました")
	}

	if route.method != http.MethodGet {
		t.Errorf("ルートのメソッドが異なります。期待値: %s, 実際: %s", http.MethodGet, route.method)
	}

	if route.subPath != "/users" {
		t.Errorf("ルートのパスが異なります。期待値: %s, 実際: %s", "/users", route.subPath)
	}

	if route.handler == nil {
		t.Errorf("ルートのハンドラが設定されていません")
	}

	if len(route.middleware) != 1 {
		t.Errorf("ルートのミドルウェアの数が異なります。期待値: %d, 実際: %d", 1, len(route.middleware))
	}

	if route.group != g {
		t.Errorf("ルートのグループが正しく設定されていません")
	}

	// グループのルートをチェック
	if len(g.routes) != 1 {
		t.Errorf("グループのルートの数が異なります。期待値: %d, 実際: %d", 1, len(g.routes))
	}
}

// TestGroupHTTPMethods はグループのHTTPメソッドをテストします
func TestGroupHTTPMethods(t *testing.T) {
	// 新しいルーターを作成
	r := NewRouter()

	// グループを作成
	g := r.Group("/api")

	// テスト用のハンドラ関数
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// 各HTTPメソッドのルートを作成
	getRoute := g.Get("/users", handler)
	postRoute := g.Post("/users", handler)
	putRoute := g.Put("/users/{id}", handler)
	deleteRoute := g.Delete("/users/{id}", handler)
	patchRoute := g.Patch("/users/{id}", handler)
	headRoute := g.Head("/users", handler)
	optionsRoute := g.Options("/users", handler)

	// 各ルートをチェック
	if getRoute == nil || getRoute.method != http.MethodGet {
		t.Errorf("GETルートが正しく作成されていません")
	}

	if postRoute == nil || postRoute.method != http.MethodPost {
		t.Errorf("POSTルートが正しく作成されていません")
	}

	if putRoute == nil || putRoute.method != http.MethodPut {
		t.Errorf("PUTルートが正しく作成されていません")
	}

	if deleteRoute == nil || deleteRoute.method != http.MethodDelete {
		t.Errorf("DELETEルートが正しく作成されていません")
	}

	if patchRoute == nil || patchRoute.method != http.MethodPatch {
		t.Errorf("PATCHルートが正しく作成されていません")
	}

	if headRoute == nil || headRoute.method != http.MethodHead {
		t.Errorf("HEADルートが正しく作成されていません")
	}

	if optionsRoute == nil || optionsRoute.method != http.MethodOptions {
		t.Errorf("OPTIONSルートが正しく作成されていません")
	}

	// グループのルートの数をチェック
	if len(g.routes) != 7 {
		t.Errorf("グループのルートの数が異なります。期待値: %d, 実際: %d", 7, len(g.routes))
	}
}
