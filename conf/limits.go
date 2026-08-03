// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import "errors"

// Hard limits for attack-resistant conf parsing. Real operator confs are
// orders of magnitude smaller; these caps bound memory, stack, and CPU.
const (
	MaxSourceBytes  = 1 << 20 // 1 MiB
	MaxNestingDepth = 32
	MaxStatements   = 100_000
	MaxArgs         = 4_096
	MaxStringBytes  = 64 << 10 // 64 KiB
	MaxIdentBytes   = 256
)

// ErrLimitExceeded is returned (wrapped) when a parse/read hits a hard limit.
var ErrLimitExceeded = errors.New("conf limit exceeded")
