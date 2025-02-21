package router

import (
	"fmt"
	"unicode"
)

type ErrorCode uint8

const (
	ErrInvalidPattern ErrorCode = iota + 1
	ErrInvalidMethod
	ErrNilHandler
	ErrInternalError
)

type RouterError struct {
	Code    ErrorCode
	Message string
}

func (e *RouterError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code.String(), e.Message)
}

func (c ErrorCode) String() string {
	switch c {
	case ErrInvalidPattern:
		return "InvalidPattern"
	case ErrInvalidMethod:
		return "InvalidMethod"
	case ErrNilHandler:
		return "NilHandler"
	case ErrInternalError:
		return "InternalError"
	default:
		return "UnknownError"
	}
}

func validateMethod(m string) error {
	switch m {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return nil
	default:
		return &RouterError{Code: ErrInvalidMethod, Message: "unsupported method: " + m}
	}
}

// validateStaticSegment は、静的セグメントに対して
// 英数字、ハイフン、アンダースコア、ドットのみが使用されているかをチェックします。
func validateStaticSegment(segment string) error {
	for _, r := range segment {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return fmt.Errorf("invalid character %q in static segment", r)
		}
	}
	return nil
}

// validatePattern はパターン全体を検証し、各セグメントが静的の場合は validateStaticSegment を適用します。
func validatePattern(p string) error {
	if p == "" {
		return &RouterError{Code: ErrInvalidPattern, Message: "empty pattern"}
	}
	segments := parseSegments(p)
	for _, seg := range segments {
		// 動的セグメント（{param} や {param:regex}）の場合はチェックしない
		if !isDynamicSeg(seg) {
			if err := validateStaticSegment(seg); err != nil {
				return err
			}
		}
	}
	return nil
}
