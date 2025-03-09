package router

import (
	"fmt"
	"net/http"
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
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		return nil
	default:
		return &RouterError{Code: ErrInvalidMethod, Message: "unsupported method: " + m}
	}
}

// validateStaticSegment checks if a static segment contains only
// alphanumeric characters, hyphens, underscores, and dots.
func validateStaticSegment(segment string) error {
	for _, r := range segment {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return fmt.Errorf("invalid character %q in static segment", r)
		}
	}
	return nil
}

// validatePattern validates the entire pattern and applies validateStaticSegment to each segment if it's static.
func validatePattern(p string) error {
	if p == "" {
		return &RouterError{Code: ErrInvalidPattern, Message: "empty pattern"}
	}
	segments := parseSegments(p)
	for _, seg := range segments {
		// Skip checking for dynamic segments ({param} or {param:regex})
		if !isDynamicSeg(seg) {
			if err := validateStaticSegment(seg); err != nil {
				return err
			}
		}
	}
	return nil
}
