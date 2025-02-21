// router_bench_test.go
package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/nissy/router"
)

func BenchmarkStaticRoute(b *testing.B) {
	r := router.NewRouter()
	err := r.Get("/static", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("static ok"))
		return err
	})
	if err != nil {
		b.Fatalf("failed to register static route: %v", err)
	}

	req := httptest.NewRequest("GET", "/static", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// 消費しておく
		_, _ = io.ReadAll(w.Result().Body)
	}
}

func BenchmarkDynamicRouteParam(b *testing.B) {
	r := router.NewRouter()
	err := r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		id, _ := ps.Get("id")
		_, err := w.Write([]byte("user:" + id))
		return err
	})
	if err != nil {
		b.Fatalf("failed to register dynamic route: %v", err)
	}

	req := httptest.NewRequest("GET", "/users/12345", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		_, _ = io.ReadAll(w.Result().Body)
	}
}

func BenchmarkDynamicRouteRegex(b *testing.B) {
	r := router.NewRouter()
	err := r.Get("/posts/{slug:[a-z0-9-]+}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		slug, _ := ps.Get("slug")
		_, err := w.Write([]byte("slug:" + slug))
		return err
	})
	if err != nil {
		b.Fatalf("failed to register regex route: %v", err)
	}

	req := httptest.NewRequest("GET", "/posts/hello-world", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		_, _ = io.ReadAll(w.Result().Body)
	}
}

func BenchmarkMassRegistration(b *testing.B) {
	r := router.NewRouter()
	numRoutes := 1000
	// 大量の静的ルートを登録
	for i := 0; i < numRoutes; i++ {
		route := "/mass/" + strconv.Itoa(i)
		expected := "mass:" + strconv.Itoa(i)
		err := r.Get(route, func(exp string) router.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) error {
				_, err := w.Write([]byte(exp))
				return err
			}
		}(expected))
		if err != nil {
			b.Fatalf("failed to register route %q: %v", route, err)
		}
	}

	// ベンチマーク対象のルート
	req := httptest.NewRequest("GET", "/mass/500", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		_, _ = io.ReadAll(w.Result().Body)
	}
}

func BenchmarkOverlappingRoutes(b *testing.B) {
	r := router.NewRouter()
	// 静的ルート
	err := r.Get("/overlap/test", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("static overlap"))
		return err
	})
	if err != nil {
		b.Fatalf("failed to register static overlap route: %v", err)
	}
	// 動的ルート
	err = r.Get("/overlap/{id}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		id, _ := ps.Get("id")
		_, err := w.Write([]byte("dynamic overlap:" + id))
		return err
	})
	if err != nil {
		b.Fatalf("failed to register dynamic overlap route: %v", err)
	}

	// 静的ルートが優先されるか確認
	req := httptest.NewRequest("GET", "/overlap/test", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		_, _ = io.ReadAll(w.Result().Body)
	}

	// 動的ルートの検証
	req = httptest.NewRequest("GET", "/overlap/123", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		_, _ = io.ReadAll(w.Result().Body)
	}
}
