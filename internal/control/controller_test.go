package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"zic-xray-agent/internal/panel"
)

type fakeCore struct {
	exists                            bool
	hash                              string
	adds, removes                     int
	crashAdd, crashRemove, crashUsers bool
	users                             []panel.User
}

func (f *fakeCore) PlanFingerprint(tag, listen string, spec panel.VLESSSpec) (string, error) {
	return fmt.Sprintf("%s:%s:%d:%d", tag, listen, spec.Port, spec.TLS), nil
}
func (f *fakeCore) InspectInbound(context.Context, string) (string, bool, error) {
	return f.hash, f.exists, nil
}
func (f *fakeCore) AddInbound(_ context.Context, tag, listen string, spec panel.VLESSSpec) error {
	f.adds++
	f.exists = true
	f.hash, _ = f.PlanFingerprint(tag, listen, spec)
	if f.crashAdd {
		f.crashAdd = false
		panic("crash after AddInbound")
	}
	return nil
}
func (f *fakeCore) RemoveInbound(context.Context, string) error {
	f.removes++
	f.exists = false
	f.hash = ""
	if f.crashRemove {
		f.crashRemove = false
		panic("crash after RemoveInbound")
	}
	return nil
}
func (f *fakeCore) ReconcileUsers(_ context.Context, _ string, _ int64, _ string, users []panel.User, before func() error) error {
	if len(f.users) > 0 {
		if err := before(); err != nil {
			return err
		}
	}
	f.users = append([]panel.User(nil), users...)
	if f.crashUsers {
		f.crashUsers = false
		panic("crash after users")
	}
	return nil
}
func raw(port int) []byte {
	return []byte(fmt.Sprintf(`{"server_port":%d,"network":"tcp","tls":0}`, port))
}
func controller(t *testing.T, f *fakeCore, path string) *Controller {
	t.Helper()
	c := &Controller{Core: f, Path: path, Identity: "test", NodeID: 42, ListenIP: "127.0.0.1", Epoch: func() (string, error) { return "core", nil }, BeforeRemove: func(context.Context) error { return nil }}
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	return c
}
func crashCall(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("crash was not injected")
		}
	}()
	action()
}

func TestCrashAfterCreateAndUsersResumesByInspection(t *testing.T) {
	for _, point := range []string{"add", "users"} {
		t.Run(point, func(t *testing.T) {
			f := &fakeCore{crashAdd: point == "add", crashUsers: point == "users"}
			path := filepath.Join(t.TempDir(), "controller.json")
			c := controller(t, f, path)
			users := []panel.User{{ID: 1, UUID: "15a4235c-39ac-42ea-a1b1-100000000042"}}
			crashCall(t, func() { _ = c.Apply(context.Background(), raw(12345), users) })
			c = controller(t, f, path)
			if err := c.Apply(context.Background(), raw(12345), users); err != nil {
				t.Fatal(err)
			}
			if f.adds != 1 || c.state.Pending != nil || c.state.Phase != "applied" {
				t.Fatal("crash recovery replayed creation or did not commit")
			}
		})
	}
}
func TestTransportReplacementCrashRestoresDesiredConfig(t *testing.T) {
	f := &fakeCore{}
	path := filepath.Join(t.TempDir(), "controller.json")
	c := controller(t, f, path)
	if err := c.Apply(context.Background(), raw(12345), nil); err != nil {
		t.Fatal(err)
	}
	f.crashRemove = true
	crashCall(t, func() { _ = c.Apply(context.Background(), raw(12346), nil) })
	c = controller(t, f, path)
	if err := c.Apply(context.Background(), raw(12346), nil); err != nil {
		t.Fatal(err)
	}
	if f.removes != 1 || f.adds != 2 || !f.exists {
		t.Fatal("replacement was not safely resumed")
	}
	f.exists = false
	f.hash = "" // fresh core after reboot: dynamic inbound absent
	if err := c.Apply(context.Background(), raw(12346), nil); err != nil {
		t.Fatal(err)
	}
	if f.adds != 3 {
		t.Fatal("missing dynamic inbound was not restored")
	}
}
func TestUnownedOrDriftedInboundNeverOverwritten(t *testing.T) {
	f := &fakeCore{exists: true, hash: "operator-config"}
	c := controller(t, f, filepath.Join(t.TempDir(), "controller.json"))
	if c.Apply(context.Background(), raw(12345), nil) == nil || f.removes != 0 || f.adds != 0 {
		t.Fatal("unowned inbound changed")
	}
	f.exists = false
	f.hash = ""
	if err := c.Apply(context.Background(), raw(12345), nil); err != nil {
		t.Fatal(err)
	}
	f.hash = "operator-drift"
	if c.Apply(context.Background(), raw(12346), nil) == nil || f.removes != 0 {
		t.Fatal("externally changed inbound overwritten")
	}
}
func TestUnsupportedLimitsDisableOwnedInbound(t *testing.T) {
	f := &fakeCore{}
	c := controller(t, f, filepath.Join(t.TempDir(), "controller.json"))
	if err := c.Apply(context.Background(), raw(12345), nil); err != nil {
		t.Fatal(err)
	}
	limit := int64(10)
	if c.Apply(context.Background(), raw(12345), []panel.User{{ID: 1, SpeedLimit: &limit}}) == nil || f.exists {
		t.Fatal("unsupported limit silently bypassed")
	}
}
func TestLostStatsDoesNotLeaveRevokedInboundListening(t *testing.T) {
	f := &fakeCore{}
	c := controller(t, f, filepath.Join(t.TempDir(), "controller.json"))
	if err := c.Apply(context.Background(), raw(12345), nil); err != nil {
		t.Fatal(err)
	}
	c.BeforeRemove = func(context.Context) error { return errors.New("stats unavailable") }
	if c.Disable(context.Background()) == nil || f.exists {
		t.Fatal("revocation lost priority or gap was hidden")
	}
}

func TestFatalFenceDoesNotWriteAgainFromPossiblyStaleRAM(t *testing.T) {
	f := &fakeCore{}
	c := controller(t, f, filepath.Join(t.TempDir(), "controller.json"))
	if err := c.Apply(context.Background(), raw(12345), nil); err != nil {
		t.Fatal(err)
	}
	c.BeforeRemove = func(context.Context) error { t.Fatal("fault containment retried a checkpoint"); return nil }
	if err := c.Fence(context.Background()); err != nil || f.exists {
		t.Fatal("fatal containment did not close owned inbound", err)
	}
}
