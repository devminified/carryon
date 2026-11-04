package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// loopbackResult is what the browser's redirect back to the local server
// carries: either an authorization code, or an error Supabase/GitHub
// reported (e.g. the user clicked "Cancel" on GitHub's consent screen).
type loopbackResult struct {
	code string
	err  error
}

// loopbackServer is a one-shot local HTTP server for the CLI auth
// redirect: listens on an OS-assigned port on 127.0.0.1, serves exactly
// one /callback request, and reports what it received on result.
type loopbackServer struct {
	addr   string // e.g. "127.0.0.1:54999" — the port is OS-assigned, so this isn't known until after starting
	result <-chan loopbackResult
	close  func()
}

// startLoopbackServer binds an ephemeral port and begins serving. Call
// close() when done, whether or not result ever fires.
func startLoopbackServer() (*loopbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting local callback server: %w", err)
	}

	resultCh := make(chan loopbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if msg := q.Get("error_description"); msg != "" {
			select {
			case resultCh <- loopbackResult{err: errors.New(msg)}:
			default:
			}
			// Deliberately not reflecting msg (server-controlled query
			// content) into the HTML response — that's a reflected-XSS
			// vector for no real benefit. The actual error reaches the
			// user through the CLI's own terminal output instead.
			_, _ = fmt.Fprint(w, "Login failed. You can close this window and check the terminal for details.")
			return
		}
		code := q.Get("code")
		if code == "" {
			select {
			case resultCh <- loopbackResult{err: errors.New("no authorization code in callback")}:
			default:
			}
			_, _ = fmt.Fprint(w, "Login failed: no authorization code received. You can close this window.")
			return
		}
		select {
		case resultCh <- loopbackResult{code: code}:
		default:
		}
		_, _ = fmt.Fprint(w, "Signed in to Carryon. You can close this window and return to the terminal.")
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(listener) }()

	return &loopbackServer{
		addr:   listener.Addr().String(),
		result: resultCh,
		close:  func() { _ = srv.Close() },
	}, nil
}

// wait blocks until the browser redirect arrives or ctx is done — callers
// bound how long to wait by the context they pass (Login uses five
// minutes, long enough to actually complete a GitHub consent screen).
func (s *loopbackServer) wait(ctx context.Context) (string, error) {
	select {
	case res := <-s.result:
		return res.code, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
