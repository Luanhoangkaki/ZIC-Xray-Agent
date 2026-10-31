package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/panel"
)

func fixture(t *testing.T, handler http.HandlerFunc) (*Sender, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client := panel.NewClient()
	client.HTTP.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(client.HTTP.CloseIdleConnections)
	path := filepath.Join(t.TempDir(), "traffic.json")
	l, err := accounting.Open(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	sample := map[string]accounting.Counter{"u": {UserID: 42, Direction: "upload"}}
	if err := l.Observe(path, "epoch", sample); err != nil {
		t.Fatal(err)
	}
	sample["u"] = accounting.Counter{UserID: 42, Direction: "upload", Value: 123}
	if err := l.Observe(path, "epoch", sample); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	return &Sender{Ledger: l, Path: path, Panel: client, Credentials: panel.Credentials{PanelURL: server.URL, Token: "dummy-token", NodeID: 99}, LabEnabled: true}, server
}
func TestAcceptedUsesVerifiedWireShapeWithoutMultiplyingRate(t *testing.T) {
	var calls atomic.Int32
	s, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/server/UniProxy/push" || r.URL.Query().Get("token") != "dummy-token" || r.URL.Query().Get("node_id") != "99" || r.URL.Query().Get("node_type") != "vless" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect verified API request")
		}
		var payload map[string][2]uint64
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["42"] != [2]uint64{123, 0} {
			t.Error("wrong payload or bytes multiplied")
		}
		fmt.Fprint(w, `{"data":true}`)
	})
	if err := s.SendNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Ledger.Queue[0].State != accounting.Accepted {
		t.Fatal("acceptance not recorded")
	}
	if err := s.SendNext(context.Background()); err == nil || calls.Load() != 1 {
		t.Fatal("accepted batch sent twice")
	}
}
func TestUncertainResponsesNeverRetry(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
	}{{500, `{"message":"token is error"}`}, {200, `{"data":false}`}, {200, `{"data":true,"data":false}`}, {200, `<html>error</html>`}, {200, `{"data":true,"error":"later failure"}`}} {
		t.Run(fmt.Sprint(test), func(t *testing.T) {
			var calls atomic.Int32
			s, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			if s.SendNext(context.Background()) == nil || s.Ledger.Queue[0].State != accounting.Unknown {
				t.Fatal("uncertain result not UNKNOWN")
			}
			l, err := accounting.Open(s.Path, "test")
			if err != nil {
				t.Fatal(err)
			}
			s.Ledger = l
			if s.SendNext(context.Background()) == nil || calls.Load() != 1 {
				t.Fatal("UNKNOWN was sent after restart")
			}
		})
	}
}
func TestAppliedThenTimeoutRemainsUnknown(t *testing.T) {
	var applied atomic.Int32
	release := make(chan struct{})
	s, server := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/warm" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var data any
		_ = json.NewDecoder(r.Body).Decode(&data)
		applied.Add(1)
		<-release
		fmt.Fprint(w, `{"data":true}`)
	})
	response, warmErr := s.Panel.HTTP.Get(server.URL + "/warm")
	if warmErr != nil {
		t.Fatal(warmErr)
	}
	response.Body.Close()
	s.Panel.HTTP.Timeout = 300 * time.Millisecond
	err := s.SendNext(context.Background())
	close(release)
	if err == nil || applied.Load() != 1 || s.Ledger.Queue[0].State != accounting.Unknown {
		t.Fatal("lost response incorrectly acknowledged")
	}
	if s.SendNext(context.Background()) == nil || applied.Load() != 1 {
		t.Fatal("applied batch retried")
	}
}
func TestCrashAfterHTTPBeforeAcknowledgementCheckpoint(t *testing.T) {
	var calls atomic.Int32
	s, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"data":true}`) })
	batch, err := s.Ledger.BeginNext(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Panel.PushTrafficLab(context.Background(), s.Credentials, batch.Payload); err != nil {
		t.Fatal(err)
	}
	// Simulate process death before FinishAttempt: only SENDING is on disk.
	s.Ledger, err = accounting.Open(s.Path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if s.Ledger.Queue[0].State != accounting.Unknown || s.SendNext(context.Background()) == nil || calls.Load() != 1 {
		t.Fatal("crash window caused duplicate submission")
	}
}
func TestDefaultAndProductionTargetsCannotSend(t *testing.T) {
	var calls atomic.Int32
	s, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	s.LabEnabled = false
	if s.SendNext(context.Background()) == nil {
		t.Fatal("default sending enabled")
	}
	s.LabEnabled = true
	s.Credentials.PanelURL = "https://panel.example"
	if s.SendNext(context.Background()) == nil || s.Ledger.Queue[0].State != accounting.Pending || calls.Load() != 0 {
		t.Fatal("production target attempted")
	}
}

func TestPostRedirectNeverForwardsEvenWithPermissiveCaller(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	s, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	s.Panel.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	if s.SendNext(context.Background()) == nil || s.Ledger.Queue[0].State != accounting.Unknown || forwarded.Load() != 0 {
		t.Fatal("traffic POST followed redirect or lost UNKNOWN")
	}
}
