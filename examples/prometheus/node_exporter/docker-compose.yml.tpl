# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

services:
  node_exporter:
    image: prom/node-exporter:v1.8.2
    restart: unless-stopped
    ports:
      - "{{ get "kive/bucket" "node_exporter_metrics_port" }}:9100"
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/rootfs:ro
    command:
      - --path.procfs=/host/proc
      - --path.sysfs=/host/sys
      - --path.rootfs=/rootfs
      - --web.listen-address=:9100
