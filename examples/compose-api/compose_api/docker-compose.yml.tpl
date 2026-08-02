# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

services:
  app:
    image: hashicorp/http-echo:1.0.0
    restart: unless-stopped
    ports:
      - "{{ get "kive/bucket" "compose_api_http_port" }}:5678"
    command:
      - -text=compose-api-ok
      - -listen=:5678
