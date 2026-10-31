package traffic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/panel"
)

func TestSenderProcessCrashHelper(t *testing.T) {
	if os.Getenv("ZIC_SEND_CRASH_HELPER") != "1" {
		return
	}
	path := os.Getenv("ZIC_TEST_LEDGER")
	unlock, err := durable.Lock(filepath.Join(filepath.Dir(path), "agent.lock"))
	if err != nil {
		os.Exit(24)
	}
	defer unlock()
	l, err := accounting.Open(path, "test")
	if err != nil {
		os.Exit(25)
	}
	batch, err := l.BeginNext(path)
	if err != nil {
		os.Exit(26)
	}
	if os.Getenv("ZIC_TEST_POST") == "1" {
		pool := x509.NewCertPool()
		ca, err := os.ReadFile(os.Getenv("ZIC_TEST_CA"))
		if err != nil || !pool.AppendCertsFromPEM(ca) {
			os.Exit(27)
		}
		client := panel.NewClient()
		client.HTTP.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		if client.PushTrafficLab(context.Background(), panel.Credentials{PanelURL: os.Getenv("ZIC_TEST_URL"), Token: "dummy-token", NodeID: 99}, batch.Payload) != nil {
			os.Exit(28)
		}
	}
	os.Exit(23) // no defers, no FinishAttempt, simulates abrupt process death
}
func TestActualProcessDeathBeforeAndAfterPOSTNeverReplays(t *testing.T) {
	for _, post := range []bool{false, true} {
		t.Run(fmt.Sprint(post), func(t *testing.T) {
			var calls atomic.Int32
			s, server := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"data":true}`) })
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			caPath := filepath.Join(filepath.Dir(s.Path), "public-ca.pem")
			if os.WriteFile(caPath, ca, 0600) != nil {
				t.Fatal("write test CA")
			}
			command := exec.Command(os.Args[0], "-test.run=^TestSenderProcessCrashHelper$")
			postValue := "0"
			if post {
				postValue = "1"
			}
			command.Env = append(os.Environ(), "ZIC_SEND_CRASH_HELPER=1", "ZIC_TEST_LEDGER="+s.Path, "ZIC_TEST_POST="+postValue, "ZIC_TEST_CA="+caPath, "ZIC_TEST_URL="+server.URL)
			err := command.Run()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 23 {
				t.Fatal("crash boundary not reached", err)
			}
			onDisk, err := accounting.Inspect(s.Path)
			if err != nil || onDisk.Queue[0].State != accounting.Sending {
				t.Fatal("SENDING was not durable before crash", err)
			}
			s.Ledger, err = accounting.Open(s.Path, "test")
			if err != nil {
				t.Fatal(err)
			}
			if s.Ledger.Queue[0].State != accounting.Unknown || s.SendNext(context.Background()) == nil {
				t.Fatal("interrupted attempt was not quarantined")
			}
			want := int32(0)
			if post {
				want = 1
			}
			if calls.Load() != want {
				t.Fatal("uncertain attempt was replayed")
			}
		})
	}
}
