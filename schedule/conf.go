// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package schedule

import (
	"fmt"
	"strconv"

	"kive/conf"
)

// ParseBlock lowers a schedule { timezone; window { … }; } conf block.
func ParseBlock(filePath string, b *conf.Block) (Schedule, error) {
	var sched Schedule
	known := []string{"timezone", "window"}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Call:
			switch s.Name {
			case "timezone":
				str, err := conf.SingleStringArg(s, filePath)
				if err != nil {
					return Schedule{}, err
				}
				sched.Timezone = str
			default:
				return Schedule{}, conf.UnknownSetting(filePath, s.NamePos, s.Name, known)
			}
		case *conf.Block:
			if s.Name != "window" {
				return Schedule{}, conf.Err(filePath, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected block %q in schedule", s.Name)).
					WithHint(`use window { ... };`)
			}
			w, err := parseWindowBlock(filePath, s)
			if err != nil {
				return Schedule{}, err
			}
			sched.Windows = append(sched.Windows, w)
		default:
			return Schedule{}, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				"schedule body only allows timezone(...) and window { ... }")
		}
	}
	return sched, nil
}

func parseWindowBlock(filePath string, b *conf.Block) (Window, error) {
	var w Window
	known := []string{"recurrence", "days", "monthly_by", "week", "weekday", "monthdays", "months", "start", "end"}
	for _, stmt := range b.Body {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return Window{}, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				"window body only allows calls")
		}
		switch c.Name {
		case "recurrence":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.Recurrence = s
		case "days":
			days, err := conf.AsStrings(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.Days = days
		case "monthly_by":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.MonthlyBy = s
		case "week":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.Week = s
		case "weekday":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.Weekday = s
		case "monthdays":
			for _, a := range c.Args {
				switch v := a.Value.(type) {
				case *conf.NumberLit:
					n := int(v.Int)
					w.Monthdays = append(w.Monthdays, Monthday{Day: n})
				case *conf.StringLit:
					if v.Value == "last" {
						w.Monthdays = append(w.Monthdays, Monthday{Last: true})
					} else {
						n, err := strconv.Atoi(v.Value)
						if err != nil {
							return Window{}, conf.Err(filePath, v.Pos().Line, v.Pos().Column,
								`monthdays entries must be 1-31 or "last"`)
						}
						w.Monthdays = append(w.Monthdays, Monthday{Day: n})
					}
				case *conf.IdentLit:
					if v.Name == "last" {
						w.Monthdays = append(w.Monthdays, Monthday{Last: true})
					} else {
						return Window{}, conf.Err(filePath, v.Pos().Line, v.Pos().Column,
							`monthdays entries must be 1-31 or "last"`)
					}
				default:
					return Window{}, conf.Err(filePath, a.Value.Pos().Line, a.Value.Pos().Column,
						`monthdays entries must be 1-31 or "last"`)
				}
			}
		case "months":
			for _, a := range c.Args {
				n, err := conf.AsInt(a.Value, filePath)
				if err != nil {
					return Window{}, err
				}
				w.Months = append(w.Months, n)
			}
		case "start":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.Start = s
		case "end":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return Window{}, err
			}
			w.End = s
		default:
			return Window{}, conf.UnknownSetting(filePath, c.NamePos, c.Name, known)
		}
	}
	return w, nil
}

// EmitBlock serializes a schedule to a conf schedule { … } block.
func EmitBlock(sched *Schedule) *conf.Block {
	sb := &conf.Block{Name: "schedule"}
	if sched == nil {
		return sb
	}
	if sched.Timezone != "" {
		sb.Body = append(sb.Body, conf.CallStmt("timezone", sched.Timezone))
	}
	for _, w := range sched.Windows {
		wb := &conf.Block{Name: "window"}
		if w.Recurrence != "" {
			wb.Body = append(wb.Body, conf.CallStmt("recurrence", w.Recurrence))
		}
		if len(w.Days) > 0 {
			wb.Body = append(wb.Body, conf.CallStmt("days", w.Days...))
		}
		if w.MonthlyBy != "" {
			wb.Body = append(wb.Body, conf.CallStmt("monthly_by", w.MonthlyBy))
		}
		if w.Week != "" {
			wb.Body = append(wb.Body, conf.CallStmt("week", w.Week))
		}
		if w.Weekday != "" {
			wb.Body = append(wb.Body, conf.CallStmt("weekday", w.Weekday))
		}
		if len(w.Monthdays) > 0 {
			call := &conf.Call{Name: "monthdays", TrailingSem: true}
			for _, md := range w.Monthdays {
				if md.Last {
					call.Args = append(call.Args, conf.Arg{Value: conf.Str("last")})
				} else {
					call.Args = append(call.Args, conf.Arg{Value: conf.Int(md.Day)})
				}
			}
			wb.Body = append(wb.Body, call)
		}
		if len(w.Months) > 0 {
			call := &conf.Call{Name: "months", TrailingSem: true}
			for _, m := range w.Months {
				call.Args = append(call.Args, conf.Arg{Value: conf.Int(m)})
			}
			wb.Body = append(wb.Body, call)
		}
		if w.Start != "" {
			wb.Body = append(wb.Body, conf.CallStmt("start", w.Start))
		}
		if w.End != "" {
			wb.Body = append(wb.Body, conf.CallStmt("end", w.End))
		}
		sb.Body = append(sb.Body, wb)
	}
	return sb
}
