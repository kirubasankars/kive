// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package schedule implements Outlook-like recurrence allow windows shared by
// bucket promotion and scheduled job hooks.
package schedule

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	RecurrenceDaily   = "daily"
	RecurrenceWeekly  = "weekly"
	RecurrenceMonthly = "monthly"
	RecurrenceYearly  = "yearly"

	MonthlyByDay     = "day"
	MonthlyByWeekday = "weekday"

	WeekFirst  = "first"
	WeekSecond = "second"
	WeekThird  = "third"
	WeekFourth = "fourth"
	WeekLast   = "last"

	scanDays = 400
)

// Schedule holds allow windows (when an action may run).
type Schedule struct {
	Timezone string   `json:"timezone"`
	Windows  []Window `json:"windows"`
}

// Window is one recurrence + local time range.
type Window struct {
	Recurrence string `json:"recurrence"`
	// Weekly
	Days []string `json:"days,omitempty"`
	// Monthly / yearly
	MonthlyBy string     `json:"monthly_by,omitempty"`
	Week      string     `json:"week,omitempty"`
	Weekday   string     `json:"weekday,omitempty"`
	Monthdays []Monthday `json:"monthdays,omitempty"`
	Months    []int      `json:"months,omitempty"`
	Start     string     `json:"start"`
	End       string     `json:"end"`
}

// Monthday is a calendar day 1–31 or the last day of the month.
type Monthday struct {
	Last bool
	Day  int
}

func (m Monthday) MarshalJSON() ([]byte, error) {
	if m.Last {
		return []byte(`"last"`), nil
	}
	return json.Marshal(m.Day)
}

func (m *Monthday) UnmarshalJSON(b []byte) error {
	if m == nil {
		return fmt.Errorf("nil Monthday")
	}
	*m = Monthday{}
	s := strings.TrimSpace(string(b))
	if s == `"last"` {
		m.Last = true
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		var str string
		if err2 := json.Unmarshal(b, &str); err2 != nil {
			return fmt.Errorf("monthday must be 1-31 or %q", "last")
		}
		str = strings.ToLower(strings.TrimSpace(str))
		if str == "last" {
			m.Last = true
			return nil
		}
		n, err = strconv.Atoi(str)
		if err != nil {
			return fmt.Errorf("monthday must be 1-31 or %q", "last")
		}
	}
	if n < 1 || n > 31 {
		return fmt.Errorf("monthday must be 1-31 or %q", "last")
	}
	m.Day = n
	return nil
}

func weekdayName(d time.Weekday) string {
	switch d {
	case time.Monday:
		return "mon"
	case time.Tuesday:
		return "tue"
	case time.Wednesday:
		return "wed"
	case time.Thursday:
		return "thu"
	case time.Friday:
		return "fri"
	case time.Saturday:
		return "sat"
	case time.Sunday:
		return "sun"
	default:
		return ""
	}
}

func parseWeekdayName(s string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	case "sun":
		return time.Sunday, true
	default:
		return 0, false
	}
}

func parseHHMM(s string) (hour, min int, err error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, fmt.Errorf("time must be HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("time must be HH:MM")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("time must be HH:MM")
	}
	return h, m, nil
}

func minutesOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

func hhmmToMinutes(s string) (int, error) {
	h, m, err := parseHHMM(s)
	if err != nil {
		return 0, err
	}
	return h*60 + m, nil
}

// ApplyDefaultTimezone fills an empty schedule timezone from defaultTZ (UTC when defaultTZ is empty).
// An explicit schedule timezone is left unchanged.
func ApplyDefaultTimezone(sched *Schedule, defaultTZ string) {
	if sched == nil {
		return
	}
	if strings.TrimSpace(sched.Timezone) != "" {
		return
	}
	defaultTZ = strings.TrimSpace(defaultTZ)
	if defaultTZ == "" {
		defaultTZ = "UTC"
	}
	sched.Timezone = defaultTZ
}

// Normalize validates and canonicalizes sched. fieldPath prefixes error messages
// (e.g. "promotion.schedule" or "schedule").
func Normalize(sched *Schedule, fieldPath string) error {
	if sched == nil {
		return nil
	}
	if fieldPath == "" {
		fieldPath = "schedule"
	}
	sched.Timezone = strings.TrimSpace(sched.Timezone)
	if sched.Timezone == "" {
		return fmt.Errorf("%s.timezone is required", fieldPath)
	}
	if _, err := time.LoadLocation(sched.Timezone); err != nil {
		return fmt.Errorf("%s.timezone: invalid IANA timezone %q", fieldPath, sched.Timezone)
	}
	if len(sched.Windows) == 0 {
		return fmt.Errorf("%s.windows must be non-empty", fieldPath)
	}
	for i := range sched.Windows {
		if err := NormalizeWindow(&sched.Windows[i]); err != nil {
			return fmt.Errorf("%s.windows[%d]: %w", fieldPath, i, err)
		}
	}
	return nil
}

// NormalizeWindowsOnly validates windows without requiring non-empty (freeze may omit windows).
func NormalizeWindowsOnly(sched *Schedule, fieldPath string) error {
	if sched == nil {
		return nil
	}
	if fieldPath == "" {
		fieldPath = "schedule"
	}
	sched.Timezone = strings.TrimSpace(sched.Timezone)
	if sched.Timezone == "" {
		return fmt.Errorf("%s.timezone is required", fieldPath)
	}
	if _, err := time.LoadLocation(sched.Timezone); err != nil {
		return fmt.Errorf("%s.timezone: invalid IANA timezone %q", fieldPath, sched.Timezone)
	}
	for i := range sched.Windows {
		if err := NormalizeWindow(&sched.Windows[i]); err != nil {
			return fmt.Errorf("%s.windows[%d]: %w", fieldPath, i, err)
		}
	}
	return nil
}

// NormalizeWindow validates one window.
func NormalizeWindow(w *Window) error {
	if w == nil {
		return fmt.Errorf("window is required")
	}
	w.Recurrence = strings.ToLower(strings.TrimSpace(w.Recurrence))
	if w.Recurrence == "" {
		w.Recurrence = RecurrenceWeekly
	}
	switch w.Recurrence {
	case RecurrenceDaily, RecurrenceWeekly, RecurrenceMonthly, RecurrenceYearly:
	default:
		return fmt.Errorf("recurrence must be daily, weekly, monthly, or yearly")
	}
	if _, err := hhmmToMinutes(w.Start); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if _, err := hhmmToMinutes(w.End); err != nil {
		return fmt.Errorf("end: %w", err)
	}

	normDays := make([]string, 0, len(w.Days))
	seenDay := map[string]bool{}
	for _, d := range w.Days {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if _, ok := parseWeekdayName(d); !ok {
			return fmt.Errorf("days: invalid weekday %q", d)
		}
		if seenDay[d] {
			continue
		}
		seenDay[d] = true
		normDays = append(normDays, d)
	}
	w.Days = normDays

	w.MonthlyBy = strings.ToLower(strings.TrimSpace(w.MonthlyBy))
	w.Week = strings.ToLower(strings.TrimSpace(w.Week))
	w.Weekday = strings.ToLower(strings.TrimSpace(w.Weekday))

	normMonths := make([]int, 0, len(w.Months))
	seenMonth := map[int]bool{}
	for _, m := range w.Months {
		if m < 1 || m > 12 {
			return fmt.Errorf("months must be 1-12")
		}
		if seenMonth[m] {
			continue
		}
		seenMonth[m] = true
		normMonths = append(normMonths, m)
	}
	w.Months = normMonths

	switch w.Recurrence {
	case RecurrenceDaily:
		// optional months filter only
	case RecurrenceWeekly:
		if len(w.Days) == 0 {
			return fmt.Errorf("weekly windows require days")
		}
	case RecurrenceMonthly, RecurrenceYearly:
		if w.MonthlyBy == "" {
			return fmt.Errorf("monthly_by is required for %s windows", w.Recurrence)
		}
		switch w.MonthlyBy {
		case MonthlyByDay:
			if len(w.Monthdays) == 0 {
				return fmt.Errorf("monthdays is required when monthly_by is day")
			}
			for _, md := range w.Monthdays {
				if md.Last {
					continue
				}
				if md.Day < 1 || md.Day > 31 {
					return fmt.Errorf("monthdays must be 1-31 or %q", "last")
				}
			}
		case MonthlyByWeekday:
			switch w.Week {
			case WeekFirst, WeekSecond, WeekThird, WeekFourth, WeekLast:
			default:
				return fmt.Errorf("week must be first, second, third, fourth, or last")
			}
			if _, ok := parseWeekdayName(w.Weekday); !ok {
				return fmt.Errorf("weekday must be mon-sun")
			}
		default:
			return fmt.Errorf("monthly_by must be day or weekday")
		}
		if w.Recurrence == RecurrenceYearly && len(w.Months) == 0 {
			return fmt.Errorf("yearly windows require months")
		}
	}
	return nil
}

func monthAllowed(months []int, month time.Month) bool {
	if len(months) == 0 {
		return true
	}
	m := int(month)
	for _, x := range months {
		if x == m {
			return true
		}
	}
	return false
}

func nthWeekdayOfMonth(year int, month time.Month, week string, weekday time.Weekday, loc *time.Location) (time.Time, bool) {
	if week == WeekLast {
		firstNext := time.Date(year, month+1, 1, 0, 0, 0, 0, loc)
		d := firstNext.AddDate(0, 0, -1)
		for d.Month() == month {
			if d.Weekday() == weekday {
				return d, true
			}
			d = d.AddDate(0, 0, -1)
		}
		return time.Time{}, false
	}
	n := 0
	switch week {
	case WeekFirst:
		n = 1
	case WeekSecond:
		n = 2
	case WeekThird:
		n = 3
	case WeekFourth:
		n = 4
	default:
		return time.Time{}, false
	}
	count := 0
	for day := 1; day <= 31; day++ {
		t := time.Date(year, month, day, 0, 0, 0, 0, loc)
		if t.Month() != month {
			break
		}
		if t.Weekday() == weekday {
			count++
			if count == n {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func dateMatchesWindow(w Window, day time.Time) bool {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	if !monthAllowed(w.Months, day.Month()) {
		return false
	}
	switch w.Recurrence {
	case RecurrenceDaily:
		return true
	case RecurrenceWeekly:
		name := weekdayName(day.Weekday())
		for _, d := range w.Days {
			if d == name {
				return true
			}
		}
		return false
	case RecurrenceMonthly, RecurrenceYearly:
		switch w.MonthlyBy {
		case MonthlyByDay:
			lastDay := time.Date(day.Year(), day.Month()+1, 1, 0, 0, 0, 0, day.Location()).AddDate(0, 0, -1).Day()
			for _, md := range w.Monthdays {
				if md.Last {
					if day.Day() == lastDay {
						return true
					}
					continue
				}
				if md.Day > lastDay {
					continue
				}
				if day.Day() == md.Day {
					return true
				}
			}
			return false
		case MonthlyByWeekday:
			wd, ok := parseWeekdayName(w.Weekday)
			if !ok {
				return false
			}
			occ, ok := nthWeekdayOfMonth(day.Year(), day.Month(), w.Week, wd, day.Location())
			if !ok {
				return false
			}
			return occ.Year() == day.Year() && occ.Month() == day.Month() && occ.Day() == day.Day()
		default:
			return false
		}
	default:
		return false
	}
}

func timeInWindowMinutes(startMin, endMin, nowMin int) bool {
	if endMin <= startMin {
		return nowMin >= startMin || nowMin <= endMin
	}
	return nowMin >= startMin && nowMin <= endMin
}

func windowAllowsInstant(w Window, t time.Time) bool {
	startMin, err1 := hhmmToMinutes(w.Start)
	endMin, err2 := hhmmToMinutes(w.End)
	if err1 != nil || err2 != nil {
		return false
	}
	nowMin := minutesOfDay(t)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	if endMin <= startMin {
		if dateMatchesWindow(w, day) && nowMin >= startMin {
			return true
		}
		prev := day.AddDate(0, 0, -1)
		if dateMatchesWindow(w, prev) && nowMin <= endMin {
			return true
		}
		return false
	}
	return dateMatchesWindow(w, day) && timeInWindowMinutes(startMin, endMin, nowMin)
}

// Allows reports whether now is inside any configured window.
// A nil schedule or empty windows returns true (caller interprets by policy).
func Allows(sched *Schedule, now time.Time) (bool, error) {
	if sched == nil || len(sched.Windows) == 0 {
		return true, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(sched.Timezone))
	if err != nil {
		return false, fmt.Errorf("schedule.timezone: %w", err)
	}
	local := now.In(loc)
	for _, w := range sched.Windows {
		if windowAllowsInstant(w, local) {
			return true, nil
		}
	}
	return false, nil
}

// DueOncePerOpening reports whether a once-per-window action should fire:
// the schedule is open now, and either lastFire is zero or the open period is not
// continuous from lastFire to now (a closed gap means a new opening).
func DueOncePerOpening(sched *Schedule, now, lastFire time.Time) (bool, error) {
	openNow, err := Allows(sched, now)
	if err != nil {
		return false, err
	}
	if !openNow {
		return false, nil
	}
	if lastFire.IsZero() {
		return true, nil
	}
	same, err := continuousOpen(sched, lastFire, now)
	if err != nil {
		return false, err
	}
	return !same, nil
}

// continuousOpen reports whether every minute from from through to is inside a window.
func continuousOpen(sched *Schedule, from, to time.Time) (bool, error) {
	if to.Before(from) {
		return false, nil
	}
	// Cap walk length to avoid pathological clocks; windows longer than this still
	// treat a long continuous open as "same opening" via early exit on first gap.
	const maxSteps = 24 * 60 * 8 // 8 days
	steps := 0
	for t := from.UTC().Truncate(time.Minute); !t.After(to); t = t.Add(time.Minute) {
		ok, err := Allows(sched, t)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		steps++
		if steps > maxSteps {
			return true, nil
		}
	}
	return true, nil
}

func windowOpenOnDay(w Window, day time.Time) (time.Time, bool) {
	if !dateMatchesWindow(w, day) {
		return time.Time{}, false
	}
	h, m, err := parseHHMM(w.Start)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location()), true
}

// NextWindowStart returns the next local instant when any window opens at or after now.
// ok is false when already inside a window or none found in the scan horizon.
func NextWindowStart(sched *Schedule, now time.Time) (time.Time, bool, error) {
	if sched == nil || len(sched.Windows) == 0 {
		return time.Time{}, false, nil
	}
	allowed, err := Allows(sched, now)
	if err != nil {
		return time.Time{}, false, err
	}
	if allowed {
		return time.Time{}, false, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(sched.Timezone))
	if err != nil {
		return time.Time{}, false, err
	}
	local := now.In(loc)
	var best time.Time
	found := false
	day0 := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	for i := 0; i <= scanDays; i++ {
		day := day0.AddDate(0, 0, i)
		for _, w := range sched.Windows {
			open, ok := windowOpenOnDay(w, day)
			if !ok {
				continue
			}
			if open.Before(local) {
				continue
			}
			if !found || open.Before(best) {
				best = open
				found = true
			}
		}
	}
	if !found {
		return time.Time{}, false, nil
	}
	return best.UTC(), true, nil
}
