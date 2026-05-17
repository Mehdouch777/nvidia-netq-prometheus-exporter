# nvidia-netq-prometheus-exporter

`nvidia-netq-prometheus-exporter` polls NetQ Ethernet telemetry object APIs and exposes Prometheus metrics on `/metrics`.

## Endpoints

- `/metrics` - Prometheus scrape endpoint
- `/healthz` - liveness probe
- `/readyz` - readiness probe after the first successful poll

## Supported collectors

- `node`
- `resource`
- `sensor`
- `interface`
- `port`
- `bgp`

## Poll model

The exporter uses a full-snapshot model:

- every poll fetches all supported NetQ object endpoints
- metrics are updated only after every required resource call succeeds
- on poll failure, the exporter keeps the previous metric set in memory
- exporter health metrics and stdout logs indicate whether data is fresh or stale

This keeps Prometheus from seeing partially refreshed state.

## Configuration

### Required environment variables

- `NETQ_HOST` - Base URL for NetQ, for example `https://X.Y.Z.W`
- `NETQ_USERNAME`
- `NETQ_PASSWORD`

### Optional environment variables

- `LISTEN_ADDRESS` - defaults to `:8080`
- `POLL_INTERVAL` - defaults to `1m`
- `NETQ_TIMEOUT` - defaults to `15s`
- `NETQ_INSECURE_SKIP_VERIFY` - defaults to `true`

## Stdout logging

The exporter writes concise operational logs to stdout, including:

- startup configuration summary
- `/metrics` readiness
- poll start
- per-resource poll duration and object count
- poll success/failure summary
- degraded poll recovery
- non-fatal VM token fetch failures

Representative lines:

```text
starting netq-exporter host=https://172.23.2.5 poll_interval=1m0s timeout=15s listen_address=:8080 insecure_skip_verify=true
exporter ready to receive requests on /metrics via :8080
netq poll started
netq resource poll complete resource=interface duration=50.431686ms count=1152
netq poll complete success=true duration=732.270431ms nodes=11 resources=14 sensors=244 interfaces=1152 ports=788 bgp=200
```

## Local development

### Run tests

```bash
go test ./...
```

### Build the binary

```bash
go build ./cmd/netq-exporter
```

### Build the container image

```bash
docker build -t nvidia-netq-prometheus-exporter:local .
```

### Run the container locally

```bash
docker run --rm -p 8080:8080 \
  -e NETQ_HOST=https://<netq-host> \
  -e NETQ_USERNAME=<username> \
  -e NETQ_PASSWORD=<password> \
  -e NETQ_INSECURE_SKIP_VERIFY=true \
  -e NETQ_TIMEOUT=15s \
  -e POLL_INTERVAL=1m \
  nvidia-netq-prometheus-exporter:local
```

Then verify:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/metrics
```

## Metrics

### Exporter health

- `netq_exporter_scrape_success`
- `netq_exporter_scrape_duration_seconds`
- `netq_exporter_last_success_timestamp_seconds`
- `netq_exporter_api_failures_total{stage}`
- `netq_exporter_poll_duration_seconds{resource}`
- `netq_exporter_poll_failures_total{resource}`
- `netq_exporter_objects_returned{resource}`

### Node and resource telemetry

- `netq_node_active{hostname}`
- `netq_node_agent_fresh{hostname}`
- `netq_node_info{hostname,version,db_state}`
- `netq_device_cpu_utilization_percent{hostname}`
- `netq_device_mem_utilization_percent{hostname}`

### Sensor telemetry

- `netq_sensor_ok{hostname,sensor_name,message_type}`
- `netq_sensor_power_input_watts{hostname,sensor_name,message_type}`
- `netq_sensor_power_output_watts{hostname,sensor_name,message_type}`
- `netq_sensor_voltage_input_volts{hostname,sensor_name,message_type}`
- `netq_sensor_voltage_output_volts{hostname,sensor_name,message_type}`

### Interface telemetry

- `netq_interface_admin_up{hostname,interface,vrf,type}`
- `netq_interface_oper_up{hostname,interface,vrf,type}`
- `netq_interface_last_change_timestamp_seconds{hostname,interface,vrf,type}`
- `netq_interface_speed_mbps{hostname,interface}`

### BGP telemetry

- `netq_bgp_peer_up{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_session_uptime_seconds{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_peer_reason_info{hostname,peer_hostname,peer_name,peer_asn,vrf,reason}`
- `netq_bgp_connections_dropped{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_connections_established{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_updates_received{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_updates_sent{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_prefixes_received{hostname,peer_hostname,peer_name,peer_asn,vrf,family}`

## Metric notes

- `netq_bgp_peer_reason_info` is an informational metric with constant value `1`; the useful data is in the `reason` label.
- `netq_bgp_connections_dropped`, `netq_bgp_connections_established`, `netq_bgp_updates_received`, and `netq_bgp_updates_sent` are exported as snapshot-backed gauges because the exporter rebuilds metrics from full NetQ snapshots on each successful poll.
- `netq_interface_last_change_timestamp_seconds` and `netq_bgp_session_uptime_seconds` reflect the values provided by NetQ. Validate units in your environment before treating them as canonical timestamps or durations in dashboards and alerts.
