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
// 正規表現パターンが無効な場合はパニックを発生させます。
func NewNode(pattern string) *Node {
	n := &Node{
		pattern:  pattern,
		children: make([]*Node, 0, 8), // 初期容量を8に設定（一般的なケースで十分）
	}
	if err := n.parseSegment(); err != nil {
		panic(err)
	}
	return n
}

// AddRoute はルートパターンとハンドラをツリーに追加します。
// パスセグメントを順に処理し、必要に応じて新しいノードを作成します。
// 同じパスパターンに対する重複登録はエラーとなります。
// 異なるパラメータ名を持つ同じパスパターン（例：/users/{id}と/users/{name}）もエラーとなります。
// 正規表現パターンの競合は許容され、登録順で優先されます。
// 同一ルート内で同じパラメータ名が複数回使用される場合（例：/users/{id}/posts/{id}）もエラーとなります。
func (n *Node) AddRoute(segments []string, handler HandlerFunc) error {
	// パラメータ名の重複チェック用のマップ
	return n.addRouteWithParamCheck(segments, handler, make(map[string]struct{}))
}

// addRouteWithParamCheck は実際のルート追加処理を行い、パラメータ名の重複チェックも行います。
func (n *Node) addRouteWithParamCheck(segments []string, handler HandlerFunc, usedParams map[string]struct{}) error {
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

	// パラメータセグメントの場合、パラメータ名の重複チェック
	if isDynamicSeg(currentSegment) {
		paramName := extractParamName(currentSegment)
		if _, exists := usedParams[paramName]; exists {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate parameter name in route: " + paramName,
			}
		}
		// パラメータ名を使用済みとして記録
		usedParams[paramName] = struct{}{}
	}

	// 既存の子ノードを探索
	child := n.findChild(currentSegment)

	// 子ノードが存在する場合、セグメントタイプをチェック
	if child != nil {
		// 新しいノードを一時的に作成してセグメントタイプを取得
		tempNode := NewNode(currentSegment)

		// セグメントタイプが同じで、パターンが異なる場合はエラー
		// 例: /users/{id} と /users/{name} の競合
		if tempNode.segType == segParam && child.segType == segParam && tempNode.pattern != child.pattern {
			// パラメータ名を抽出
			tempParamName := extractParamName(tempNode.pattern)
			childParamName := extractParamName(child.pattern)

			if tempParamName != childParamName {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "conflicting parameter names in pattern: " + tempParamName + " and " + childParamName,
				}
			}
		}

		// 静的セグメントと動的セグメントの混在チェック
		if (tempNode.segType == segStatic && (child.segType == segParam || child.segType == segRegex)) ||
			((tempNode.segType == segParam || tempNode.segType == segRegex) && child.segType == segStatic) {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "conflicting segment types: static and dynamic segments cannot be mixed at the same position",
			}
		}

		// 残りのセグメントを再帰的に処理
		return child.addRouteWithParamCheck(segments[1:], handler, usedParams)
	}

	// 子ノードが存在しない場合は新規作成
	child = NewNode(currentSegment)
	n.children = append(n.children, child)

	// 残りのセグメントを再帰的に処理
	return child.addRouteWithParamCheck(segments[1:], handler, usedParams)
}

// extractParamName はパラメータセグメント（{name}形式）からパラメータ名を抽出します。
func extractParamName(pattern string) string {
	// パターンが{name}形式であることを前提とする
	if len(pattern) < 3 || pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		return ""
	}

	// コロンがある場合は、コロンまでの部分がパラメータ名
	if colonIdx := strings.IndexByte(pattern, ':'); colonIdx > 0 {
		return pattern[1:colonIdx]
	}

	// コロンがない場合は、括弧内全体がパラメータ名
	return pattern[1 : len(pattern)-1]
}

// _extractRegexInfo は正規表現セグメント（{name:pattern}形式）からパラメータ名と正規表現パターンを抽出します。
// 現在は未使用ですが、将来の拡張のために保持されています。
func _extractRegexInfo(pattern string) (string, string) {
	// パターンが{name:pattern}形式であることを前提とする
	if len(pattern) < 5 || pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		return "", ""
	}

	// コロンの位置を探す
	colonIdx := strings.IndexByte(pattern, ':')
	if colonIdx <= 0 || colonIdx >= len(pattern)-2 {
		return "", ""
	}

	// パラメータ名と正規表現パターンを抽出
	paramName := pattern[1:colonIdx]
	regexPattern := pattern[colonIdx+1 : len(pattern)-1]

	return paramName, regexPattern
}

// Match はパスとパスセグメントを照合し、一致するハンドラとパラメータを返します。
// 動的セグメントの場合、パラメータ値を抽出してParamsに格納します。
func (n *Node) Match(path string, ps *Params) (HandlerFunc, bool) {
	// パスの終端に到達した場合
	if path == "" {
		return n.handler, true
	}

	// パスの先頭のセグメントを抽出（高速化のため最適化）
	var currentSegment string
	var remainingPath string

	// 次の「/」を探して現在のセグメントと残りのパスを分離
	if path[0] != '/' {
		return nil, false // 正規化されたパスは常に/で始まるはず
	}

	slashIdx := strings.IndexByte(path[1:], '/')
	if slashIdx >= 0 {
		slashIdx++ // path[1:]のインデックスを補正
		currentSegment = path[1:slashIdx]
		remainingPath = path[slashIdx:]
	} else {
		currentSegment = path[1:]
		remainingPath = ""
	}

	// 子ノードを探索して一致するものを探す
	// 静的ノードを最初にチェックして高速化（最も一般的なケース）
	for _, child := range n.children {
		if child.segType == segStatic {
			if child.pattern == currentSegment {
				if h, ok := child.Match(remainingPath, ps); ok {
					return h, true
				}
			}
		}
	}

	// 次に動的ノード（パラメータと正規表現）をチェック
	for _, child := range n.children {
		switch child.segType {
		case segParam:
			paramName := extractParamName(child.pattern)
			ps.Add(paramName, currentSegment)
			if h, ok := child.Match(remainingPath, ps); ok {
				return h, true
			}
			ps.reset() // マッチしなかった場合はパラメータをリセット
		case segRegex:
			if child.regex.MatchString(currentSegment) {
				paramName := extractParamName(child.pattern)
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
// 正規表現パターンが無効な場合はエラーを返します。
func (n *Node) parseSegment() error {
	pattern := n.pattern

	// 空のパターンは静的セグメント
	if pattern == "" {
		n.segType = segStatic
		return nil
	}

	// パラメータ形式（{param}または{param:regex}）かチェック
	if pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		n.segType = segStatic
		return nil
	}

	// 正規表現パターンの検出（{name:pattern}形式）
	if colonIdx := strings.IndexByte(pattern, ':'); colonIdx > 0 {
		n.segType = segRegex
		regexStr := pattern[colonIdx+1 : len(pattern)-1]

		// 正規表現をコンパイル（^と$を自動追加して完全一致を保証）
		// 既に ^ と $ が含まれている場合は追加しない
		var completeRegexStr string
		if !strings.HasPrefix(regexStr, "^") {
			completeRegexStr = "^" + regexStr
		} else {
			completeRegexStr = regexStr
		}
		if !strings.HasSuffix(regexStr, "$") {
			completeRegexStr = completeRegexStr + "$"
		}

		var err error
		n.regex, err = regexp.Compile(completeRegexStr)
		if err != nil {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "invalid regex pattern: " + regexStr + " - " + err.Error(),
			}
		}
		return nil
	}

	// 単純なパラメータ（{name}形式）
	n.segType = segParam
	return nil
}

// findChild は指定されたパターンに一致する子ノードを探します。
// 完全一致する子ノードが存在する場合のみ、そのノードを返します。
// 子ノードの数が多い場合はバイナリサーチを使用して高速化します。
func (n *Node) findChild(pattern string) *Node {
	// 子ノードの数が少ない場合は線形探索
	if len(n.children) < 8 {
		for _, child := range n.children {
			if child.pattern == pattern {
				return child
			}
		}
		return nil
	}

	// 子ノードの数が多い場合はバイナリサーチ
	// 注: バイナリサーチを使用するには、子ノードがパターンでソートされている必要がある
	// 現在の実装では子ノードはソートされていないため、線形探索を使用
	// 将来的にはソートされた子ノードリストを維持することでパフォーマンスを向上できる
	for _, child := range n.children {
		if child.pattern == pattern {
			return child
		}
	}
	return nil
}
