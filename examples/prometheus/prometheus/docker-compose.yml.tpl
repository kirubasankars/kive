{{/*
Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
Use of this source code is governed by the GNU AGPL v3
license that can be found in the LICENSE file.
*/}}
{{- /* Hard budget $M = kive job reservation (kive/job/prometheus/memory MB).
     GOMEMLIMIT ≈80% so Go GC runs before the cgroup OOM killer.
     retention.size ≈50% of $M caps mmap RSS from compacted TSDB blocks. */ -}}
{{- $memMB := int (getJob "memory") -}}
{{- $minMemMB := int (getJob "min_memory_mb") -}}
{{- $goMemMB := max 192 (div (mul $memMB 80) 100) -}}
{{- $retentionMB := max 256 (div $memMB 2) -}}
services:
  prometheus:
    image: prom/prometheus:v2.54.1
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "{{ get "kive/bucket" "prometheus_http_port" }}:9090"
    # Non-Swarm Compose ignores deploy.resources; pin limits explicitly.
    mem_limit: {{ $memMB }}m
    mem_reservation: {{ $minMemMB }}m
    environment:
      GOMEMLIMIT: {{ $goMemMB }}MiB
    deploy:
      resources:
        limits:
          memory: {{ $memMB }}m
        reservations:
          memory: {{ $minMemMB }}m
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./rules:/etc/prometheus/rules:ro
      - prometheus_data:/prometheus
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --storage.tsdb.path=/prometheus
      - --storage.tsdb.retention.time=15d
      - --storage.tsdb.retention.size={{ $retentionMB }}MB
      - --storage.tsdb.wal-compression
      - --query.max-samples={{ mul $memMB 5000 }}
      - --query.max-concurrency=4
      - --query.timeout=30s
      - --web.enable-lifecycle

volumes:
  prometheus_data:
