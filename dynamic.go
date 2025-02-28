package router

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// セグメント種別
type segmentType uint8

const (
	segmentTypeStatic segmentType = iota
	segmentTypeParam
	segmentTypeRegex
)

var regexCache sync.Map

// Node は動的ルート用 Radix ツリーのノード
type Node struct {
	prefix      string         // 静的部分の共通接頭辞
	segmentType segmentType    // segmentTypeStatic, segmentTypeParam, segmentTypeRegex
	paramName   string         // パラメータノードの場合の名前
	regex       *regexp.Regexp // segmentTypeRegex 用のコンパイル済み正規表現
	children    []*Node        // 子ノード
	handler     HandlerFunc    // 自作の HandlerFunc を使用
}

func NewNode(prefix string) *Node {
	return &Node{
		prefix:      prefix,
		segmentType: segmentTypeStatic,
		children:    make([]*Node, 0),
	}
}

// AddRoute はセグメントリストを辿ってルートを追加します。
func (n *Node) AddRoute(segs []string, handler HandlerFunc) error {
	curr := n
	for i, seg := range segs {
		isLast := (i == len(segs)-1)
		st, param, rexp, err := parseSegment(seg)
		if err != nil {
			return err
		}
		curr = curr.insertSegment(st, param, rexp)
		if isLast {
			curr.handler = handler
		}
	}
	return nil
}

// Match は path を受け取り、マッチしたハンドラと抽出パラメータを返します。
func (n *Node) Match(path string, ps *Params) (HandlerFunc, bool) {
	return n.matchOne(path, ps)
}

func (n *Node) matchOne(path string, ps *Params) (HandlerFunc, bool) {
	// 正規表現ノード
	if n.segmentType == segmentTypeRegex {
		part, remain := cutPath(path)
		if !n.regex.MatchString(part) {
			return nil, false
		}
		ps.Add(n.paramName, part)
		if remain == "" {
			if n.handler != nil {
				return n.handler, true
			}
			return nil, false
		}
		for _, child := range n.children {
			if h, ok := child.matchOne(remain, ps); ok {
				return h, true
			}
		}
		return nil, false
	}

	// パラメータノード
	if n.segmentType == segmentTypeParam {
		part, remain := cutPath(path)
		ps.Add(n.paramName, part)
		if remain == "" {
			if n.handler != nil {
				return n.handler, true
			}
			return nil, false
		}
		for _, child := range n.children {
			if h, ok := child.matchOne(remain, ps); ok {
				return h, true
			}
		}
		return nil, false
	}

	// 静的ノード
	if !strings.HasPrefix(path, n.prefix) {
		return nil, false
	}
	remain := path[len(n.prefix):]
	if remain == "" {
		if n.handler != nil {
			return n.handler, true
		}
		for _, child := range n.children {
			if h, ok := child.matchOne(remain, ps); ok {
				return h, true
			}
		}
		return nil, false
	}
	if remain[0] == '/' {
		remain = remain[1:]
	}
	for _, child := range n.children {
		if h, ok := child.matchOne(remain, ps); ok {
			return h, true
		}
	}

	return nil, false
}

func (n *Node) insertSegment(st segmentType, param, rexp string) *Node {
	// 動的ノードの場合（パラメータ / 正規表現）
	if st != segmentTypeStatic {
		for _, child := range n.children {
			if child.segmentType == st && child.paramName == param {
				if st == segmentTypeRegex && child.regex.String() == rexp {
					return child
				} else if st != segmentTypeRegex {
					return child
				}
			}
		}
		newChild := &Node{
			segmentType: st,
			paramName:   param,
			children:    make([]*Node, 0),
		}
		if st == segmentTypeRegex {
			if cached, ok := regexCache.Load(rexp); ok {
				newChild.regex = cached.(*regexp.Regexp)
			} else {
				rx, err := regexp.Compile(rexp)
				if err != nil {
					panic(fmt.Sprintf("invalid regex: %s", rexp))
				}
				newChild.regex = rx
				regexCache.Store(rexp, rx)
			}
		}
		n.children = append(n.children, newChild)
		return newChild
	}
	// 静的ノードの場合は Radix 的に統合
	return n.insertStatic(param)
}

func (n *Node) insertStatic(seg string) *Node {
	for _, child := range n.children {
		if child.segmentType == segmentTypeStatic {
			cp := longestCommonPrefix(child.prefix, seg)
			if cp == "" {
				continue
			}
			if cp == child.prefix {
				if len(seg) > len(cp) {
					remain := seg[len(cp):]
					return child.insertStatic(remain)
				}
				return child
			} else if cp == seg {
				remainChild := child.prefix[len(cp):]
				splitNode := &Node{
					prefix:      remainChild,
					segmentType: segmentTypeStatic,
					handler:     child.handler,
					children:    child.children,
				}
				child.prefix = cp
				child.handler = nil
				child.children = []*Node{splitNode}
				return child
			} else {
				remainChild := child.prefix[len(cp):]
				remainSeg := seg[len(cp):]
				splitNode := &Node{
					prefix:      remainChild,
					segmentType: segmentTypeStatic,
					handler:     child.handler,
					children:    child.children,
				}
				child.prefix = cp
				child.handler = nil
				child.children = []*Node{splitNode}
				if remainSeg != "" {
					newNode := &Node{
						prefix:      remainSeg,
						segmentType: segmentTypeStatic,
					}
					child.children = append(child.children, newNode)
					return newNode
				}
				return child
			}
		}
	}
	newChild := &Node{
		prefix:      seg,
		segmentType: segmentTypeStatic,
	}
	n.children = append(n.children, newChild)
	return newChild
}

func cutPath(path string) (string, string) {
	idx := strings.IndexByte(path, '/')
	if idx < 0 {
		return path, ""
	}
	return path[:idx], path[idx+1:]
}

func longestCommonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	var i int
	for i = 0; i < minLen; i++ {
		if a[i] != b[i] {
			break
		}
	}
	return a[:i]
}

// parseSegment は、セグメント文字列を解析します。
// ワイルドカード（先頭が '*'）はエラーを返します。
func parseSegment(seg string) (segmentType, string, string, error) {
	if seg == "" {
		return segmentTypeStatic, seg, "", nil
	}
	if seg[0] == '*' {
		return 0, "", "", fmt.Errorf("invalid wildcard usage: %q (wildcard '*' is not allowed)", seg)
	}
	if seg[0] == '{' && seg[len(seg)-1] == '}' {
		content := seg[1 : len(seg)-1]
		if idx := strings.IndexByte(content, ':'); idx != -1 {
			pattern := content[idx+1:]
			// アンカーがなければ追加
			if !strings.HasPrefix(pattern, "^") {
				pattern = "^" + pattern
			}
			if !strings.HasSuffix(pattern, "$") {
				pattern = pattern + "$"
			}
			return segmentTypeRegex, content[:idx], pattern, nil
		}
		return segmentTypeParam, content, "", nil
	}

	return segmentTypeStatic, seg, "", nil
}
