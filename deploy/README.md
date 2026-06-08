# Observability Stack

Start the local stack:

```sh
docker compose up -d
```

Endpoints:

- Kibana: http://localhost:5601
- Grafana: http://localhost:3000
- Prometheus: http://localhost:9090
- Elasticsearch: http://localhost:9200
- App metrics: http://localhost:8080/metrics

Grafana uses the default local login `admin` / `admin` and asks for a password change on first login. The Prometheus datasource is provisioned from `deploy/grafana/provisioning/datasources/prometheus.yml`.

Dashboards live in `deploy/grafana/dashboards` and are loaded by `deploy/grafana/provisioning/dashboards/dashboards.yml`. To update one, edit it in Grafana, use Share > Export > Save to file, then commit the exported JSON.

Error-rate panels use Grafana's panel-level `No value = 0` option. PromQL returns no series when there are no 5xx samples in the window, and the panel option keeps the query readable while rendering the expected 0%.

Create the Kibana data view:

```sh
make kibana-bootstrap
```

That creates `app-logs-*` with `timestamp` as the time field. If the target Kibana URL is different, run `make kibana-bootstrap KIBANA_URL=http://host:5601`.

Elasticsearch runs with `xpack.security.enabled=false` for local development only. Do not reuse this setting in production.

Filebeat defaults to Docker autodiscover and only ingests the `app` Compose service. On Windows Docker Desktop, if Filebeat cannot read `/var/lib/docker/containers`, switch `deploy/filebeat/filebeat.yml` from the autodiscover block to the commented filestream fallback and mount a shared app log volume at `/var/log/app` for both `app` and `filebeat`.
