package auth

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestWaitForCallbackOnListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan CallbackResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := WaitForCallbackOnListener(listener, "state", 2*time.Second)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	resp, err := http.Get("http://" + listener.Addr().String() + "/?state=state&code=code")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case result := <-resultCh:
		if result.Code != "code" {
			t.Fatalf("code = %q", result.Code)
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("callback timed out")
	}
}
