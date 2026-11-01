package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// CallbackResult is the outcome of the OAuth redirect.
type CallbackResult struct {
	Code string
}

// WaitForCallbackOn serves the OAuth redirect on bindHost:port and returns the
// authorization code once it arrives or the timeout elapses. bindHost lets a
// remote browser reach the loopback redirect via a private network address.
func WaitForCallbackOn(bindHost string, port int, expectedState string, timeout time.Duration) (CallbackResult, error) {
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost, port))
	if err != nil {
		return CallbackResult{}, err
	}
	return WaitForCallbackOnListener(listener, expectedState, timeout)
}

// WaitForCallbackOnListener serves the OAuth redirect on an existing listener.
func WaitForCallbackOnListener(listener net.Listener, expectedState string, timeout time.Duration) (CallbackResult, error) {
	resultCh := make(chan CallbackResult, 1)
	errCh := make(chan error, 1)

	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	mux := http.NewServeMux()
	// fail reports a bad callback without ever blocking the handler: the
	// caller may already have returned on the first error or result.
	fail := func(w http.ResponseWriter, status int, message string, err error) {
		http.Error(w, message, status)
		select {
		case errCh <- err:
		default:
		}
	}
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			fail(w, http.StatusBadRequest, "Authorization failed: "+errParam, fmt.Errorf("authorization failed: %s", errParam))
			return
		}
		gotState := q.Get("state")
		if gotState != expectedState {
			fail(w, http.StatusBadRequest, "Invalid state parameter", fmt.Errorf("invalid OAuth state parameter"))
			return
		}
		code := q.Get("code")
		if code == "" {
			fail(w, http.StatusBadRequest, "Missing authorization code", fmt.Errorf("missing authorization code in callback"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintln(w, "<html><body><h1>calendar-agent authorised</h1><p>You can close this window.</p></body></html>")
		select {
		case resultCh <- CallbackResult{Code: code}:
		default:
		}
	})
	server.Handler = mux

	go func() {
		_ = server.Serve(listener)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	select {
	case res := <-resultCh:
		return res, nil
	case err := <-errCh:
		return CallbackResult{}, err
	case <-ctx.Done():
		return CallbackResult{}, fmt.Errorf("timed out waiting for OAuth callback after %s", timeout)
	}
}
