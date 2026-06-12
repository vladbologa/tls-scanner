package k8s

import (
	"testing"
)

// newTestClient returns a Client with all internal maps initialised, suitable
// for unit tests that don't need a real Kubernetes API server.
func newTestClient() *Client {
	return &Client{
		processNameMap:    make(map[string]map[int]string),
		listenInfoMap:     make(map[string]map[int]ListenInfo),
		procListenAddrMap: make(map[string]map[int]string),
	}
}

func TestIsLocalhostOnly_procData(t *testing.T) {
	c := newTestClient()

	c.procListenAddrMap["10.0.0.1"] = map[int]string{
		9259: "127.0.0.1",
		9258: "0.0.0.0",
	}

	tests := []struct {
		port     int
		wantIs   bool
		wantAddr string
	}{
		{9259, true, "127.0.0.1"},
		{9258, false, ""},
		{9999, false, ""},
	}

	for _, tt := range tests {
		gotIs, gotAddr := c.IsLocalhostOnly("10.0.0.1", tt.port)
		if gotIs != tt.wantIs || gotAddr != tt.wantAddr {
			t.Errorf("IsLocalhostOnly(port=%d) = (%v, %q), want (%v, %q)",
				tt.port, gotIs, gotAddr, tt.wantIs, tt.wantAddr)
		}
	}
}

func TestIsLocalhostOnly_ipv6Localhost(t *testing.T) {
	c := newTestClient()

	c.procListenAddrMap["10.0.0.2"] = map[int]string{
		8080: "::1",
	}

	gotIs, gotAddr := c.IsLocalhostOnly("10.0.0.2", 8080)
	if !gotIs || gotAddr != "::1" {
		t.Errorf("IsLocalhostOnly() = (%v, %q), want (true, %q)", gotIs, gotAddr, "::1")
	}
}

func TestCacheProcListenInfo(t *testing.T) {
	c := newTestClient()
	pod := PodInfo{IPs: []string{"10.0.0.1"}}
	entries := map[int]ProcListenEntry{
		443:  {Addr: "0.0.0.0", Inode: 12345},
		8080: {Addr: "127.0.0.1", Inode: 99}, // no inode mapping
	}
	inodeComm := map[uint64]string{12345: "nginx"}

	c.cacheProcListenInfo(pod, entries, inodeComm)

	if c.procListenAddrMap["10.0.0.1"][443] != "0.0.0.0" {
		t.Errorf("procListenAddrMap[443] = %q, want 0.0.0.0", c.procListenAddrMap["10.0.0.1"][443])
	}
	if c.processNameMap["10.0.0.1"][443] != "nginx" {
		t.Errorf("processNameMap[443] = %q, want nginx", c.processNameMap["10.0.0.1"][443])
	}
	if _, ok := c.processNameMap["10.0.0.1"][8080]; ok {
		t.Error("expected no process name for port without inode mapping")
	}
}

func TestGetCachedProcessMap(t *testing.T) {
	c := newTestClient()
	c.processNameMap["10.0.0.1"] = map[int]string{443: "nginx", 8443: "sidecar"}

	got := c.GetCachedProcessMap([]string{"10.0.0.1", "10.0.0.2"})
	if got["10.0.0.1"][443] != "nginx" || got["10.0.0.1"][8443] != "sidecar" {
		t.Errorf("GetCachedProcessMap() = %v", got)
	}
	if _, ok := got["10.0.0.2"]; ok {
		t.Error("expected no entry for IP without process data")
	}

	if c.GetCachedProcessMap(nil) != nil {
		t.Error("expected nil for empty ips with no cache")
	}
}

func TestGetListenInfo(t *testing.T) {
	t.Parallel()

	c := newTestClient()
	c.listenInfoMap["10.0.0.1"] = map[int]ListenInfo{
		443: {Port: 443, ListenAddress: "0.0.0.0", ProcessName: "nginx"},
	}

	info, ok := c.GetListenInfo("10.0.0.1", 443)
	if !ok {
		t.Fatal("expected ok=true for known port")
	}
	if info.ListenAddress != "0.0.0.0" {
		t.Errorf("ListenAddress = %q, want %q", info.ListenAddress, "0.0.0.0")
	}

	_, ok = c.GetListenInfo("10.0.0.1", 9999)
	if ok {
		t.Error("expected ok=false for unknown port")
	}

	_, ok = c.GetListenInfo("10.0.0.2", 443)
	if ok {
		t.Error("expected ok=false for unknown IP")
	}
}

func TestGetProcessName(t *testing.T) {
	t.Parallel()

	c := newTestClient()
	c.processNameMap["10.0.0.1"] = map[int]string{
		443: "nginx",
	}

	name, ok := c.GetProcessName("10.0.0.1", 443)
	if !ok || name != "nginx" {
		t.Errorf("GetProcessName() = (%q, %v), want (%q, true)", name, ok, "nginx")
	}

	_, ok = c.GetProcessName("10.0.0.1", 9999)
	if ok {
		t.Error("expected ok=false for unknown port")
	}

	_, ok = c.GetProcessName("10.0.0.2", 443)
	if ok {
		t.Error("expected ok=false for unknown IP")
	}
}

func TestIsLocalhostAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"localhost", true},
		{"0.0.0.0", false},
		{"::", false},
		{"*", false},
		{"10.0.0.1", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isLocalhostAddr(tt.addr)
		if got != tt.want {
			t.Errorf("isLocalhostAddr(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}
