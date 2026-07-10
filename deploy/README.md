# Observability Stack

Start the local stack:

```sh
docker compose up -d
```

Endpoints:

- Kibana: http://localhost:5601
- Grafana: http://localhost:3000
- Prometheus: http://localhost:9090
- Alertmanager: http://localhost:9093
- Elasticsearch: http://localhost:9200
- App metrics: http://localhost:8080/metrics

Grafana uses the default local login `admin` / `admin` and asks for a password change on first login. The Prometheus datasource is provisioned from `deploy/grafana/provisioning/datasources/prometheus.yml`.

Dashboards live in `deploy/grafana/dashboards` and are loaded by `deploy/grafana/provisioning/dashboards/dashboards.yml`. To update one, edit it in Grafana, use Share > Export > Save to file, then commit the exported JSON.

Error-rate panels use Grafana's panel-level `No value = 0` option. PromQL returns no series when there are no 5xx samples in the window, and the panel option keeps the query readable while rendering the expected 0%.

## Metrics

Beyond the RED HTTP metrics, the app exposes business metrics on `/metrics`: `poller_scan_cycles_total`, `poller_scan_duration_seconds`, `poller_releases_detected_total`, `github_rate_limit_hits_total`, and `notification_requests_total{kind,outcome}`. The `/metrics` and `/health` endpoints are excluded from `http_requests_total` so scrapes and healthchecks don't inflate it.

## Alerts

Prometheus loads rules from `deploy/prometheus/alerts.yml` and forwards firing alerts to Alertmanager (`deploy/alertmanager/alertmanager.yml`, UI on :9093). The default receiver has no destination — add a Slack/email/webhook receiver to route alerts outward.

## Log retention & sampling

Filebeat ships logs to Elasticsearch under an ILM policy (`deploy/filebeat/ilm-policy.json`) that rolls daily and deletes after 7 days, so indices don't grow without bound; debug lines are dropped before shipping. The app additionally samples repetitive log lines at the source (`internal/platform/logger/sampling.go`). Container logs rotate (10 MB × 3) and Prometheus retains TSDB data for 15 days.

Create the Kibana data view:

```sh
make kibana-bootstrap
```

That creates `app-logs-*` with `timestamp` as the time field. If the target Kibana URL is different, run `make kibana-bootstrap KIBANA_URL=http://host:5601`.

Elasticsearch runs with `xpack.security.enabled=false` for local development only. Do not reuse this setting in production.

Filebeat defaults to Docker autodiscover and only ingests the `app` Compose service. On Windows Docker Desktop, if Filebeat cannot read `/var/lib/docker/containers`, switch `deploy/filebeat/filebeat.yml` from the autodiscover block to the commented filestream fallback and mount a shared app log volume at `/var/log/app` for both `app` and `filebeat`.
