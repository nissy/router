package router

import (
	"regexp"
	"strings"
)

// segmentType はURLパスセグメントの種類を表す型です。
type segmentType uint8

// セグメントの種類を定義する定数
const (
	staticSegment segmentType = iota // 静的セグメント（通常の文字列）
	paramSegment                     // パラメータセグメント（{name}形式）
	regexSegment                     // 正規表現セグメント（{name:pattern}形式）
)

// Node はURLパスの各セグメントを表すノードです。
// Radixツリーの構造を形成し、ルートマッチングを
// 効率的に管理するために使用されます。
type Node struct {
	segment     string         // このノードが表すパスセグメント
	handler     HandlerFunc    // このノードに関連付けられたハンドラ関数
	children    []*Node        // 子ノードのリスト
	segmentType segmentType    // セグメントの種類（静的、パラメータ、正規表現）
	regex       *regexp.Regexp // 正規表現パターン（segTypeがregexの場合のみ使用）
}

// NewNode は新しいノードを作成して返します。
// パターンを解析し、適切なセグメントタイプを設定します。
// 正規表現パターンが無効な場合はパニックを発生させます。
func NewNode(pattern string) *Node {
	n := &Node{
		segment:  pattern,
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
		if tempNode.segmentType == paramSegment && child.segmentType == paramSegment && tempNode.segment != child.segment {
			// パラメータ名を抽出
			tempParamName := extractParamName(tempNode.segment)
			childParamName := extractParamName(child.segment)

			if tempParamName != childParamName {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "conflicting parameter names in pattern: " + tempParamName + " and " + childParamName,
				}
			}
		}

		// 静的セグメントと動的セグメントの混在チェック
		if (tempNode.segmentType == staticSegment && (child.segmentType == paramSegment || child.segmentType == regexSegment)) ||
			((tempNode.segmentType == paramSegment || tempNode.segmentType == regexSegment) && child.segmentType == staticSegment) {
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

// Match はパスがこのノードまたはその子ノードに一致するかどうかを判定します。
// 一致した場合はハンドラ関数とtrueを返し、一致しなかった場合はnilとfalseを返します。
// パラメータが抽出された場合は、paramsに追加されます。
func (n *Node) Match(path string, params *Params) (HandlerFunc, bool) {
	// パスが空の場合は現在のノードのハンドラを返す
	if path == "" || path == "/" {
		return n.handler, true
	}

	// パスの先頭が/の場合は削除
	if path[0] == '/' {
		path = path[1:]
	}

	// 現在のセグメントと残りのパスを抽出
	var currentSegment string
	var remainingPath string

	slashIndex := strings.IndexByte(path, '/')
	if slashIndex == -1 {
		// スラッシュがない場合は、パス全体が現在のセグメント
		currentSegment = path
		remainingPath = ""
	} else {
		// スラッシュがある場合は、スラッシュまでが現在のセグメント
		currentSegment = path[:slashIndex]
		remainingPath = path[slashIndex:]
	}

	// 子ノードを分類
	var staticMatches []*Node
	var paramMatches []*Node
	var regexMatches []*Node

	// 子ノードを一度のループで分類
	for _, child := range n.children {
		if child.segmentType == staticSegment && child.segment == currentSegment {
			staticMatches = append(staticMatches, child)
		} else if child.segmentType == paramSegment {
			paramMatches = append(paramMatches, child)
		} else if child.segmentType == regexSegment && child.regex.MatchString(currentSegment) {
			regexMatches = append(regexMatches, child)
		}
	}

	// 静的セグメントを優先的にマッチング
	for _, child := range staticMatches {
		handler, matched := child.Match(remainingPath, params)
		if matched {
			return handler, true
		}
	}

	// パラメータセグメントをマッチング
	for _, child := range paramMatches {
		// パラメータ名を抽出
		paramName := extractParamName(child.segment)
		// パラメータを追加
		params.Add(paramName, currentSegment)
		handler, matched := child.Match(remainingPath, params)
		if matched {
			return handler, true
		}
		// マッチしなかった場合はパラメータを削除（バックトラック）
		// 現在の実装では削除は行わず、上書きする方式を採用
	}

	// 正規表現セグメントをマッチング
	for _, child := range regexMatches {
		// パラメータ名を抽出
		paramName := extractParamName(child.segment)
		// パラメータを追加
		params.Add(paramName, currentSegment)
		handler, matched := child.Match(remainingPath, params)
		if matched {
			return handler, true
		}
		// マッチしなかった場合はパラメータを削除（バックトラック）
		// 現在の実装では削除は行わず、上書きする方式を採用
	}

	// マッチするノードが見つからなかった場合
	return nil, false
}

// parseSegment はパターン文字列を解析し、セグメントタイプを決定します。
// また、正規表現セグメントの場合はregexpパターンをコンパイルします。
// 正規表現パターンが無効な場合はエラーを返します。
func (n *Node) parseSegment() error {
	pattern := n.segment

	// 空のパターンは静的セグメント
	if pattern == "" {
		n.segmentType = staticSegment
		return nil
	}

	// パラメータ形式（{param}または{param:regex}）かチェック
	if pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		n.segmentType = staticSegment
		return nil
	}

	// 正規表現パターンの検出（{name:pattern}形式）
	if colonIdx := strings.IndexByte(pattern, ':'); colonIdx > 0 {
		n.segmentType = regexSegment
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
	n.segmentType = paramSegment
	return nil
}

// findChild は指定されたパターンに一致する子ノードを探します。
// 完全一致する子ノードが存在する場合のみ、そのノードを返します。
// 子ノードの数が多い場合はマップを使用して高速化します。
func (n *Node) findChild(pattern string) *Node {
	// 子ノードの数が少ない場合は線形探索（最も一般的なケース）
	if len(n.children) < 8 {
		for _, child := range n.children {
			if child.segment == pattern {
				return child
			}
		}
		return nil
	}

	// 子ノードの数が多い場合はマップを使用して高速化
	childMap := make(map[string]*Node, len(n.children))
	for _, child := range n.children {
		childMap[child.segment] = child
	}

	return childMap[pattern]
}
