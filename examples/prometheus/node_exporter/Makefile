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
		curl -sf "http://127.0.0.1:$(METRICS_PORT)/metrics" | head -5 | grep -Eq '^# (HELP|TYPE)|^node_' && \
		{ echo node-exporter-metrics-ok; exit 0; }; \
		sleep 3; \
	done; exit 1
