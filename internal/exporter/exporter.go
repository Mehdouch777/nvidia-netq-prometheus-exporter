package exporter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"repos.apps.bluvalt.com/BluvaltCloud/netq-exporter/internal/netq"
)

type client interface {
	Login(context.Context) (string, error)
	VMToken(context.Context, string) (netq.VMTokenResponse, error)
	Nodes(context.Context, string) ([]netq.Node, error)
	Resources(context.Context, string) ([]netq.Resource, error)
	Sensors(context.Context, string) ([]netq.Sensor, error)
	Interfaces(context.Context, string) ([]netq.Interface, error)
	Ports(context.Context, string) ([]netq.Port, error)
	BGPPeers(context.Context, string) ([]netq.BGPPeer, error)
}

type snapshot struct {
	nodes      []netq.Node
	resources  []netq.Resource
	sensors    []netq.Sensor
	interfaces []netq.Interface
	ports      []netq.Port
	bgpPeers   []netq.BGPPeer
}

var speedPattern = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*([kmgt])\s*$`)

// Exporter periodically polls NetQ object APIs and projects the latest
// successful snapshot into Prometheus gauges.
type Exporter struct {
	client       client
	pollInterval time.Duration
	now          func() time.Time

	mu                  sync.RWMutex
	lastSuccess         time.Time
	lastError           string
	ready               bool
	nodeActive          *prometheus.GaugeVec
	nodeAgentFresh      *prometheus.GaugeVec
	nodeInfo            *prometheus.GaugeVec
	resourceCPU         *prometheus.GaugeVec
	resourceMemory      *prometheus.GaugeVec
	sensorOK            *prometheus.GaugeVec
	sensorPowerInput    *prometheus.GaugeVec
	sensorPowerOutput   *prometheus.GaugeVec
	sensorVoltageInput  *prometheus.GaugeVec
	sensorVoltageOutput *prometheus.GaugeVec
	interfaceAdminUp    *prometheus.GaugeVec
	interfaceOperUp     *prometheus.GaugeVec
	interfaceLastChange *prometheus.GaugeVec
	interfaceSpeedMbps  *prometheus.GaugeVec
	bgpPeerUp           *prometheus.GaugeVec
	bgpSessionUptime    *prometheus.GaugeVec
	bgpPeerReasonInfo   *prometheus.GaugeVec
	bgpConnDropped      *prometheus.GaugeVec
	bgpConnEstd         *prometheus.GaugeVec
	bgpUpdatesReceived  *prometheus.GaugeVec
	bgpUpdatesSent      *prometheus.GaugeVec
	bgpPrefixesReceived *prometheus.GaugeVec
	scrapeSuccess       prometheus.Gauge
	scrapeDuration      prometheus.Gauge
	lastSuccessTS       prometheus.Gauge
	apiFailures         *prometheus.CounterVec
	pollDuration        *prometheus.GaugeVec
	pollFailures        *prometheus.CounterVec
	objectsReturned     *prometheus.GaugeVec
}

// New constructs a NetQ exporter with the supplied API client and poll
// interval.
func New(c client, pollInterval time.Duration, now func() time.Time) *Exporter {
	if now == nil {
		now = time.Now
	}
	return &Exporter{
		client:       c,
		pollInterval: pollInterval,
		now:          now,
		nodeActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_node_active",
			Help: "Whether the NetQ node is active.",
		}, []string{"hostname"}),
		nodeAgentFresh: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_node_agent_fresh",
			Help: "Whether the NetQ node agent state is Fresh.",
		}, []string{"hostname"}),
		nodeInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_node_info",
			Help: "Static information about the NetQ node.",
		}, []string{"hostname", "version", "db_state"}),
		resourceCPU: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_device_cpu_utilization_percent",
			Help: "CPU utilization percentage reported by NetQ.",
		}, []string{"hostname"}),
		resourceMemory: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_device_mem_utilization_percent",
			Help: "Memory utilization percentage reported by NetQ.",
		}, []string{"hostname"}),
		sensorOK: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_sensor_ok",
			Help: "Whether the NetQ sensor state is ok.",
		}, []string{"hostname", "sensor_name", "message_type"}),
		sensorPowerInput: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_sensor_power_input_watts",
			Help: "Sensor power input in watts reported by NetQ.",
		}, []string{"hostname", "sensor_name", "message_type"}),
		sensorPowerOutput: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_sensor_power_output_watts",
			Help: "Sensor power output in watts reported by NetQ.",
		}, []string{"hostname", "sensor_name", "message_type"}),
		sensorVoltageInput: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_sensor_voltage_input_volts",
			Help: "Sensor input voltage in volts reported by NetQ.",
		}, []string{"hostname", "sensor_name", "message_type"}),
		sensorVoltageOutput: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_sensor_voltage_output_volts",
			Help: "Sensor output voltage in volts reported by NetQ.",
		}, []string{"hostname", "sensor_name", "message_type"}),
		interfaceAdminUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_interface_admin_up",
			Help: "Whether the NetQ interface administrative state is up.",
		}, []string{"hostname", "interface", "vrf", "type"}),
		interfaceOperUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_interface_oper_up",
			Help: "Whether the NetQ interface operational state is up.",
		}, []string{"hostname", "interface", "vrf", "type"}),
		interfaceLastChange: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_interface_last_change_timestamp_seconds",
			Help: "Unix timestamp of the last interface state change reported by NetQ.",
		}, []string{"hostname", "interface", "vrf", "type"}),
		interfaceSpeedMbps: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_interface_speed_mbps",
			Help: "Interface speed in Mbps reported by NetQ port telemetry.",
		}, []string{"hostname", "interface"}),
		bgpPeerUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_peer_up",
			Help: "Whether the NetQ BGP peer is established.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf"}),
		bgpSessionUptime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_session_uptime_seconds",
			Help: "Current BGP session uptime in seconds reported by NetQ.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf"}),
		bgpPeerReasonInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_peer_reason_info",
			Help: "Informational BGP peer metric keyed by the latest reason string reported by NetQ.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf", "reason"}),
		bgpConnDropped: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_connections_dropped",
			Help: "Connection drops reported by NetQ for the BGP peer.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf"}),
		bgpConnEstd: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_connections_established",
			Help: "Connections established reported by NetQ for the BGP peer.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf"}),
		bgpUpdatesReceived: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_updates_received",
			Help: "BGP updates received reported by NetQ for the peer.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf"}),
		bgpUpdatesSent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_updates_sent",
			Help: "BGP updates sent reported by NetQ for the peer.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf"}),
		bgpPrefixesReceived: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_bgp_prefixes_received",
			Help: "Number of prefixes received from a BGP peer by address family.",
		}, []string{"hostname", "peer_hostname", "peer_name", "peer_asn", "vrf", "family"}),
		scrapeSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "netq_exporter_scrape_success",
			Help: "Whether the last NetQ poll succeeded.",
		}),
		scrapeDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "netq_exporter_scrape_duration_seconds",
			Help: "Duration of the last successful or failed NetQ poll.",
		}),
		lastSuccessTS: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "netq_exporter_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful NetQ poll.",
		}),
		apiFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netq_exporter_api_failures_total",
			Help: "Number of failed NetQ API operations by stage.",
		}, []string{"stage"}),
		pollDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_exporter_poll_duration_seconds",
			Help: "Duration of the latest NetQ API poll by resource.",
		}, []string{"resource"}),
		pollFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "netq_exporter_poll_failures_total",
			Help: "Number of failed NetQ API polls by resource.",
		}, []string{"resource"}),
		objectsReturned: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "netq_exporter_objects_returned",
			Help: "Number of objects returned by the latest successful NetQ API poll by resource.",
		}, []string{"resource"}),
	}
}

// Describe forwards descriptor discovery to the collector implementation.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(e, ch)
}

func (e *Exporter) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		e.nodeActive,
		e.nodeAgentFresh,
		e.nodeInfo,
		e.resourceCPU,
		e.resourceMemory,
		e.sensorOK,
		e.sensorPowerInput,
		e.sensorPowerOutput,
		e.sensorVoltageInput,
		e.sensorVoltageOutput,
		e.interfaceAdminUp,
		e.interfaceOperUp,
		e.interfaceLastChange,
		e.interfaceSpeedMbps,
		e.bgpPeerUp,
		e.bgpSessionUptime,
		e.bgpPeerReasonInfo,
		e.bgpConnDropped,
		e.bgpConnEstd,
		e.bgpUpdatesReceived,
		e.bgpUpdatesSent,
		e.bgpPrefixesReceived,
		e.scrapeSuccess,
		e.scrapeDuration,
		e.lastSuccessTS,
		e.apiFailures,
		e.pollDuration,
		e.pollFailures,
		e.objectsReturned,
	}
}

// Collect emits the current in-memory metric set.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	for _, collector := range e.collectors() {
		collector.Collect(ch)
	}
}

// Run performs an immediate poll, then continues polling until the context is
// canceled.
func (e *Exporter) Run(ctx context.Context) error {
	if err := e.poll(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			e.recordFailure("poll", err, 0)
		}
	}

	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = e.poll(ctx)
		}
	}
}

// HealthHandler reports process liveness only.
func (e *Exporter) HealthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// ReadyHandler reports whether the exporter has completed at least one
// successful poll.
func (e *Exporter) ReadyHandler(w http.ResponseWriter, _ *http.Request) {
	e.mu.RLock()
	ready := e.ready
	lastError := e.lastError
	e.mu.RUnlock()
	if !ready {
		http.Error(w, fmt.Sprintf("not ready: %s", lastError), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (e *Exporter) poll(ctx context.Context) error {
	start := e.now()
	e.mu.RLock()
	wasReady := e.ready
	previousError := e.lastError
	previousSuccess := e.lastSuccess
	e.mu.RUnlock()
	log.Printf("netq poll started")

	loginStart := e.now()
	accessToken, err := e.client.Login(ctx)
	if err != nil {
		e.recordResourceFailure("login", e.now().Sub(loginStart), err)
		e.recordFailure("login", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("login", e.now().Sub(loginStart), 1)
	vmStart := e.now()
	if _, err := e.client.VMToken(ctx, accessToken); err != nil {
		e.recordResourceFailure("vm_token", e.now().Sub(vmStart), err)
		// Non-fatal for the v1 exporter. We only need the login token for object APIs.
		log.Printf("netq optional vm token request failed; continuing with login token duration=%s error=%v", e.now().Sub(vmStart), err)
	} else {
		e.recordResourceSuccess("vm_token", e.now().Sub(vmStart), 1)
	}

	nodesStart := e.now()
	nodes, err := e.client.Nodes(ctx, accessToken)
	if err != nil {
		e.recordResourceFailure("node", e.now().Sub(nodesStart), err)
		e.recordFailure("nodes", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("node", e.now().Sub(nodesStart), len(nodes))
	resourcesStart := e.now()
	resources, err := e.client.Resources(ctx, accessToken)
	if err != nil {
		e.recordResourceFailure("resource", e.now().Sub(resourcesStart), err)
		e.recordFailure("resources", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("resource", e.now().Sub(resourcesStart), len(resources))
	sensorsStart := e.now()
	sensors, err := e.client.Sensors(ctx, accessToken)
	if err != nil {
		e.recordResourceFailure("sensor", e.now().Sub(sensorsStart), err)
		e.recordFailure("sensors", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("sensor", e.now().Sub(sensorsStart), len(sensors))
	interfacesStart := e.now()
	interfaces, err := e.client.Interfaces(ctx, accessToken)
	if err != nil {
		e.recordResourceFailure("interface", e.now().Sub(interfacesStart), err)
		e.recordFailure("interfaces", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("interface", e.now().Sub(interfacesStart), len(interfaces))
	portsStart := e.now()
	ports, err := e.client.Ports(ctx, accessToken)
	if err != nil {
		e.recordResourceFailure("port", e.now().Sub(portsStart), err)
		e.recordFailure("ports", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("port", e.now().Sub(portsStart), len(ports))
	bgpStart := e.now()
	bgpPeers, err := e.client.BGPPeers(ctx, accessToken)
	if err != nil {
		e.recordResourceFailure("bgp", e.now().Sub(bgpStart), err)
		e.recordFailure("bgp", err, e.now().Sub(start))
		return err
	}
	e.recordResourceSuccess("bgp", e.now().Sub(bgpStart), len(bgpPeers))

	// Apply the snapshot only after every required resource call succeeds, so
	// Prometheus never observes a partially refreshed view.
	e.apply(snapshot{
		nodes:      nodes,
		resources:  resources,
		sensors:    sensors,
		interfaces: interfaces,
		ports:      ports,
		bgpPeers:   bgpPeers,
	})
	end := e.now()
	e.mu.Lock()
	e.ready = true
	e.lastError = ""
	e.lastSuccess = end
	e.mu.Unlock()
	e.scrapeSuccess.Set(1)
	e.scrapeDuration.Set(end.Sub(start).Seconds())
	e.lastSuccessTS.Set(float64(end.Unix()))
	log.Printf(
		"netq poll complete success=true duration=%s nodes=%d resources=%d sensors=%d interfaces=%d ports=%d bgp=%d",
		end.Sub(start),
		len(nodes),
		len(resources),
		len(sensors),
		len(interfaces),
		len(ports),
		len(bgpPeers),
	)
	switch {
	case !wasReady:
		log.Printf(
			"netq exporter ready after initial successful poll duration=%s nodes=%d resources=%d sensors=%d interfaces=%d ports=%d bgp=%d",
			end.Sub(start),
			len(nodes),
			len(resources),
			len(sensors),
			len(interfaces),
			len(ports),
			len(bgpPeers),
		)
	case previousError != "":
		degradedFor := time.Duration(0)
		if !previousSuccess.IsZero() {
			degradedFor = end.Sub(previousSuccess)
		}
		log.Printf(
			"netq poll recovered previous_error=%q degraded_for=%s nodes=%d resources=%d sensors=%d interfaces=%d ports=%d bgp=%d duration=%s",
			previousError,
			degradedFor,
			len(nodes),
			len(resources),
			len(sensors),
			len(interfaces),
			len(ports),
			len(bgpPeers),
			end.Sub(start),
		)
	}
	return nil
}

func (e *Exporter) recordFailure(stage string, err error, duration time.Duration) {
	e.mu.Lock()
	e.lastError = fmt.Sprintf("%s: %v", stage, err)
	e.mu.Unlock()
	e.scrapeSuccess.Set(0)
	if duration > 0 {
		e.scrapeDuration.Set(duration.Seconds())
	}
	log.Printf(
		"netq poll complete success=false failed_stage=%s duration=%s error=%v keeping_previous_metrics=true",
		stage,
		duration,
		err,
	)
}

func (e *Exporter) recordResourceFailure(resource string, duration time.Duration, err error) {
	e.apiFailures.WithLabelValues(resource).Inc()
	e.pollFailures.WithLabelValues(resource).Inc()
	e.pollDuration.WithLabelValues(resource).Set(duration.Seconds())
	log.Printf("netq resource poll failed resource=%s duration=%s error=%v", resource, duration, err)
}

func (e *Exporter) recordResourceSuccess(resource string, duration time.Duration, count int) {
	e.pollDuration.WithLabelValues(resource).Set(duration.Seconds())
	e.objectsReturned.WithLabelValues(resource).Set(float64(count))
	log.Printf("netq resource poll complete resource=%s duration=%s count=%d", resource, duration, count)
}

func (e *Exporter) apply(s snapshot) {
	e.resetSnapshotMetrics()
	e.applyNodes(s.nodes)
	e.applyResources(s.resources)
	e.applySensors(s.sensors)
	e.applyInterfaces(s.interfaces)
	e.applyPorts(s.ports)
	e.applyBGPPeers(s.bgpPeers)
}

func (e *Exporter) resetSnapshotMetrics() {
	// Reset all series before repopulating them from the latest snapshot so
	// metrics for deleted objects disappear naturally on the next successful poll.
	e.nodeActive.Reset()
	e.nodeAgentFresh.Reset()
	e.nodeInfo.Reset()
	e.resourceCPU.Reset()
	e.resourceMemory.Reset()
	e.sensorOK.Reset()
	e.sensorPowerInput.Reset()
	e.sensorPowerOutput.Reset()
	e.sensorVoltageInput.Reset()
	e.sensorVoltageOutput.Reset()
	e.interfaceAdminUp.Reset()
	e.interfaceOperUp.Reset()
	e.interfaceLastChange.Reset()
	e.interfaceSpeedMbps.Reset()
	e.bgpPeerUp.Reset()
	e.bgpSessionUptime.Reset()
	e.bgpPeerReasonInfo.Reset()
	e.bgpConnDropped.Reset()
	e.bgpConnEstd.Reset()
	e.bgpUpdatesReceived.Reset()
	e.bgpUpdatesSent.Reset()
	e.bgpPrefixesReceived.Reset()
}

func (e *Exporter) applyNodes(nodes []netq.Node) {
	for _, node := range nodes {
		e.nodeActive.WithLabelValues(node.Hostname).Set(boolFloat(node.Active))
		e.nodeAgentFresh.WithLabelValues(node.Hostname).Set(boolFloat(strings.EqualFold(node.AgentState, "fresh")))
		e.nodeInfo.WithLabelValues(node.Hostname, node.Version, node.DBState).Set(1)
	}
}

func (e *Exporter) applyResources(resources []netq.Resource) {
	for _, resource := range resources {
		e.resourceCPU.WithLabelValues(resource.Hostname).Set(resource.CPUUtilization)
		e.resourceMemory.WithLabelValues(resource.Hostname).Set(resource.MemUtilization)
	}
}

func (e *Exporter) applySensors(sensors []netq.Sensor) {
	for _, sensor := range sensors {
		labels := []string{sensor.Hostname, sensor.Name, sensor.MessageType}
		e.sensorOK.WithLabelValues(labels...).Set(boolFloat(strings.EqualFold(sensor.State, "ok")))
		e.sensorPowerInput.WithLabelValues(labels...).Set(float64(sensor.PowerInput))
		e.sensorPowerOutput.WithLabelValues(labels...).Set(float64(sensor.PowerOutput))
		e.sensorVoltageInput.WithLabelValues(labels...).Set(float64(sensor.VoltageInput))
		e.sensorVoltageOutput.WithLabelValues(labels...).Set(float64(sensor.VoltageOutput))
	}
}

func (e *Exporter) applyInterfaces(interfaces []netq.Interface) {
	for _, iface := range interfaces {
		labels := []string{iface.Hostname, iface.IfName, iface.VRF, iface.Type}
		e.interfaceAdminUp.WithLabelValues(labels...).Set(stateUp(iface.AdminState))
		e.interfaceOperUp.WithLabelValues(labels...).Set(stateUp(iface.State))
		e.interfaceLastChange.WithLabelValues(labels...).Set(float64(iface.LastChanged))
	}
}

func (e *Exporter) applyPorts(ports []netq.Port) {
	for _, port := range ports {
		if speedMbps, ok := parseSpeedMbps(port.Speed); ok {
			e.interfaceSpeedMbps.WithLabelValues(port.Hostname, port.IfName).Set(speedMbps)
		}
	}
}

func (e *Exporter) applyBGPPeers(peers []netq.BGPPeer) {
	for _, peer := range peers {
		labels := bgpLabels(peer)
		e.bgpPeerUp.WithLabelValues(labels...).Set(stateUp(peer.State))
		e.bgpSessionUptime.WithLabelValues(labels...).Set(float64(peer.UpTime))
		e.bgpPeerReasonInfo.WithLabelValues(append(labels, reasonLabel(peer.Reason))...).Set(1)
		e.bgpConnDropped.WithLabelValues(labels...).Set(float64(peer.ConnDropped))
		e.bgpConnEstd.WithLabelValues(labels...).Set(float64(peer.ConnEstd))
		e.bgpUpdatesReceived.WithLabelValues(labels...).Set(float64(peer.UpdatesReceived))
		e.bgpUpdatesSent.WithLabelValues(labels...).Set(float64(peer.UpdatesSent))
		e.bgpPrefixesReceived.WithLabelValues(append(labels, "ipv4")...).Set(float64(peer.IPv4PrefixesReceived))
		e.bgpPrefixesReceived.WithLabelValues(append(labels, "ipv6")...).Set(float64(peer.IPv6PrefixesReceived))
		e.bgpPrefixesReceived.WithLabelValues(append(labels, "evpn")...).Set(float64(peer.EVPNPrefixesReceived))
	}
}

func bgpLabels(peer netq.BGPPeer) []string {
	return []string{
		peer.Hostname,
		peer.PeerHostname,
		peer.PeerName,
		strconv.FormatInt(peer.PeerASN, 10),
		peer.VRF,
	}
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func stateUp(state string) float64 {
	if strings.EqualFold(strings.TrimSpace(state), "up") || strings.EqualFold(strings.TrimSpace(state), "established") {
		return 1
	}
	return 0
}

func reasonLabel(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "none"
	}
	return reason
}

func parseSpeedMbps(speed string) (float64, bool) {
	matches := speedPattern.FindStringSubmatch(strings.TrimSpace(speed))
	if len(matches) != 3 {
		return 0, false
	}
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToUpper(matches[2]) {
	case "K":
		return value / 1000, true
	case "M":
		return value, true
	case "G":
		return value * 1000, true
	case "T":
		return value * 1000 * 1000, true
	default:
		return 0, false
	}
}
