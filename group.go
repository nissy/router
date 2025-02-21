package router

import (
	"net/http"
	"strings"
)

type Group struct {
	router *Router
	prefix string
}

func (r *Router) Group(prefix string) *Group {
	return &Group{
		router: r,
		prefix: normalizePath(prefix),
	}
}

func (g *Group) Group(prefix string) *Group {
	return &Group{
		router: g.router,
		prefix: joinPath(g.prefix, normalizePath(prefix)),
	}
}

func (g *Group) Handle(method, subPath string, h HandlerFunc) error {
	full := joinPath(g.prefix, normalizePath(subPath))
	return g.router.Handle(method, full, h)
}

func (g *Group) Get(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodGet, subPath, h)
}

func (g *Group) Post(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodPost, subPath, h)
}

func (g *Group) Put(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodPut, subPath, h)
}

func (g *Group) Delete(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodDelete, subPath, h)
}

func (g *Group) Patch(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodPatch, subPath, h)
}

func (g *Group) Head(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodHead, subPath, h)
}

func (g *Group) Options(subPath string, h HandlerFunc) error {
	return g.Handle(http.MethodOptions, subPath, h)
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
