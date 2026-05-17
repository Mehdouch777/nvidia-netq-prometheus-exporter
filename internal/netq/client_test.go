package netq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientLoginAndFetchObjects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/netq/auth/v1/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		json.NewEncoder(w).Encode(LoginResponse{AccessToken: "login-token"})
	})
	mux.HandleFunc("/api/netq/auth/v1/vm-access-token", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer login-token" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		if got := r.URL.Query().Get("expiryDays"); got != "1" {
			t.Fatalf("unexpected expiryDays: %s", got)
		}
		json.NewEncoder(w).Encode(VMTokenResponse{AccessToken: "vm-token", ExpiresAt: 1234})
	})
	mux.HandleFunc("/api/netq/telemetry/v1/object/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Node{{Hostname: "leaf-01", Active: true, AgentState: "Fresh", DBState: "Update", Version: "4.15.0"}})
	})
	mux.HandleFunc("/api/netq/telemetry/v1/object/resource", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]Resource{{Hostname: "leaf-01", CPUUtilization: 11.5, MemUtilization: 37.2}})
	})
	mux.HandleFunc("/api/netq/telemetry/v1/object/sensor", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"hostname":"leaf-01","s_name":"psu1","message_type":"PSU","s_state":"ok","power_input":"500","power_output":450,"voltage_input":"230","voltage_output":"54"}]`))
	})
	mux.HandleFunc("/api/netq/telemetry/v1/object/interface", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"hostname":"leaf-01","ifname":"swp1","admin_state":"up","state":"up","vrf":"default","type":"physical"}]`))
	})
	mux.HandleFunc("/api/netq/telemetry/v1/object/port", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"hostname":"leaf-01","ifname":"swp1","speed":"100G","state":"up"}]`))
	})
	mux.HandleFunc("/api/netq/telemetry/v1/object/bgp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"hostname":"leaf-01","peer_hostname":"spine-01","peer_name":"swp1","peer_asn":65001,"vrf":"default","state":"Established","ipv4_pfx_rcvd":10,"ipv6_pfx_rcvd":0,"evpn_pfx_rcvd":20}]`))
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	client := NewClient(Config{
		Host:               server.URL,
		Username:           "user",
		Password:           "pass",
		InsecureSkipVerify: true,
		Timeout:            5 * time.Second,
	})
	ctx := context.Background()

	token, err := client.Login(ctx)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token != "login-token" {
		t.Fatalf("unexpected token %q", token)
	}

	vm, err := client.VMToken(ctx, token)
	if err != nil {
		t.Fatalf("vm token: %v", err)
	}
	if vm.AccessToken != "vm-token" {
		t.Fatalf("unexpected vm token %q", vm.AccessToken)
	}

	nodes, err := client.Nodes(ctx, token)
	if err != nil || len(nodes) != 1 || nodes[0].Hostname != "leaf-01" {
		t.Fatalf("nodes: %v %+v", err, nodes)
	}
	resources, err := client.Resources(ctx, token)
	if err != nil || len(resources) != 1 || resources[0].CPUUtilization != 11.5 {
		t.Fatalf("resources: %v %+v", err, resources)
	}
	sensors, err := client.Sensors(ctx, token)
	if err != nil || len(sensors) != 1 || sensors[0].Name != "psu1" {
		t.Fatalf("sensors: %v %+v", err, sensors)
	}
	if sensors[0].PowerInput != 500 || sensors[0].VoltageInput != 230 || sensors[0].VoltageOutput != 54 {
		t.Fatalf("unexpected sensor values: %+v", sensors[0])
	}
	interfaces, err := client.Interfaces(ctx, token)
	if err != nil || len(interfaces) != 1 || interfaces[0].IfName != "swp1" || interfaces[0].AdminState != "up" {
		t.Fatalf("interfaces: %v %+v", err, interfaces)
	}
	ports, err := client.Ports(ctx, token)
	if err != nil || len(ports) != 1 || ports[0].Speed != "100G" {
		t.Fatalf("ports: %v %+v", err, ports)
	}
	bgpPeers, err := client.BGPPeers(ctx, token)
	if err != nil || len(bgpPeers) != 1 || bgpPeers[0].PeerHostname != "spine-01" || bgpPeers[0].EVPNPrefixesReceived != 20 {
		t.Fatalf("bgp peers: %v %+v", err, bgpPeers)
	}
}
