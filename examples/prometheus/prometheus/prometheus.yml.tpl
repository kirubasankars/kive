# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

global:
  scrape_interval: 15s
  evaluation_interval: 15s

{{ ruleFiles }}

scrape_configs:
  - job_name: prometheus
    sample_limit: 5000
    body_size_limit: 10MB
    static_configs:
      - targets: ['127.0.0.1:9090']
{{ scrapeConfigs }}
