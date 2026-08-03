// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"fmt"

	"kive/bucket"
)

func runnerCommand(bucketID, action, job string) string {
	return fmt.Sprintf(
		"python3 %s/bin/runner.py %s %s --jobs %s",
		bucket.WorkerBucketPath(bucketID), bucketID, action, job,
	)
}
