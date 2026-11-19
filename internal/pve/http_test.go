package pve_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/pve"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/coder/websocket"
)

func setup(t *testing.T) (*fake.Server, pve.Client) {
	t.Helper()
	f := fake.New(t)
	f.AddNode("pve1", 8, 32768)
	f.AddTemplate("pve1", 9000, "local-lvm")
	return f, pve.New(f.Conn())
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestCloneAndWait(t *testing.T) {
	f, c := setup(t)
	id, err := c.NextID(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: id, Name: "web", Target: "pve1", Storage: "local-lvm", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	vm, ok := f.VM(id)
	if !ok || vm.Name != "web" || vm.Config["scsi0"] == "" || vm.Node != "pve1" {
		t.Fatalf("vm = %+v ok=%v", vm, ok)
	}
	vms, err := c.VMs(ctx(t))
	if err != nil || len(vms) != 2 {
		t.Fatalf("vms = %+v err=%v", vms, err)
	}
	if next, _ := c.NextID(ctx(t)); next != id+1 {
		t.Fatalf("next = %d", next)
	}
}

func TestWaitTaskReportsFailure(t *testing.T) {
	f, c := setup(t)
	f.FailNext("/clone", 500)
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: 101, Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.WaitTask(ctx(t), "pve1", up)
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := f.VM(101); ok {
		t.Fatal("vm created despite failed task")
	}
}

func TestSetFirewallIPSet(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	ips := []netip.Addr{netip.MustParseAddr("203.0.113.5"), netip.MustParseAddr("2001:db8::1")}
	for range 2 {
		if err := c.SetFirewall(ctx(t), "pve1", 101, ips); err != nil {
			t.Fatal(err)
		}
	}
	vm, _ := f.VM(101)
	if !slices.Equal(vm.IPSets["ipfilter-net0"], []string{"203.0.113.5", "2001:db8::1"}) || vm.Firewall["ipfilter"] != "1" {
		t.Fatalf("vm = %+v", vm)
	}
	if err := c.SetFirewall(ctx(t), "pve1", 101, ips[:1]); err != nil {
		t.Fatal(err)
	}
	vm, _ = f.VM(101)
	if !slices.Equal(vm.IPSets["ipfilter-net0"], []string{"203.0.113.5"}) {
		t.Fatalf("ipset = %v", vm.IPSets)
	}
}

func TestNotFoundMapped(t *testing.T) {
	_, c := setup(t)
	err := c.SetConfig(ctx(t), "pve1", 555, map[string]string{"memory": "1024"})
	if !errors.Is(err, pve.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if pve.IsTransient(err) {
		t.Fatal("not found is not transient")
	}
}

func TestAuthHeaderFormat(t *testing.T) {
	f, c := setup(t)
	cn := f.Conn()
	want := "PVEAPIToken=" + cn.TokenID + "=" + cn.Secret
	if got := c.AuthHeader().Get("Authorization"); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestTLSModes(t *testing.T) {
	f, _ := setup(t)
	cn := f.Conn()
	bad := cn
	bad.CAFingerprint = strings.Repeat("00:", 31) + "00"
	if _, err := pve.New(bad).Version(ctx(t)); !errors.Is(err, pve.ErrFingerprint) || pve.IsTransient(err) {
		t.Fatalf("wrong fingerprint: %v", err)
	}
	none := cn
	none.CAFingerprint = ""
	if _, err := pve.New(none).Version(ctx(t)); err == nil || pve.IsTransient(err) {
		t.Fatalf("untrusted cert: %v", err)
	}
	none.InsecureTLS = true
	if v, err := pve.New(none).Version(ctx(t)); err != nil || v == "" {
		t.Fatalf("insecure: %v %q", err, v)
	}
	if v, err := pve.New(cn).Version(ctx(t)); err != nil || v == "" {
		t.Fatalf("pinned: %v %q", err, v)
	}
}

func TestBadToken(t *testing.T) {
	f, _ := setup(t)
	cn := f.Conn()
	cn.Secret = "nope"
	_, err := pve.New(cn).Version(ctx(t))
	var pe *pve.Error
	if !errors.As(err, &pe) || pe.Status != 401 {
		t.Fatalf("err = %v", err)
	}
}

func TestInventory(t *testing.T) {
	f, c := setup(t)
	f.SetNodeLoad("pve1", 0.5, 1024)
	ns, err := c.Nodes(ctx(t))
	if err != nil || len(ns) != 1 || ns[0].Name != "pve1" || ns[0].MaxCPU != 8 || ns[0].CPU != 0.5 || ns[0].Mem != 1024<<20 || ns[0].MaxMem != 32768<<20 {
		t.Fatalf("nodes = %+v err=%v", ns, err)
	}
	ss, err := c.Storages(ctx(t), "pve1")
	if err != nil || len(ss) != 1 || ss[0].ID != "local-lvm" || !slices.Contains(ss[0].Content, "images") {
		t.Fatalf("storages = %+v err=%v", ss, err)
	}
}

func TestLifecycle(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	wait := func(up pve.UPID, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.SetConfig(ctx(t), "pve1", 101, map[string]string{"tags": "nebla;nb-srv-7", "memory": "2048"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ResizeDisk(ctx(t), "pve1", 101, "scsi0", 40); err != nil {
		t.Fatal(err)
	}
	cfg, err := c.Config(ctx(t), "pve1", 101)
	if err != nil || cfg["memory"] != "2048" || !strings.Contains(cfg["scsi0"], "size=40G") {
		t.Fatalf("cfg = %v err=%v", cfg, err)
	}
	wait(c.Power(ctx(t), "pve1", 101, pve.PowerStart))
	if vm, _ := f.VM(101); vm.Status != "running" || vm.Tags != "nebla;nb-srv-7" {
		t.Fatalf("vm = %+v", vm)
	}
	wait(c.Power(ctx(t), "pve1", 101, pve.PowerStop))
	if vm, _ := f.VM(101); vm.Status != "stopped" {
		t.Fatalf("vm = %+v", vm)
	}
	wait(c.Destroy(ctx(t), "pve1", 101))
	if _, ok := f.VM(101); ok {
		t.Fatal("vm not destroyed")
	}
}

func TestISOUploadDelete(t *testing.T) {
	f, c := setup(t)
	f.AddStorage("pve1", "local", []string{"iso"}, false)
	if err := c.UploadISO(ctx(t), "pve1", "local", "ci-1.iso", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatal(err)
	}
	if b, ok := f.Volume("local:iso/ci-1.iso"); !ok || string(b) != "data" {
		t.Fatalf("volume = %q %v", b, ok)
	}
	if err := c.DeleteVolume(ctx(t), "pve1", "local", "local:iso/ci-1.iso"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Volume("local:iso/ci-1.iso"); ok {
		t.Fatal("volume remains")
	}
}

func TestVNCEcho(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	tk, err := c.VNCProxy(ctx(t), "pve1", 101)
	if err != nil || tk.Ticket == "" || tk.Port == 0 {
		t.Fatalf("ticket = %+v err=%v", tk, err)
	}
	url := c.VNCWebsocketURL("pve1", 101, tk)
	conn, _, err := websocket.Dial(ctx(t), url, &websocket.DialOptions{HTTPClient: f.HTTPClient(), HTTPHeader: c.AuthHeader()})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx(t), websocket.MessageBinary, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	_, b, err := conn.Read(ctx(t))
	if err != nil || string(b) != "hi" {
		t.Fatalf("echo = %q %v", b, err)
	}
	tk.Ticket = "bad"
	if _, _, err := websocket.Dial(ctx(t), c.VNCWebsocketURL("pve1", 101, tk), &websocket.DialOptions{HTTPClient: f.HTTPClient(), HTTPHeader: c.AuthHeader()}); err == nil {
		t.Fatal("bad ticket accepted")
	}
}

func TestRRDAndHooks(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	pts, err := c.RRD(ctx(t), "pve1", 101, "hour")
	if err != nil || len(pts) == 0 || pts[0].Time == 0 {
		t.Fatalf("pts = %+v err=%v", pts, err)
	}
	f.AddNode("pve2", 4, 8192)
	f.MoveVM(101, "pve2")
	if err := c.SetConfig(ctx(t), "pve1", 101, map[string]string{"memory": "1"}); !errors.Is(err, pve.ErrNotFound) {
		t.Fatalf("old node err = %v", err)
	}
	f.SetVMStatus(101, "running")
	vms, _ := c.VMs(ctx(t))
	if i := slices.IndexFunc(vms, func(v pve.VM) bool { return v.VMID == 101 }); i < 0 || vms[i].Node != "pve2" || vms[i].Status != "running" {
		t.Fatalf("vms = %+v", vms)
	}
	if !slices.Contains(f.Calls(), "GET /nodes/pve1/qemu/101/rrddata") {
		t.Fatalf("calls = %v", f.Calls())
	}
	f.FailNext("/version", 503)
	_, err = c.Version(ctx(t))
	if !pve.IsTransient(err) {
		t.Fatalf("503 should be transient: %v", err)
	}
	if _, err := c.Version(ctx(t)); err != nil {
		t.Fatal(err)
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"502", &pve.Error{Status: 502, Message: "bad gateway"}, true},
		{"503", &pve.Error{Status: 503, Message: "x"}, true},
		{"504", &pve.Error{Status: 504, Message: "x"}, true},
		{"500 timeout", &pve.Error{Status: 500, Message: "got timeout"}, true},
		{"500 lock", &pve.Error{Status: 500, Message: "can't lock file '/var/lock/qemu-server/lock-101.conf' - got timeout"}, true},
		{"500 refused", &pve.Error{Status: 500, Message: "Connection refused"}, true},
		{"500 unable", &pve.Error{Status: 500, Message: "unable to connect to VM 101 qmp socket"}, true},
		{"500 exists", &pve.Error{Status: 500, Message: "unable to create VM 101: config file already exists"}, false},
		{"500 running", &pve.Error{Status: 500, Message: "VM 101 is running - destroy failed"}, false},
		{"500 missing", &pve.Error{Status: 500, Message: "Configuration file does not exist"}, false},
		{"400", &pve.Error{Status: 400, Message: "bad"}, false},
		{"401", &pve.Error{Status: 401, Message: "no ticket"}, false},
		{"404", &pve.Error{Status: 404, Message: "x"}, false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"fingerprint", fmt.Errorf("dial: %w", pve.ErrFingerprint), false},
		{"net", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"plain", errors.New("boom"), false},
	}
	for _, c := range cases {
		if got := pve.IsTransient(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestWaitTaskToleratesPollErrors(t *testing.T) {
	f, c := setup(t)
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: 101, Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		f.FailNext("/status", 503)
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatalf("3 transient errors should be tolerated: %v", err)
	}
	for range 4 {
		f.FailNext("/status", 503)
	}
	err = c.WaitTask(ctx(t), "pve1", up)
	var pe *pve.Error
	if !errors.As(err, &pe) || pe.Status != 503 {
		t.Fatalf("4th consecutive error should abort: %v", err)
	}
	f.FailNext("/status", 400)
	if err := c.WaitTask(ctx(t), "pve1", up); err == nil {
		t.Fatal("permanent poll error should abort")
	}
}

func TestFakeRejectsLikeRealPVE(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	waitErr := func(up pve.UPID, err error) error {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return c.WaitTask(ctx(t), "pve1", up)
	}
	if err := waitErr(c.Power(ctx(t), "pve1", 101, pve.PowerStop)); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stop stopped vm: %v", err)
	}
	if err := waitErr(c.Power(ctx(t), "pve1", 101, pve.PowerStart)); err != nil {
		t.Fatal(err)
	}
	if err := waitErr(c.Power(ctx(t), "pve1", 101, pve.PowerStart)); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("start running vm: %v", err)
	}
	if _, err := c.Destroy(ctx(t), "pve1", 101); err == nil || !strings.Contains(err.Error(), "VM 101 is running") {
		t.Fatalf("destroy running vm: %v", err)
	}
	if _, ok := f.VM(101); !ok {
		t.Fatal("running vm was destroyed")
	}
	if err := c.ResizeDisk(ctx(t), "pve1", 101, "scsi0", 5); err == nil {
		t.Fatal("shrink accepted")
	}
	if err := c.SetConfig(ctx(t), "pve1", 101, map[string]string{"cores": "4", "tags": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetConfig(ctx(t), "pve1", 101, map[string]string{"delete": "tags,cores"}); err != nil {
		t.Fatal(err)
	}
	vm, _ := f.VM(101)
	if _, ok := vm.Config["tags"]; ok || vm.Config["cores"] != "" || vm.Config["delete"] != "" {
		t.Fatalf("config = %v", vm.Config)
	}
}

func TestTaskEffectsApplyOnCompletion(t *testing.T) {
	f, c := setup(t)
	f.SetTaskDelay(200 * time.Millisecond)
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: 101, Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.VM(101); ok {
		t.Fatal("clone applied before task completed")
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.VM(101); !ok {
		t.Fatal("clone missing after task")
	}
	f.SetTaskDelay(0)
	up, err = c.Destroy(ctx(t), "pve1", 101)
	if err != nil {
		t.Fatal(err)
	}
	f.SetTaskDelay(0)
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.VM(101); ok {
		t.Fatal("destroy not applied")
	}
}

func TestDeleteVolumeNeedsEscapedVolid(t *testing.T) {
	f, _ := setup(t)
	f.AddStorage("pve1", "local", []string{"iso"}, false)
	cn := f.Conn()
	req, _ := http.NewRequest(http.MethodDelete, cn.BaseURL+"/api2/json/nodes/pve1/storage/local/content/local:iso/x.iso", nil)
	req.Header = pve.New(cn).AuthHeader()
	res, err := f.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unescaped slash status = %d", res.StatusCode)
	}
}
