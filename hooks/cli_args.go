// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"fmt"
	"strings"
)

// CLIHookInvocation is the parsed form of `kive hooks` positional arguments.
type CLIHookInvocation struct {
	Hook       string
	Job        string
	ScriptArgs []string
}

// ParseCLIHookArgs parses `kive hooks` args after cobra has stripped flags.
// argsLenAtDash is cobra's ArgsLenAtDash(): the number of positional args before `--`,
// or -1 when `--` was not used. Tokens after that index are script argv (CLI only).
//
// Forms:
//
//	<hook>
//	<hook> <job>
//	<hook> -- <scriptArgs...>
//	<hook> <job> -- <scriptArgs...>
//
// Extra tokens without `--` are rejected so operators use `--` for script argv.
func ParseCLIHookArgs(args []string, argsLenAtDash int) (CLIHookInvocation, error) {
	var out CLIHookInvocation
	if len(args) == 0 {
		return out, fmt.Errorf("hook name is required")
	}
	out.Hook = args[0]

	if argsLenAtDash >= 0 {
		if argsLenAtDash < 1 {
			return out, fmt.Errorf("hook name is required before --")
		}
		if argsLenAtDash > len(args) {
			return out, fmt.Errorf("invalid -- position")
		}
		before := args[1:argsLenAtDash]
		out.ScriptArgs = append([]string(nil), args[argsLenAtDash:]...)
		if len(before) > 1 {
			return out, fmt.Errorf("unexpected arguments %q before --; expected at most one job name", strings.Join(before, " "))
		}
		if len(before) == 1 {
			out.Job = before[0]
		}
		return out, nil
	}

	rest := args[1:]
	if len(rest) > 1 {
		return out, fmt.Errorf("unexpected arguments %q; use -- to pass arguments to the hook script", strings.Join(rest[1:], " "))
	}
	if len(rest) == 1 {
		out.Job = rest[0]
	}
	return out, nil
}
