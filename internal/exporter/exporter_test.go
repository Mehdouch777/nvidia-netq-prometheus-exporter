package exporter

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"repos.apps.bluvalt.com/BluvaltCloud/netq-exporter/internal/netq"
)

type fakeClient struct {
	loginToken string
	nodes      []netq.Node
	resources  []netq.Resource
	sensors    []netq.Sensor
	interfaces []netq.Interface
	ports      []netq.Port
	bgpPeers   []netq.BGPPeer
	vmErr      error
}

func (f fakeClient) Login(context.Context) (string, error) { return f.loginToken, nil }
func (f fakeClient) VMToken(context.Context, string) (netq.VMTokenResponse, error) {
	if f.vmErr != nil {
		return netq.VMTokenResponse{}, f.vmErr
	}
	return netq.VMTokenResponse{AccessToken: "vm"}, nil
}
func (f fakeClient) Nodes(context.Context, string) ([]netq.Node, error) { return f.nodes, nil }
func (f fakeClient) Resources(context.Context, string) ([]netq.Resource, error) {
	return f.resources, nil
}
func (f fakeClient) Sensors(context.Context, string) ([]netq.Sensor, error) { return f.sensors, nil }
func (f fakeClient) Interfaces(context.Context, string) ([]netq.Interface, error) {
	return f.interfaces, nil
}
func (f fakeClient) Ports(context.Context, string) ([]netq.Port, error)       { return f.ports, nil }
func (f fakeClient) BGPPeers(context.Context, string) ([]netq.BGPPeer, error) { return f.bgpPeers, nil }

func TestPollPopulatesMetrics(t *testing.T) {
	exp := New(fakeClient{
		loginToken: "login",
		nodes:      []netq.Node{{Hostname: "leaf-01", Active: true, AgentState: "Fresh", DBState: "Update", Version: "4.15.0"}},
		resources:  []netq.Resource{{Hostname: "leaf-01", CPUUtilization: 12.5, MemUtilization: 44.3}},
		sensors:    []netq.Sensor{{Hostname: "leaf-01", Name: "psu1", MessageType: "PSU", State: "ok", PowerInput: 500, PowerOutput: 450, VoltageInput: 230, VoltageOutput: 54}},
		interfaces: []netq.Interface{{Hostname: "leaf-01", IfName: "swp1", AdminState: "up", State: "up", VRF: "default", Type: "physical", LastChanged: 1715000000}},
		ports:      []netq.Port{{Hostname: "leaf-01", IfName: "swp1", Speed: "100G"}},
		bgpPeers:   []netq.BGPPeer{{Hostname: "leaf-01", PeerHostname: "spine-01", PeerName: "swp1", PeerASN: 65001, VRF: "default", State: "Established", UpTime: 7200, ConnDropped: 2, ConnEstd: 5, UpdatesReceived: 101, UpdatesSent: 77, Reason: "Hold Timer Expired", IPv4PrefixesReceived: 10, IPv6PrefixesReceived: 0, EVPNPrefixesReceived: 20}},
	}, time.Minute, func() time.Time { return time.Unix(100, 0) })

	if err := exp.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(exp)

	if got := testutil.ToFloat64(exp.nodeActive.WithLabelValues("leaf-01")); got != 1 {
		t.Fatalf("netq_node_active=%v, want 1", got)
	}
	if got := testutil.ToFloat64(exp.resourceCPU.WithLabelValues("leaf-01")); got != 12.5 {
		t.Fatalf("netq_device_cpu_utilization_percent=%v, want 12.5", got)
	}
	if got := testutil.ToFloat64(exp.sensorOK.WithLabelValues("leaf-01", "psu1", "PSU")); got != 1 {
		t.Fatalf("netq_sensor_ok=%v, want 1", got)
	}
	if got := testutil.ToFloat64(exp.interfaceAdminUp.WithLabelValues("leaf-01", "swp1", "default", "physical")); got != 1 {
		t.Fatalf("netq_interface_admin_up=%v, want 1", got)
	}
	if got := testutil.ToFloat64(exp.interfaceLastChange.WithLabelValues("leaf-01", "swp1", "default", "physical")); got != 1715000000 {
		t.Fatalf("netq_interface_last_change_timestamp_seconds=%v, want 1715000000", got)
	}
	if got := testutil.ToFloat64(exp.interfaceSpeedMbps.WithLabelValues("leaf-01", "swp1")); got != 100000 {
		t.Fatalf("netq_interface_speed_mbps=%v, want 100000", got)
	}
	if got := testutil.ToFloat64(exp.bgpPeerUp.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default")); got != 1 {
		t.Fatalf("netq_bgp_peer_up=%v, want 1", got)
	}
	if got := testutil.ToFloat64(exp.bgpSessionUptime.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default")); got != 7200 {
		t.Fatalf("netq_bgp_session_uptime_seconds=%v, want 7200", got)
	}
	if got := testutil.ToFloat64(exp.bgpPeerReasonInfo.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default", "Hold Timer Expired")); got != 1 {
		t.Fatalf("netq_bgp_peer_reason_info=%v, want 1", got)
	}
	if got := testutil.ToFloat64(exp.bgpConnDropped.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default")); got != 2 {
		t.Fatalf("netq_bgp_connections_dropped=%v, want 2", got)
	}
	if got := testutil.ToFloat64(exp.bgpConnEstd.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default")); got != 5 {
		t.Fatalf("netq_bgp_connections_established=%v, want 5", got)
	}
	if got := testutil.ToFloat64(exp.bgpUpdatesReceived.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default")); got != 101 {
		t.Fatalf("netq_bgp_updates_received=%v, want 101", got)
	}
	if got := testutil.ToFloat64(exp.bgpUpdatesSent.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default")); got != 77 {
		t.Fatalf("netq_bgp_updates_sent=%v, want 77", got)
	}
	if got := testutil.ToFloat64(exp.bgpPrefixesReceived.WithLabelValues("leaf-01", "spine-01", "swp1", "65001", "default", "evpn")); got != 20 {
		t.Fatalf("netq_bgp_prefixes_received=%v, want 20", got)
	}
	if got := testutil.ToFloat64(exp.objectsReturned.WithLabelValues("interface")); got != 1 {
		t.Fatalf("netq_exporter_objects_returned{resource=interface}=%v, want 1", got)
	}
	if got := testutil.ToFloat64(exp.lastSuccessTS); got != 100 {
		t.Fatalf("last success=%v, want 100", got)
	}
}

func TestReadyHandlerBeforeSuccessfulPoll(t *testing.T) {
	exp := New(fakeClient{}, time.Minute, time.Now)
	rr := httptest.NewRecorder()
	exp.ReadyHandler(rr, nil)
	if rr.Code != 503 {
		t.Fatalf("status=%d, want 503", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not ready") {
		t.Fatalf("unexpected body %q", rr.Body.String())
	}
}

func TestVMTokFailureIsNonFatal(t *testing.T) {
	exp := New(fakeClient{
		loginToken: "login",
		vmErr:      errors.New("vm disabled"),
		nodes:      []netq.Node{},
		resources:  []netq.Resource{},
		sensors:    []netq.Sensor{},
		interfaces: []netq.Interface{},
		ports:      []netq.Port{},
		bgpPeers:   []netq.BGPPeer{},
	}, time.Minute, time.Now)
	if err := exp.poll(context.Background()); err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if got := testutil.ToFloat64(exp.apiFailures.WithLabelValues("vm_token")); got != 1 {
		t.Fatalf("vm token failures=%v, want 1", got)
	}
}

func TestParseSpeedMbps(t *testing.T) {
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{in: "100G", want: 100000, ok: true},
		{in: "25G", want: 25000, ok: true},
		{in: "100M", want: 100, ok: true},
		{in: "n/a", want: 0, ok: false},
	}

	for _, tc := range tests {
		got, ok := parseSpeedMbps(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("parseSpeedMbps(%q)=(%v,%v), want (%v,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestReasonLabel(t *testing.T) {
	if got := reasonLabel("  "); got != "none" {
		t.Fatalf("reasonLabel(blank)=%q, want none", got)
	}
	if got := reasonLabel("Cease"); got != "Cease" {
		t.Fatalf("reasonLabel(Cease)=%q, want Cease", got)
	}
}
