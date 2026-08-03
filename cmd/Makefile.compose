# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

.PHONY: cert-permissions start stop restart status logs

COMPOSE ?= docker compose -f docker-compose.yml
CERT_DIR ?= certs
LOG_TAIL ?= 100

cert-permissions:
	@if [ -d "$(CERT_DIR)" ]; then \
		chmod 0755 "$(CERT_DIR)"; \
		find "$(CERT_DIR)" -type f -name '*.key' -exec chmod 0600 {} +; \
		find "$(CERT_DIR)" -type f ! -name '*.key' -exec chmod 0644 {} +; \
	fi

start: cert-permissions
	$(COMPOSE) up -d --force-recreate --remove-orphans

stop:
	$(COMPOSE) down --remove-orphans

restart: stop start

status:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs --tail=$(LOG_TAIL) --no-color
