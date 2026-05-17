# netq-exporter

`netq-exporter` polls NetQ Ethernet telemetry object APIs and exposes Prometheus metrics on `/metrics`.

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
docker build -t netq-exporter:local .
```

Local validation result: `docker build -t netq-exporter:local .` completed successfully on May 16, 2026.

### Run the container locally

```bash
docker run --rm -p 8080:8080 \
  -e NETQ_HOST=https://<netq-host> \
  -e NETQ_USERNAME=<username> \
  -e NETQ_PASSWORD=<password> \
  -e NETQ_INSECURE_SKIP_VERIFY=true \
  -e NETQ_TIMEOUT=15s \
  -e POLL_INTERVAL=1m \
  netq-exporter:local
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

### Interface and BGP telemetry

- `netq_interface_admin_up{hostname,interface,vrf,type}`
- `netq_interface_oper_up{hostname,interface,vrf,type}`
- `netq_interface_last_change_timestamp_seconds{hostname,interface,vrf,type}`
- `netq_interface_speed_mbps{hostname,interface}`
- `netq_bgp_peer_up{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_session_uptime_seconds{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_peer_reason_info{hostname,peer_hostname,peer_name,peer_asn,vrf,reason}`
- `netq_bgp_connections_dropped{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_connections_established{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_updates_received{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_updates_sent{hostname,peer_hostname,peer_name,peer_asn,vrf}`
- `netq_bgp_prefixes_received{hostname,peer_hostname,peer_name,peer_asn,vrf,family}`
