package lab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/netutil"
)

func readEmptyDirectory(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	return len(entries) == 0, err
}

// Serve exposes only two source-shaped GET endpoints and a bounded TCP echo.
// /push is always rejected, even with the generated dummy token.
func Serve(ctx context.Context, c Config, cert, key string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	api, err := net.Listen("tcp", c.Address(0))
	if err != nil {
		return errors.New("lab HTTPS port unavailable")
	}
	defer api.Close()
	echo, err := net.Listen("tcp", c.Address(2))
	if err != nil {
		return errors.New("lab echo port unavailable")
	}
	defer echo.Close()
	node, _ := json.Marshal(c.NodeConfig())
	users, _ := json.Marshal(map[string]any{"users": c.Users})
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "GET" {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"lab traffic sending disabled"}`)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(c.Token)) != 1 {
			w.WriteHeader(500)
			io.WriteString(w, `{"message":"token is error"}`)
			return
		}
		if r.URL.Query().Get("node_id") != strconv.FormatInt(NodeID, 10) || r.URL.Query().Get("node_type") != "vless" {
			w.WriteHeader(500)
			io.WriteString(w, `{"message":"server is not exist"}`)
			return
		}
		var body []byte
		switch r.URL.Path {
		case "/api/v1/server/UniProxy/config":
			body = node
		case "/api/v1/server/UniProxy/user":
			body = users
		default:
			w.WriteHeader(404)
			return
		}
		etag := `"` + digest(body) + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", etag)
		w.Write(body)
	})}
	// Echo workers are bounded and each connection has a finite idle deadline.
	capacity := make(chan struct{}, 64)
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			select {
			case capacity <- struct{}{}:
			default:
				conn.Close()
				continue
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-capacity }()
				defer conn.Close()
				buffer := make([]byte, 32<<10)
				for {
					conn.SetDeadline(time.Now().Add(10 * time.Second))
					n, err := conn.Read(buffer)
					if n > 0 {
						if _, writeErr := conn.Write(buffer[:n]); writeErr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	finished := make(chan error, 1)
	go func() { finished <- server.ServeTLS(netutil.LimitListener(api, 8), cert, key) }()
	select {
	case err := <-finished:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("lab HTTPS server stopped unexpectedly")
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdown)
	echo.Close()
	<-acceptDone // No WaitGroup.Add may race with Wait after accept stops.
	workers.Wait()
	return nil
}
