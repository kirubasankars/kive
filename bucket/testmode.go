// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"flag"
	"os"
)

// integrationTestRSA opts into production-strength RSA keys while running under
// go test (SSH integration tests need ≥2048-bit leaf keys for OpenSSL 3/Python).
var integrationTestRSA bool

// EnableIntegrationTestRSA switches RSAKeyBits to production strength for the
// remainder of the process. Call from integration TestMain before initialize.
func EnableIntegrationTestRSA() {
	integrationTestRSA = true
}

// TestMode reports whether the process was started by go test (flag test.v).
// Prefer this over KIVE_TEST when deciding crypto or other security-sensitive behavior.
func TestMode() bool {
	return flag.Lookup("test.v") != nil
}

// RSAKeyBits returns the RSA key size to use for generated certificates.
// Weak keys are used only under go test — never via KIVE_TEST alone — so a
// mis-set env var cannot weaken production CAs or leaf certs.
func RSAKeyBits() int {
	if TestMode() && !integrationTestRSA {
		return 1024
	}
	return 4096
}

// QuietCLIOutput reports whether CLI table/init chatter should be suppressed in tests.
// The tests package sets KIVE_TEST=1 in TestMain for this purpose.
func QuietCLIOutput() bool {
	return os.Getenv("KIVE_TEST") == "1"
}
