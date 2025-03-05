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
		children: make([]*Node, 0, 8), // 初期容量を8に設定（一般的なケースで十分）
	}
	n.parseSegment()
	return n
}

// AddRoute はルートパターンとハンドラをツリーに追加します。
// パスセグメントを順に処理し、必要に応じて新しいノードを作成します。
func (n *Node) AddRoute(segments []string, handler HandlerFunc) error {
	// 全セグメントを処理し終えた場合、現在のノードにハンドラを設定
	if len(segments) == 0 {
		if n.handler != nil {
			return &RouterError{Code: ErrInvalidPattern, Message: "duplicate pattern"}
		}
		n.handler = handler
		return nil
	}

	// 現在のセグメントを取得
	currentSegment := segments[0]

	// 既存の子ノードを探索
	child := n.findChild(currentSegment)

	// 子ノードが存在しない場合は新規作成
	if child == nil {
		child = NewNode(currentSegment)
		n.children = append(n.children, child)
	}

	// 残りのセグメントを再帰的に処理
	return child.AddRoute(segments[1:], handler)
}

// Match はパスとパスセグメントを照合し、一致するハンドラとパラメータを返します。
// 動的セグメントの場合、パラメータ値を抽出してParamsに格納します。
func (n *Node) Match(path string, ps *Params) (HandlerFunc, bool) {
	// パスの終端に到達した場合
	if path == "" {
		return n.handler, true
	}

	// パスの先頭のセグメントを抽出
	var currentSegment string
	remainingPath := ""

	// 次の「/」を探して現在のセグメントと残りのパスを分離
	if idx := strings.IndexByte(path[1:], '/'); idx >= 0 {
		currentSegment = path[1 : idx+1]
		remainingPath = path[idx+1:]
	} else {
		currentSegment = path[1:]
		remainingPath = ""
	}

	// 子ノードを探索して一致するものを探す
	for _, child := range n.children {
		switch child.segType {
		case segStatic:
			// 静的セグメントは完全一致が必要
			if child.pattern == currentSegment {
				if h, ok := child.Match(remainingPath, ps); ok {
					return h, true
				}
			}
		case segParam:
			// パラメータセグメントは値を抽出
			paramName := child.pattern[1 : len(child.pattern)-1]
			ps.Add(paramName, currentSegment)
			if h, ok := child.Match(remainingPath, ps); ok {
				return h, true
			}
			ps.reset() // マッチしなかった場合はパラメータをリセット
		case segRegex:
			// 正規表現セグメントはパターンマッチを実行
			if child.regex.MatchString(currentSegment) {
				// パラメータ名を抽出（{name:pattern}形式から）
				colonIdx := strings.IndexByte(child.pattern, ':')
				paramName := child.pattern[1:colonIdx]

				ps.Add(paramName, currentSegment)
				if h, ok := child.Match(remainingPath, ps); ok {
					return h, true
				}
				ps.reset() // マッチしなかった場合はパラメータをリセット
			}
		}
	}

	// 一致するハンドラが見つからなかった
	return nil, false
}

// parseSegment はパターン文字列を解析し、セグメントタイプを決定します。
// また、正規表現セグメントの場合はregexpパターンをコンパイルします。
func (n *Node) parseSegment() {
	pattern := n.pattern

	// 空のパターンは静的セグメント
	if pattern == "" {
		n.segType = segStatic
		return
	}

	// パラメータ形式（{param}または{param:regex}）かチェック
	if pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		n.segType = segStatic
		return
	}

	// 正規表現パターンの検出（{name:pattern}形式）
	if colonIdx := strings.IndexByte(pattern, ':'); colonIdx > 0 {
		n.segType = segRegex
		regexStr := pattern[colonIdx+1 : len(pattern)-1]

		// 正規表現をコンパイル（^と$を自動追加して完全一致を保証）
		n.regex = regexp.MustCompile("^" + regexStr + "$")
		return
	}

	// 単純なパラメータ（{name}形式）
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
