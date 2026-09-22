package outbound

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrBadRequest   ErrorKind = "bad_request"
	ErrUnauthorized ErrorKind = "unauthorized"
	ErrForbidden    ErrorKind = "forbidden"
	ErrNotFound     ErrorKind = "not_found"
	ErrConflict     ErrorKind = "conflict"
	ErrRateLimited  ErrorKind = "rate_limited"
	ErrUnavailable  ErrorKind = "unavailable"
	ErrUpstream     ErrorKind = "upstream_error"
)

type Error struct {
	Kind       ErrorKind
	StatusCode int
	Message    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("outbound %s: status=%d", e.Kind, e.StatusCode)
}

func IsKind(err error, kind ErrorKind) bool {
	var target *Error
	return errors.As(err, &target) && target.Kind == kind
}

func normalizeStatus(status int) ErrorKind {
	switch status {
	case 400:
		return ErrBadRequest
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 409:
		return ErrConflict
	case 429:
		return ErrRateLimited
	case 502, 503, 504:
		return ErrUnavailable
	default:
		return ErrUpstream
	}
}
