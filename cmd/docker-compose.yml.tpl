# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

# Kive validates placement reservations but does not translate CPU MHz or
# memory reservations into Compose limits. cpu_shares is the one explicit
# Docker runtime control exposed by the job resource helper.
x-resource-helper: &resource-helper
  cpu_shares: {{ getJob "cpu_shares" }}

services:
  app:
    <<: *resource-helper
    image: replace-me
    restart: unless-stopped
