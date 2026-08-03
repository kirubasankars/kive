// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package promconfig

import _ "embed"

//go:embed kive_certs_alerts.yaml
var KiveCertAlertsYAML []byte

const KiveAlertsJob = "kive"

const KiveCertAlertsFile = "certs.yaml"
