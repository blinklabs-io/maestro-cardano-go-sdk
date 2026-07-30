package client

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// trackingBody records whether Close was called, so tests can prove a non-2xx
// response does not leak its connection.
type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func newResponse(status int, body string) (*http.Response, *trackingBody) {
	tb := &trackingBody{Reader: strings.NewReader(body)}
	return &http.Response{StatusCode: status, Body: tb}, tb
}

func TestNewAPIErrorPreservesStatusCodeAndMessage(t *testing.T) {
	resp, body := newResponse(
		http.StatusTooManyRequests,
		`{"code":429,"message":"usage limit reached"}`,
	)

	err := newAPIError(resp)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", apiErr.StatusCode)
	}
	if apiErr.Message != "usage limit reached" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "usage limit reached")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error text %q does not mention the status code", err.Error())
	}
	if !strings.Contains(err.Error(), "usage limit reached") {
		t.Errorf("error text %q does not mention the API message", err.Error())
	}
	if !body.closed {
		t.Error("response body was not closed")
	}
}

func TestNewAPIErrorClosesBodyOnEveryStatus(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	} {
		resp, body := newResponse(status, `{"message":"nope"}`)
		if err := newAPIError(resp); err == nil {
			t.Fatalf("status %d: expected an error", status)
		}
		if !body.closed {
			t.Errorf("status %d: response body was not closed", status)
		}
	}
}

func TestNewAPIErrorDistinguishesStatusCodes(t *testing.T) {
	// A caller must be able to tell a rate limit from a client error without
	// string matching, so backoff can be implemented correctly.
	rateLimited, _ := newResponse(http.StatusTooManyRequests, "")
	badRequest, _ := newResponse(http.StatusBadRequest, "")

	var a, b *APIError
	if !errors.As(newAPIError(rateLimited), &a) {
		t.Fatal("expected *APIError for 429")
	}
	if !errors.As(newAPIError(badRequest), &b) {
		t.Fatal("expected *APIError for 400")
	}
	if a.StatusCode == b.StatusCode {
		t.Fatalf("429 and 400 are indistinguishable (both %d)", a.StatusCode)
	}
}

func TestNewAPIErrorFallsBackToRawBody(t *testing.T) {
	resp, _ := newResponse(http.StatusBadGateway, "<html>gateway timeout</html>")

	err := newAPIError(resp)

	if !strings.Contains(err.Error(), "gateway timeout") {
		t.Errorf("error %q does not include the raw body", err.Error())
	}
}

func TestNewAPIErrorTruncatesHugeBody(t *testing.T) {
	resp, _ := newResponse(
		http.StatusBadGateway,
		strings.Repeat("A", 4<<20), // 4 MiB proxy page
	)

	err := newAPIError(resp)

	if len(err.Error()) > maxErrorMessageBytes+256 {
		t.Errorf("error message is %d bytes; expected truncation", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error %q is not marked as truncated", err.Error())
	}
}

func TestNewAPIErrorHandlesEmptyAndNilBody(t *testing.T) {
	resp, _ := newResponse(http.StatusNotFound, "")
	if err := newAPIError(resp); err == nil {
		t.Fatal("expected an error for an empty body")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q does not mention the status code", err.Error())
	}

	nilBody := &http.Response{StatusCode: http.StatusNotFound}
	if err := newAPIError(nilBody); err == nil {
		t.Fatal("expected an error for a nil body")
	}

	if err := newAPIError(nil); err == nil {
		t.Fatal("expected an error for a nil response")
	}
}

func TestAPIErrorMessageFormats(t *testing.T) {
	tests := []struct {
		name string
		err  APIError
		want string
	}{
		{
			name: "status and message",
			err:  APIError{StatusCode: 429, Message: "slow down"},
			want: "maestro API error: 429 Too Many Requests: slow down",
		},
		{
			name: "status only",
			err:  APIError{StatusCode: 503},
			want: "maestro API error: 503 Service Unavailable",
		},
		{
			name: "unknown status with message",
			err:  APIError{StatusCode: 599, Message: "weird"},
			want: "maestro API error: 599: weird",
		},
		{
			name: "unknown status only",
			err:  APIError{StatusCode: 599},
			want: "maestro API error: 599",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClientSurfacesStatusCodeEndToEnd drives a real client method against a
// test server to prove the status code and message reach the caller instead of
// a formatted pointer address.
func TestClientSurfacesStatusCodeEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":429,"message":"usage limit reached"}`))
		},
	))
	defer srv.Close()

	c := NewClient("test-key", "mainnet")
	c.BaseUrl = srv.URL

	_, err := c.ChainTip()
	if err == nil {
		t.Fatal("expected an error from a 429 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", apiErr.StatusCode)
	}
	if apiErr.Message != "usage limit reached" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "usage limit reached")
	}
	// Guard against the original defect, which rendered the body pointer.
	if strings.Contains(err.Error(), "0x") ||
		strings.Contains(err.Error(), "&{") {
		t.Errorf("error text looks like a formatted pointer: %q", err.Error())
	}
}

// TestSendRequestSurfacesStatusCode covers the sendRequest path, which
// previously discarded the status code and left the body open.
func TestSendRequestSurfacesStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":503,"message":"maintenance"}`))
		},
	))
	defer srv.Close()

	c := NewClient("test-key", "mainnet")
	c.BaseUrl = srv.URL

	req, err := http.NewRequest(
		http.MethodPost,
		srv.URL+"/whatever",
		bytes.NewReader(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	err = c.sendRequest(req, &body)
	if err == nil {
		t.Fatal("expected an error from a 503 response")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", apiErr.StatusCode)
	}
	if apiErr.Message != "maintenance" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "maintenance")
	}
}
