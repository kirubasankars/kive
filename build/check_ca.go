// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"fmt"
	"os"

	"kive/bucket"
	"kive/certs"
)

func checkCAExpiry() error {
	warning, err := certs.CheckCAExpiryFromConf()
	if err != nil {
		return err
	}
	if warning != "" && !bucket.QuietCLIOutput() {
		fmt.Fprintln(os.Stderr, warning)
	}
	return nil
}
