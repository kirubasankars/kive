# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

.PHONY: start stop restart status logs test

COMPOSE := docker compose -f docker-compose.yml
LOG_TAIL ?= 100

include config.env
export

start:
	$(COMPOSE) up -d --remove-orphans

stop:
	$(COMPOSE) down --remove-orphans

restart: stop start

status:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs --tail=$(LOG_TAIL) --no-color

test:
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		curl -sf "http://127.0.0.1:$(PROMETHEUS_PORT)/api/v1/targets" | python3 -c 'import json,sys; data=json.load(sys.stdin); targets=[t for t in data.get("data",{}).get("activeTargets",[]) if t.get("labels",{}).get("job")=="node_exporter"]; assert targets, "no node_exporter targets"; bad=[t for t in targets if t.get("health")!="up"]; assert not bad, bad; print("prometheus-node-exporter-connected")' && exit 0; \
		sleep 3; \
	done; exit 1
