package router

import (
	"regexp"
	"strings"
)

// Node はRadixツリーのノードを表す構造体です。
// 動的なルーティングパターン（パラメータやワイルドカードを含むパス）を
// 効率的に管理するために使用されます。
type Node struct {
	pattern  string         // このノードが表すパスセグメント
	handler  HandlerFunc    // このノードに関連付けられたハンドラ関数
	children []*Node        // 子ノードのリスト
	segType  segmentType    // セグメントの種類（静的、パラメータ、正規表現）
	regex    *regexp.Regexp // 正規表現パターン（segTypeがregexの場合のみ使用）
}

// segmentType はパスセグメントの種類を表す型です。
type segmentType uint8

// セグメントの種類を定義する定数
const (
	segStatic segmentType = iota // 静的セグメント（通常の文字列）
	segParam                     // パラメータセグメント（{name}形式）
	segRegex                     // 正規表現セグメント（{name:pattern}形式）
)

// NewNode は新しいノードを作成して返します。
// パターンを解析し、適切なセグメントタイプを設定します。
func NewNode(pattern string) *Node {
	n := &Node{
		pattern:  pattern,
		children: make([]*Node, 0, 8),
	}
	n.parseSegment()
	return n
}

// AddRoute はルートパターンとハンドラをツリーに追加します。
// パスセグメントを順に処理し、必要に応じて新しいノードを作成します。
func (n *Node) AddRoute(segments []string, handler HandlerFunc) error {
	if len(segments) == 0 {
		if n.handler != nil {
			return &RouterError{Code: ErrInvalidPattern, Message: "duplicate pattern"}
		}
		n.handler = handler
		return nil
	}

	seg := segments[0]
	child := n.findChild(seg)
	if child == nil {
		child = NewNode(seg)
		n.children = append(n.children, child)
	}
	return child.AddRoute(segments[1:], handler)
}

// Match はパスとパスセグメントを照合し、一致するハンドラとパラメータを返します。
// 動的セグメントの場合、パラメータ値を抽出してParamsに格納します。
func (n *Node) Match(path string, ps *Params) (HandlerFunc, bool) {
	if path == "" {
		return n.handler, true
	}

	// パスの先頭のセグメントを抽出
	var seg string
	if idx := strings.IndexByte(path[1:], '/'); idx >= 0 {
		seg = path[1 : idx+1]
	} else {
		seg = path[1:]
	}

	// 子ノードを探索
	for _, child := range n.children {
		switch child.segType {
		case segStatic:
			// 静的セグメントは完全一致が必要
			if child.pattern == seg {
				if h, ok := child.Match(path[len(seg)+1:], ps); ok {
					return h, true
				}
			}
		case segParam:
			// パラメータセグメントは値を抽出
			ps.Add(child.pattern[1:len(child.pattern)-1], seg)
			if h, ok := child.Match(path[len(seg)+1:], ps); ok {
				return h, true
			}
			ps.reset()
		case segRegex:
			// 正規表現セグメントはパターンマッチを実行
			if child.regex.MatchString(seg) {
				name := child.pattern[1:strings.IndexByte(child.pattern, ':')]
				ps.Add(name, seg)
				if h, ok := child.Match(path[len(seg)+1:], ps); ok {
					return h, true
				}
				ps.reset()
			}
		}
	}

	return nil, false
}

// parseSegment はパターン文字列を解析し、セグメントタイプを決定します。
// また、正規表現セグメントの場合はregexpパターンをコンパイルします。
func (n *Node) parseSegment() {
	pattern := n.pattern
	if pattern == "" {
		n.segType = segStatic
		return
	}

	if pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		n.segType = segStatic
		return
	}

	// 正規表現パターンの検出
	if idx := strings.IndexByte(pattern, ':'); idx > 0 {
		n.segType = segRegex
		regexStr := pattern[idx+1 : len(pattern)-1]
		n.regex = regexp.MustCompile("^" + regexStr + "$")
		return
	}

	n.segType = segParam
}

// findChild は指定されたパターンに一致する子ノードを探します。
// 完全一致する子ノードが存在する場合のみ、そのノードを返します。
func (n *Node) findChild(pattern string) *Node {
	for _, child := range n.children {
		if child.pattern == pattern {
			return child
		}
	}
	return nil
}
