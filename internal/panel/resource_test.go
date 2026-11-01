package panel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func testFDCount() (int, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	files, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1, true
	}
	// ReadDir temporarily opens its own FD, which is included in the listing.
	return len(files) - 1, true
}

func TestHTTPResourcePlateauAfterRepeatedPolling(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("ETag", `"fixture"`)
		if r.Header.Get("If-None-Match") == `"fixture"` {
			w.WriteHeader(304)
			return
		}
		fmt.Fprint(w, `{"server_port":1443,"network":"tcp","tls":0}`)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	client := trustedClient(server)
	defer client.HTTP.CloseIdleConnections()
	auth := Credentials{PanelURL: server.URL, Token: "not-for-output-resource-canary", NodeID: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 0; i < 10; i++ {
		if _, err := client.FetchConfig(ctx, auth, ""); err != nil {
			t.Fatal("resource warmup failed")
		}
	}
	client.HTTP.CloseIdleConnections()
	server.CloseClientConnections()
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	g0 := runtime.NumGoroutine()
	fd0, linuxFD := testFDCount()
	for i := 0; i < 300; i++ {
		etag := ""
		if i%2 == 1 {
			etag = `"fixture"`
		}
		if _, err := client.FetchConfig(ctx, auth, etag); err != nil {
			t.Fatal("bounded resource polling failed")
		}
	}
	client.HTTP.CloseIdleConnections()
	server.CloseClientConnections()
	deadline := time.Now().Add(3 * time.Second)
	g1 := runtime.NumGoroutine()
	fd1, _ := testFDCount()
	for (g1 > g0+4 || (linuxFD && fd1 > fd0+4)) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		g1 = runtime.NumGoroutine()
		fd1, _ = testFDCount()
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if requests.Load() != 310 || g1 > g0+4 || (linuxFD && (fd0 < 0 || fd1 < 0 || fd1 > fd0+4)) {
		t.Fatal("HTTP resource plateau regression; no payload emitted")
	}
	t.Logf("host=%s/%s polls=300 goroutines_before=%d goroutines_after=%d heap_alloc_before_gc_bytes=%d heap_alloc_after_gc_bytes=%d", runtime.GOOS, runtime.GOARCH, g0, g1, before.HeapAlloc, after.HeapAlloc)
	if linuxFD {
		t.Logf("test_process_fd_before=%d test_process_fd_after=%d", fd0, fd1)
	} else {
		t.Log("Linux FD measurement: NOT RUN on this host")
	}
}
