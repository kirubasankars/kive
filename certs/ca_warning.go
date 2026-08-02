// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package certs

import (
	"fmt"
	"time"

	"kive/bucket"
)

// CheckCAExpiry validates secrets/ca.crt before build. An expired CA returns an error.
// A CA within certs_renewal_buffer of expiry returns a warning string (stderr only).
func CheckCAExpiry(renewalBufferDays int, now time.Time) (warning string, err error) {
	metric, ok := readCAMetric(renewalBufferDays, now)
	if !ok {
		return "", nil
	}

	switch metric.Status {
	case StatusExpired:
		return "", fmt.Errorf(
			"bucket CA certificate (secrets/ca.crt) expired on %s",
			metric.NotAfter.Format(time.RFC3339),
		)
	case StatusExpiring:
		return fmt.Sprintf(
			"warning: bucket CA certificate (secrets/ca.crt) expires in %d days on %s (certs_renewal_buffer(%d) days)",
			metric.DaysLeft,
			metric.NotAfter.Format(time.RFC3339),
			renewalBufferDays,
		), nil
	case StatusInvalid:
		return "warning: bucket CA certificate (secrets/ca.crt) is invalid or unreadable", nil
	default:
		return "", nil
	}
}

// CheckCAExpiryFromConf loads workspace/bucket.conf and runs CheckCAExpiry.
func CheckCAExpiryFromConf() (warning string, err error) {
	settings, err := bucket.LoadBucketSettings()
	if err != nil {
		return "", err
	}
	return CheckCAExpiry(settings.CertsRenewalBuffer, time.Now().UTC())
}
