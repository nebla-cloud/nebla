package pve_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
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
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: 101, Name: "x", Description: "nebla nb-srv-1 n0nce"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.WaitTask(ctx(t), "pve1", up)
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, pve.ErrTaskFailed) {
		t.Fatalf("failed task is not ErrTaskFailed: %v", err)
	}
	if _, ok := f.VM(101); ok {
		t.Fatal("vm created despite failed task")
	}
	if err := c.WaitTask(ctx(t), "pve1", "UPID:pve1:unknown"); err == nil || errors.Is(err, pve.ErrTaskFailed) {
		t.Fatalf("unknown task: %v", err)
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
	want := map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1"}
	if !maps.Equal(vm.Firewall, want) {
		t.Fatalf("firewall = %v", vm.Firewall)
	}
	got, err := c.FirewallOptions(ctx(t), "pve1", 101)
	if err != nil || !maps.Equal(got, want) {
		t.Fatalf("firewall options = %v, %v", got, err)
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
	if err != nil || len(ss) != 1 || ss[0].ID != "local-lvm" || ss[0].Type != "lvmthin" || !slices.Contains(ss[0].Content, "images") {
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
		{"500 locked", &pve.Error{Status: 500, Message: "VM 101 is locked (clone)"}, true},
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
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: 101, Name: "x", Description: "nebla nb-srv-1 n0nce"})
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
	up, err := c.Clone(ctx(t), "pve1", 9000, pve.CloneParams{NewID: 101, Name: "x", Description: "nebla nb-srv-1 n0nce"})
	if err != nil {
		t.Fatal(err)
	}
	// Like real PVE, the new VMID holds a lock-only config while the clone runs.
	v, ok := f.VM(101)
	if !ok || len(v.Config) != 1 || v.Config["lock"] != "clone" || v.Name != "" {
		t.Fatalf("in-flight clone: %v %+v", ok, v)
	}
	if cfg, err := c.Config(ctx(t), "pve1", 101); err != nil || cfg["lock"] != "clone" {
		t.Fatalf("config of in-flight clone: %v %v", cfg, err)
	}
	if err := c.SetConfig(ctx(t), "pve1", 101, map[string]string{"tags": "x"}); !pve.IsTransient(err) || !strings.Contains(err.Error(), "is locked (clone)") {
		t.Fatalf("set config on locked vm: %v", err)
	}
	if _, err := c.Destroy(ctx(t), "pve1", 101); !pve.IsTransient(err) {
		t.Fatalf("destroy of locked vm: %v", err)
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	v, ok = f.VM(101)
	if !ok || v.Name != "x" || v.Config["lock"] != "" || v.Config["scsi0"] == "" || v.Config["description"] != "nebla nb-srv-1 n0nce" {
		t.Fatalf("clone after task: %v %+v", ok, v)
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

func TestRRDGapsAreNil(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	f.SetRRD([]map[string]any{
		{"time": 1, "cpu": 0.5, "netin": "12"},
		{"time": 2, "cpu": nil, "netin": "NaN", "diskread": "nan"},
		{"time": 3},
	})
	pts, err := c.RRD(ctx(t), "pve1", 101, "hour")
	if err != nil || len(pts) != 3 {
		t.Fatalf("pts = %+v err=%v", pts, err)
	}
	if pts[0].CPU == nil || *pts[0].CPU != 0.5 || pts[0].NetIn == nil || *pts[0].NetIn != 12 || pts[0].DiskRead != nil {
		t.Fatalf("point 0 = %+v", pts[0])
	}
	for _, p := range pts[1:] {
		if p.CPU != nil || p.NetIn != nil || p.DiskRead != nil || p.NetOut != nil {
			t.Fatalf("gap point = %+v", p)
		}
	}
}

func TestAtLeast(t *testing.T) {
	for _, c := range []struct {
		v      string
		maj    int
		min    int
		expect bool
	}{{"8.4.1", 8, 4, true}, {"8.3.5", 8, 4, false}, {"9.0", 8, 4, true}, {"garbage", 8, 4, false}} {
		if got := pve.AtLeast(c.v, c.maj, c.min); got != c.expect {
			t.Errorf("AtLeast(%q,%d,%d) = %v", c.v, c.maj, c.min, got)
		}
	}
}

func TestTemplateBuildCalls(t *testing.T) {
	f, c := setup(t)
	f.AddStorage("pve1", "local", []string{"iso", "import"}, false)
	f.SetNodeVersion("pve1", "9.2.31")
	if v, err := c.NodeVersion(ctx(t), "pve1"); err != nil || v != "9.2.31" {
		t.Fatalf("version = %q err=%v", v, err)
	}
	dl := pve.DownloadParams{URL: "https://img.example/x.qcow2", Filename: "x.qcow2", Content: "import", Checksum: "abc", Algorithm: "sha256"}
	up, err := c.DownloadURL(ctx(t), "pve1", "local", dl)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Volume("local:import/x.qcow2"); !ok {
		t.Fatal("volume missing")
	}
	if d := f.Downloads(); len(d) != 1 || d[0].Checksum != "abc" || d[0].Algorithm != "sha256" || d[0].Storage != "local" {
		t.Fatalf("downloads = %+v", d)
	}
	if _, err := c.DownloadURL(ctx(t), "pve1", "local", dl); err == nil {
		t.Fatal("duplicate download accepted")
	}

	up, err = c.CreateVM(ctx(t), "pve1", 500, map[string]string{"name": "tpl", "tags": "nebla-tpl", "scsi0": "local-lvm:0,import-from=local:import/x.qcow2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	up, err = c.ConvertToTemplate(ctx(t), "pve1", 500)
	if err != nil {
		t.Fatal(err)
	}
	if up == "" {
		t.Fatal("expected a task id")
	}
	if err := c.WaitTask(ctx(t), "pve1", up); err != nil {
		t.Fatal(err)
	}
	vm, ok := f.VM(500)
	if !ok || !vm.Template || vm.Tags != "nebla-tpl" || vm.Config["scsi0"] != "local-lvm:vm-500-disk-0,size=3G" {
		t.Fatalf("vm = %+v", vm)
	}

	up, err = c.CreateVM(ctx(t), "pve1", 501, map[string]string{"scsi0": "local-lvm:0,import-from=local:import/missing.qcow2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx(t), "pve1", up); !errors.Is(err, pve.ErrTaskFailed) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.CreateVM(ctx(t), "pve1", 500, nil); err == nil {
		t.Fatal("duplicate vmid accepted")
	}
}

func TestFailDownload(t *testing.T) {
	f, c := setup(t)
	f.AddStorage("pve1", "local", []string{"import"}, false)
	f.FailDownload("bad.qcow2")
	up, err := c.DownloadURL(ctx(t), "pve1", "local", pve.DownloadParams{URL: "https://x/bad.qcow2", Filename: "bad.qcow2", Content: "import"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.WaitTask(ctx(t), "pve1", up)
	var te *pve.TaskError
	if !errors.As(err, &te) || te.ExitStatus != "checksum mismatch" {
		t.Fatalf("err = %v", err)
	}
}

func TestTemplateFormEncoding(t *testing.T) {
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(b))
		w.Write([]byte(`{"data":null}`))
	}))
	defer srv.Close()
	c := pve.New(pve.Conn{BaseURL: srv.URL, TokenID: "t", Secret: "s", InsecureTLS: true})
	if _, err := c.DownloadURL(ctx(t), "n1", "local", pve.DownloadParams{URL: "https://x/y.img", Filename: "y.img", Content: "import", Checksum: "ff", Algorithm: "sha512"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DownloadURL(ctx(t), "n1", "local", pve.DownloadParams{URL: "https://x/y.img", Filename: "y.img", Content: "import"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateVM(ctx(t), "n1", 700, map[string]string{"name": "a"}); err != nil {
		t.Fatal(err)
	}
	if up, err := c.ConvertToTemplate(ctx(t), "n1", 700); err != nil || up != "" {
		t.Fatalf("up=%q err=%v", up, err)
	}
	want := []string{
		"POST /api2/json/nodes/n1/storage/local/download-url checksum=ff&checksum-algorithm=sha512&content=import&filename=y.img&url=https%3A%2F%2Fx%2Fy.img",
		"POST /api2/json/nodes/n1/storage/local/download-url content=import&filename=y.img&url=https%3A%2F%2Fx%2Fy.img",
		"POST /api2/json/nodes/n1/qemu name=a&vmid=700",
		"POST /api2/json/nodes/n1/qemu/700/template ",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestNormFP(t *testing.T) {
	for in, want := range map[string]string{"ab:cd ef": "ABCDEF", "": "", "AB:CD": "ABCD"} {
		if got := pve.NormFP(in); got != want {
			t.Errorf("NormFP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetFirewallLeavesPolicies(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	if err := c.SetFirewallOptions(ctx(t), "pve1", 101, map[string]string{"policy_in": "DROP"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFirewall(ctx(t), "pve1", 101, []netip.Addr{netip.MustParseAddr("10.0.0.5")}); err != nil {
		t.Fatal(err)
	}
	vm, _ := f.VM(101)
	want := map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "policy_in": "DROP"}
	if !maps.Equal(vm.Firewall, want) || !slices.Equal(vm.IPSets["ipfilter-net0"], []string{"10.0.0.5"}) {
		t.Fatalf("vm = %+v", vm)
	}
}

func TestFirewallRulesRoundTrip(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	for _, cm := range []string{"a", "b", "c"} {
		err := c.AddFirewallRule(ctx(t), "pve1", 101, pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22", Comment: cm, Enable: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.DeleteFirewallRule(ctx(t), "pve1", 101, 1, ""); err != nil {
		t.Fatal(err)
	}
	got, err := c.FirewallRules(ctx(t), "pve1", 101)
	if err != nil || len(got) != 2 {
		t.Fatalf("rules = %+v, %v", got, err)
	}
	if got[0].Pos != 0 || got[0].Comment != "c" || got[1].Pos != 1 || got[1].Comment != "a" || !got[1].Enable || got[1].Dport != "22" {
		t.Fatalf("rules = %+v", got)
	}
	if vm, _ := f.VM(101); len(vm.Rules) != 2 {
		t.Fatalf("fake rules = %+v", vm.Rules)
	}
}

func TestFailNextRuleWrite(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	f.FailNextRuleWrite()
	if err := c.AddFirewallRule(ctx(t), "pve1", 101, pve.FirewallRule{Type: "in", Action: "ACCEPT"}); err == nil {
		t.Fatal("expected failure")
	}
	if err := c.AddFirewallRule(ctx(t), "pve1", 101, pve.FirewallRule{Type: "in", Action: "ACCEPT"}); err != nil {
		t.Fatal(err)
	}
}

func TestClusterFirewallEnabled(t *testing.T) {
	f, c := setup(t)
	on, err := c.ClusterFirewallEnabled(ctx(t))
	if err != nil || !on {
		t.Fatalf("default = %v, %v", on, err)
	}
	f.SetClusterFirewall(false)
	on, err = c.ClusterFirewallEnabled(ctx(t))
	if err != nil || on {
		t.Fatalf("disabled = %v, %v", on, err)
	}
}

func TestFirewallFormEncoding(t *testing.T) {
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		w.Write([]byte(`{"data":null}`))
	}))
	defer srv.Close()
	c := pve.New(pve.Conn{BaseURL: srv.URL, TokenID: "t", Secret: "s", InsecureTLS: true})
	r := pve.FirewallRule{Pos: 2, Type: "in", Action: "ACCEPT", Iface: "net0", Proto: "tcp", Dport: "8000:8100", Source: "1.2.3.4", Dest: "5.6.7.8", Comment: "web", Enable: true}
	if err := c.AddFirewallRule(ctx(t), "n1", 101, r); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteFirewallRule(ctx(t), "n1", 101, 2, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFirewallOptions(ctx(t), "n1", 101, map[string]string{"policy_in": "DROP"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /api2/json/nodes/n1/qemu/101/firewall/rules action=ACCEPT&comment=web&dest=5.6.7.8&dport=8000%3A8100&enable=1&iface=net0&proto=tcp&source=1.2.3.4&type=in",
		"DELETE /api2/json/nodes/n1/qemu/101/firewall/rules/2?digest=abc ",
		"PUT /api2/json/nodes/n1/qemu/101/firewall/options policy_in=DROP",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestFirewallIPSet(t *testing.T) {
	f, c := setup(t)
	f.AddVM("pve1", 101, "a")
	got, ok, err := c.FirewallIPSet(ctx(t), "pve1", 101)
	if err != nil || ok || len(got) != 0 {
		t.Fatalf("missing set = %v, %v, %v", got, ok, err)
	}
	ips := []netip.Addr{netip.MustParseAddr("203.0.113.5"), netip.MustParseAddr("2001:db8::1")}
	if err := c.SetFirewall(ctx(t), "pve1", 101, ips); err != nil {
		t.Fatal(err)
	}
	got, ok, err = c.FirewallIPSet(ctx(t), "pve1", 101)
	if err != nil || !ok || !slices.Equal(got, []string{"203.0.113.5", "2001:db8::1"}) {
		t.Fatalf("set = %v, %v, %v", got, ok, err)
	}
}

func stubClient(t *testing.T, h http.HandlerFunc) pve.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return pve.New(pve.Conn{BaseURL: srv.URL, TokenID: "root@pam!nebla", Secret: "s"})
}

func TestAllocVolumeForm(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Method != http.MethodPost || r.URL.Path != "/api2/json/nodes/pve1/storage/local-lvm/content" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.PostForm.Encode() != "filename=vm-999999999-nb-vol-7&size=20G&vmid=999999999" {
			t.Errorf("form = %s", r.PostForm.Encode())
		}
		fmt.Fprint(w, `{"data":"local-lvm:vm-999999999-nb-vol-7"}`)
	})
	volid, err := c.AllocVolume(ctx(t), "pve1", "local-lvm", 999999999, "vm-999999999-nb-vol-7", 20)
	if err != nil || volid != "local-lvm:vm-999999999-nb-vol-7" {
		t.Fatalf("volid = %q err=%v", volid, err)
	}
}

func TestSetConfigDigestMismatch(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("digest") != "abc" || r.PostForm.Get("virtio1") != "x" {
			t.Errorf("form = %s", r.PostForm.Encode())
		}
		w.WriteHeader(500)
		fmt.Fprint(w, `{"data":null,"message":"detected modified configuration - file changed by other user? Try again. (digest mismatch)"}`)
	})
	err := c.SetConfigDigest(ctx(t), "pve1", 101, map[string]string{"virtio1": "x"}, "abc")
	if !errors.Is(err, pve.ErrDigestMismatch) {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, pve.ErrNotFound) {
		t.Fatal("digest error must not be not-found")
	}
}

func TestStorageContentFilter(t *testing.T) {
	var queries []string
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		fmt.Fprint(w, `{"data":[{"volid":"local-lvm:vm-5-nb-vol-1","size":1073741824,"vmid":5},{"volid":"local-lvm:vm-6-disk-0","size":"2147483648","vmid":"6"}]}`)
	})
	items, err := c.StorageContent(ctx(t), "pve1", "local-lvm", 5)
	if err != nil || len(items) != 2 || items[0] != (pve.StorageItem{Volid: "local-lvm:vm-5-nb-vol-1", Size: 1 << 30, VMID: 5}) || items[1].Size != 2<<30 || items[1].VMID != 6 {
		t.Fatalf("items = %+v err=%v", items, err)
	}
	if _, err := c.StorageContent(ctx(t), "pve1", "local-lvm", 0); err != nil {
		t.Fatal(err)
	}
	if queries[0] != "vmid=5" || queries[1] != "" {
		t.Fatalf("queries = %q", queries)
	}
}

func TestPending(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve1/qemu/101/pending" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"key":"cores","value":2},{"key":"virtio1","pending":"local-lvm:vm-1-x"},{"key":"virtio2","value":"a","delete":1}]}`)
	})
	got, err := c.Pending(ctx(t), "pve1", 101)
	want := []pve.PendingItem{{Key: "cores", Value: "2"}, {Key: "virtio1", Pending: "local-lvm:vm-1-x"}, {Key: "virtio2", Value: "a", Delete: 1}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestPermissions(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api2/json/access/permissions" || r.URL.Query().Get("path") != "/storage/local-lvm" {
			t.Errorf("got %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"data":{"/storage/local-lvm":{"Datastore.Allocate":1,"Datastore.Audit":"0"},"/":{}}}`)
	})
	got, err := c.Permissions(ctx(t), "/storage/local-lvm")
	if err != nil || got["/storage/local-lvm"]["Datastore.Allocate"] != 1 || len(got["/storage/local-lvm"]) != 2 || got["/"] == nil {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestResizeDiskWaitsForTask(t *testing.T) {
	polls := 0
	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api2/json/nodes/pve1/qemu/101/resize":
			fmt.Fprint(w, `{"data":"UPID:pve1:00000001:00000001:00000001:resize:101:root@pam:"}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			polls++
			if polls < 2 {
				fmt.Fprint(w, `{"data":{"status":"running"}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"status":"stopped","exitstatus":"OK"}}`)
		default:
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
	})
	if err := c.ResizeDisk(ctx(t), "pve1", 101, "virtio1", 30); err != nil || polls != 2 {
		t.Fatalf("polls = %d err=%v", polls, err)
	}
	failed := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") {
			fmt.Fprint(w, `{"data":{"status":"stopped","exitstatus":"lvresize failed"}}`)
			return
		}
		fmt.Fprint(w, `{"data":"UPID:pve1:00000001:00000001:00000001:resize:101:root@pam:"}`)
	})
	if err := failed.ResizeDisk(ctx(t), "pve1", 101, "virtio1", 30); !errors.Is(err, pve.ErrTaskFailed) {
		t.Fatalf("err = %v", err)
	}
	// Older PVE answers null.
	plain := stubClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":null}`) })
	if err := plain.ResizeDisk(ctx(t), "pve1", 101, "virtio1", 30); err != nil {
		t.Fatal(err)
	}
}
