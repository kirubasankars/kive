// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

// HealthFailedAppliedHash is stored in applied_hash when a post-batch health
// gate fails or is cancelled after that allocation was promoted. Catalog shows
// rollout health_failed; the next deploy treats the allocation as needing restart.
const HealthFailedAppliedHash = "__kive_health_failed__"
