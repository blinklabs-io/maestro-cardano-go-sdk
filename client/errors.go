package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// maxErrorBodyBytes bounds how much of a non-2xx response body is read
	// when building an APIError. Maestro error payloads are small; anything
	// larger is a proxy or gateway page and is truncated rather than buffered
	// in full.
	maxErrorBodyBytes = 64 << 10

	// maxErrorMessageBytes bounds the length of an error message taken
	// verbatim from a response body that was not valid Maestro error JSON.
	maxErrorMessageBytes = 512
)

// APIError is returned when the Maestro API responds with a non-2xx status.
// Callers can inspect StatusCode to tell a rate limit or a transient
// server-side fault apart from a permanent client error:
//
//	var apiErr *client.APIError
//	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
//		// back off and retry
//	}
type APIError struct {
	// StatusCode is the HTTP status returned by the API.
	StatusCode int
	// Message is the error text reported by the API, when it supplied one.
	Message string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	status := http.StatusText(e.StatusCode)
	switch {
	case e.Message != "" && status != "":
		return fmt.Sprintf(
			"maestro API error: %d %s: %s",
			e.StatusCode,
			status,
			e.Message,
		)
	case e.Message != "":
		return fmt.Sprintf("maestro API error: %d: %s", e.StatusCode, e.Message)
	case status != "":
		return fmt.Sprintf("maestro API error: %d %s", e.StatusCode, status)
	default:
		return fmt.Sprintf("maestro API error: %d", e.StatusCode)
	}
}

// newAPIError builds an APIError from a non-2xx response. It consumes and
// closes the response body, which must not be used afterwards.
func newAPIError(resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("empty response")
	}
	apiErr := &APIError{StatusCode: resp.StatusCode}
	if resp.Body == nil {
		return apiErr
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil || len(body) == 0 {
		return apiErr
	}
	var parsed errorResponse
	if json.Unmarshal(body, &parsed) == nil && parsed.Message != "" {
		apiErr.Message = parsed.Message
		return apiErr
	}
	apiErr.Message = truncateMessage(body)
	return apiErr
}

// truncateMessage renders a response body as a single-line error message,
// bounded to maxErrorMessageBytes.
func truncateMessage(body []byte) string {
	msg := strings.TrimSpace(string(body))
	if len(msg) > maxErrorMessageBytes {
		return msg[:maxErrorMessageBytes] + "... (truncated)"
	}
	return msg
}
