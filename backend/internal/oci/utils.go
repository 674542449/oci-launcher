package oci

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
)

// StrVal safely dereferences a *string, returning empty string if nil
func StrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Int64Val safely dereferences a *int64
func Int64Val(i *int64) int64 {
	if i == nil {
		return 0
	}
	return *i
}

// BoolVal safely dereferences a *bool
func BoolVal(b *bool) bool {
	return b != nil && *b
}

// ServiceErrorInfo extracts the HTTP status and OCI error code from an SDK error.
// ok is false for non-service errors (network failures, context cancellation, ...).
func ServiceErrorInfo(err error) (status int, code string, ok bool) {
	if err == nil {
		return 0, "", false
	}
	if se, isSvc := common.IsServiceError(err); isSvc {
		return se.GetHTTPStatusCode(), se.GetCode(), true
	}
	return 0, "", false
}

// IsNotFoundError reports whether err is an OCI 404 (NotAuthorizedOrNotFound).
func IsNotFoundError(err error) bool {
	status, _, ok := ServiceErrorInfo(err)
	return ok && status == 404
}

// IsTransientError reports network-level failures that are worth retrying:
// timeouts, connection resets, DNS hiccups. OCI service errors are never transient here;
// callers classify those by status code.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	if _, isSvc := common.IsServiceError(err); isSvc {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection reset", "connection refused", "unexpected eof", "no such host", "tls handshake timeout", "i/o timeout", "client.timeout"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
