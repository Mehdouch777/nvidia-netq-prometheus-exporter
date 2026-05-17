package netq

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host               string
	Username           string
	Password           string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

const (
	loginEndpoint           = "/api/netq/auth/v1/login"
	vmTokenEndpoint         = "/api/netq/auth/v1/vm-access-token?expiryDays=1"
	telemetryObjectEndpoint = "/api/netq/telemetry/v1/object/"
)

// Client wraps the small subset of NetQ APIs the exporter currently needs.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// LoginResponse is returned by the NetQ login endpoint.
type LoginResponse struct {
	AccessToken string `json:"access_token"`
}

// VMTokenResponse is returned by the VM access token endpoint.
type VMTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

// Node models telemetry returned by the NetQ node object API.
type Node struct {
	Hostname   string `json:"hostname"`
	Active     bool   `json:"active"`
	AgentState string `json:"agent_state"`
	DBState    string `json:"db_state"`
	Timestamp  int64  `json:"timestamp"`
	Version    string `json:"version"`
}

// Resource models host resource telemetry returned by NetQ.
type Resource struct {
	Hostname       string  `json:"hostname"`
	CPUUtilization float64 `json:"cpu_utilization"`
	MemUtilization float64 `json:"mem_utilization"`
	Timestamp      int64   `json:"timestamp"`
}

// Sensor models sensor telemetry returned by NetQ.
type Sensor struct {
	Hostname      string        `json:"hostname"`
	MessageType   string        `json:"message_type"`
	Name          string        `json:"s_name"`
	State         string        `json:"s_state"`
	PowerInput    FlexibleFloat `json:"power_input"`
	PowerOutput   FlexibleFloat `json:"power_output"`
	VoltageInput  FlexibleFloat `json:"voltage_input"`
	VoltageOutput FlexibleFloat `json:"voltage_output"`
	Timestamp     int64         `json:"timestamp"`
}

// Interface models interface state returned by NetQ.
type Interface struct {
	AdminState  string `json:"admin_state"`
	DBState     string `json:"db_state"`
	Details     string `json:"details"`
	Hostname    string `json:"hostname"`
	IfAlias     string `json:"ifalias"`
	IfName      string `json:"ifname"`
	IsSVD       bool   `json:"is_svd"`
	LastChanged int64  `json:"last_changed"`
	OpID        int64  `json:"opid"`
	State       string `json:"state"`
	Timestamp   int64  `json:"timestamp"`
	Type        string `json:"type"`
	VRF         string `json:"vrf"`
}

// Port models port inventory and speed telemetry returned by NetQ.
type Port struct {
	AdvertisedFEC string `json:"advertised_fec"`
	Autoneg       string `json:"autoneg"`
	Connector     string `json:"connector"`
	DBState       string `json:"db_state"`
	FEC           string `json:"fec"`
	Hostname      string `json:"hostname"`
	Identifier    string `json:"identifier"`
	IfName        string `json:"ifname"`
	Length        string `json:"length"`
	MessageType   string `json:"message_type"`
	OpID          int64  `json:"opid"`
	PartNumber    string `json:"part_number"`
	SerialNumber  string `json:"serial_number"`
	Speed         string `json:"speed"`
	State         string `json:"state"`
	SupportedFEC  string `json:"supported_fec"`
	Timestamp     int64  `json:"timestamp"`
	Transreceiver string `json:"transreceiver"`
	VendorName    string `json:"vendor_name"`
}

// BGPPeer models BGP session telemetry returned by NetQ.
type BGPPeer struct {
	ASN                         int64         `json:"asn"`
	ConfiguredHoldTime          FlexibleFloat `json:"configured_hold_time"`
	ConfiguredKeepAliveInterval FlexibleFloat `json:"configured_keep_alive_interval"`
	ConnDropped                 FlexibleFloat `json:"conn_dropped"`
	ConnEstd                    FlexibleFloat `json:"conn_estd"`
	DBState                     string        `json:"db_state"`
	EVPNPrefixesReceived        FlexibleFloat `json:"evpn_pfx_rcvd"`
	HoldTime                    FlexibleFloat `json:"hold_time"`
	Hostname                    string        `json:"hostname"`
	IPv4PrefixesReceived        FlexibleFloat `json:"ipv4_pfx_rcvd"`
	IPv6PrefixesReceived        FlexibleFloat `json:"ipv6_pfx_rcvd"`
	KeepAliveInterval           FlexibleFloat `json:"keep_alive_interval"`
	LocalRouterID               string        `json:"local_router_id"`
	OpID                        int64         `json:"opid"`
	PeerASN                     int64         `json:"peer_asn"`
	PeerHostname                string        `json:"peer_hostname"`
	PeerName                    string        `json:"peer_name"`
	PeerRouterID                string        `json:"peer_router_id"`
	Reason                      string        `json:"reason"`
	State                       string        `json:"state"`
	Timestamp                   int64         `json:"timestamp"`
	UpTime                      FlexibleFloat `json:"up_time"`
	UpdatesReceived             FlexibleFloat `json:"upd8_rx"`
	UpdatesSent                 FlexibleFloat `json:"upd8_tx"`
	VRF                         string        `json:"vrf"`
}

// FlexibleFloat accepts numeric NetQ fields that may be encoded as JSON
// numbers, strings, null, or the string "n/a".
type FlexibleFloat float64

func (f *FlexibleFloat) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}

	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*f = FlexibleFloat(number)
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("decode flexible float: %w", err)
	}
	if text == "" || strings.EqualFold(text, "n/a") {
		*f = 0
		return nil
	}

	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("parse flexible float %q: %w", text, err)
	}
	*f = FlexibleFloat(number)
	return nil
}

// NewClient constructs a NetQ API client with the configured TLS and timeout
// settings.
func NewClient(cfg Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}
	return &Client{
		baseURL:  strings.TrimRight(cfg.Host, "/"),
		username: cfg.Username,
		password: cfg.Password,
		httpClient: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}
}

// Login authenticates to NetQ and returns the bearer token used for object
// API requests.
func (c *Client) Login(ctx context.Context) (string, error) {
	payload := map[string]string{
		"username": c.username,
		"password": c.password,
	}
	var resp LoginResponse
	if err := c.doJSON(ctx, http.MethodPost, loginEndpoint, payload, nil, &resp); err != nil {
		return "", err
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}
	return resp.AccessToken, nil
}

// VMToken retrieves the VM-scoped access token exposed by newer NetQ builds.
func (c *Client) VMToken(ctx context.Context, accessToken string) (VMTokenResponse, error) {
	var resp VMTokenResponse
	if err := c.doJSON(ctx, http.MethodGet, vmTokenEndpoint, nil, bearer(accessToken), &resp); err != nil {
		return VMTokenResponse{}, err
	}
	if resp.AccessToken == "" {
		return VMTokenResponse{}, fmt.Errorf("empty vm access token")
	}
	return resp, nil
}

// Nodes fetches node telemetry from NetQ.
func (c *Client) Nodes(ctx context.Context, accessToken string) ([]Node, error) {
	return fetchTelemetryObjects[Node](ctx, c, accessToken, "node")
}

// Resources fetches resource telemetry from NetQ.
func (c *Client) Resources(ctx context.Context, accessToken string) ([]Resource, error) {
	return fetchTelemetryObjects[Resource](ctx, c, accessToken, "resource")
}

// Sensors fetches sensor telemetry from NetQ.
func (c *Client) Sensors(ctx context.Context, accessToken string) ([]Sensor, error) {
	return fetchTelemetryObjects[Sensor](ctx, c, accessToken, "sensor")
}

// Interfaces fetches interface telemetry from NetQ.
func (c *Client) Interfaces(ctx context.Context, accessToken string) ([]Interface, error) {
	return fetchTelemetryObjects[Interface](ctx, c, accessToken, "interface")
}

// Ports fetches port telemetry from NetQ.
func (c *Client) Ports(ctx context.Context, accessToken string) ([]Port, error) {
	return fetchTelemetryObjects[Port](ctx, c, accessToken, "port")
}

// BGPPeers fetches BGP session telemetry from NetQ.
func (c *Client) BGPPeers(ctx context.Context, accessToken string) ([]BGPPeer, error) {
	return fetchTelemetryObjects[BGPPeer](ctx, c, accessToken, "bgp")
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func fetchTelemetryObjects[T any](ctx context.Context, c *Client, accessToken, object string) ([]T, error) {
	var items []T
	if err := c.doJSON(ctx, http.MethodGet, telemetryObjectEndpoint+object, nil, bearer(accessToken), &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, headers map[string]string, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base url: %w", err)
	}
	rel, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse relative path: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + rel.Path
	u.RawQuery = rel.RawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
