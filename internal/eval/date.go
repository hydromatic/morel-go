// Licensed to Julian Hyde under one or more contributor license
// agreements.  See the NOTICE file distributed with this work
// for additional information regarding copyright ownership.
// Julian Hyde licenses this file to you under the Apache
// License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License.  You may obtain a
// copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied.  See the License for the specific
// language governing permissions and limitations under the
// License.

package eval

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The Date structure. A "date" value is a calendar date and time
// of day with a fixed zone offset, held as a Go time.Time.

const (
	monthDatatype   = "month"
	weekdayDatatype = "weekday"
	ctimeLayout     = "Mon Jan _2 15:04:05 2006"
	daysPerWeek     = 7
	hoursPerHalfDay = 12
	yearsPerCentury = 100
	secPerHour      = 3600
	secPerMinute    = 60
)

var monthNames = []string{
	"Jan", "Feb", "Mar", "Apr", "May", "Jun",
	"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
}

// asDate views a runtime value as a date.
func asDate(v Val) time.Time {
	t, ok := v.(time.Time)
	if !ok {
		panic(fmt.Sprintf("expected date, got %T", v))
	}
	return t
}

// FormatDate renders a date as ISO-8601 with an offset, omitting
// the seconds field when the second and sub-second parts are both
// zero: "1970-01-01T00:00Z",
// "1995-03-08T19:06:45Z".
func FormatDate(v Val) string {
	t := asDate(v)
	s := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d",
		t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute())
	if n := t.Nanosecond(); t.Second() != 0 || n != 0 {
		s += fmt.Sprintf(":%02d", t.Second())
		switch {
		case n == 0:
		case n%nsPerMilli == 0:
			s += fmt.Sprintf(".%03d", n/nsPerMilli)
		case n%nsPerMicro == 0:
			s += fmt.Sprintf(".%06d", n/nsPerMicro)
		default:
			s += fmt.Sprintf(".%09d", n)
		}
	}
	return s + formatOffset(t)
}

// formatOffset renders the zone offset: "Z" for UTC, else "+HH:MM"
// or "-HH:MM".
func formatOffset(t time.Time) string {
	_, off := t.Zone()
	if off == 0 {
		return "Z"
	}
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	return fmt.Sprintf("%s%02d:%02d", sign, off/secPerHour,
		off%secPerHour/secPerMinute)
}

// monthVal builds a month datatype value from a Go month (1..12).
func monthVal(m time.Month) Val {
	i := int(m) - 1
	return Con{Datatype: monthDatatype, Name: monthNames[i], Ordinal: i}
}

// monthFromName is the 1..12 number of a month name, or 0.
func monthFromName(name string) int {
	for i, n := range monthNames {
		if n == name {
			return i + 1
		}
	}
	return 0
}

// weekdayVal builds a weekday datatype value from a Go weekday. The
// weekday datatype runs Mon..Sun (Mon = 0), so Sunday is last.
func weekdayVal(w time.Weekday) Val {
	names := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	// Go's Sunday = 0; shift so Monday = 0.
	i := (int(w) + daysPerWeek - 1) % daysPerWeek
	return Con{Datatype: weekdayDatatype, Name: names[i], Ordinal: i}
}

// DateConstruct validates the calendar fields and builds a date in
// the given location, raising Date on any out-of-range field (Go's
// time.Date silently normalizes, so a round-trip check catches it).
func DateConstruct(
	year, month, day, hour, minute, second int, loc *time.Location,
) (Val, error) {
	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day ||
		t.Hour() != hour || t.Minute() != minute || t.Second() != second {
		return nil, &MorelError{Exn: ExnDate}
	}
	return t, nil
}

// DateConstructRecord is "Date.date {…}". The record arrives in
// alphabetical field order: day, hour, minute, month, offset,
// second, year. A SOME offset gives a fixed zone; NONE uses loc.
func DateConstructRecord(rec []Val, loc *time.Location) (Val, error) {
	monthCon, _ := rec[3].(Con)
	offVal, isSome := asOption(rec[4])
	if isSome {
		loc = time.FixedZone("", int(asTime(offVal)/nsPerSecond))
	}
	return DateConstruct(int(asInt(rec[6])), monthCon.Ordinal+1,
		int(asInt(rec[0])), int(asInt(rec[1])), int(asInt(rec[2])),
		int(asInt(rec[5])), loc)
}

// DateFromTimeLocal is "Date.fromTimeLocal t": the instant in loc.
func DateFromTimeLocal(arg Val, loc *time.Location) Val {
	return time.Unix(0, asTime(arg)).In(loc)
}

// DateLocalOffset is "Date.localOffset ()": the zone's offset from
// UTC at now, as a time (nanoseconds).
func DateLocalOffset(loc *time.Location, now time.Time) Val {
	_, off := now.In(loc).Zone()
	return int64(off) * nsPerSecond
}

// dateFromTimeUnivFn is "Date.fromTimeUniv t": the date at UTC.
func dateFromTimeUnivFn(arg Val) (Val, error) {
	return time.Unix(0, asTime(arg)).UTC(), nil
}

// dateToTimeFn is "Date.toTime d": the instant as nanoseconds.
func dateToTimeFn(arg Val) (Val, error) {
	return asDate(arg).UnixNano(), nil
}

// dateCompareFn is "Date.compare (d1, d2)": by instant.
func dateCompareFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return orderVal(cmpTime(asDate(a).UnixNano(), asDate(b).UnixNano())), nil
}

// dateField builds a "date -> int" extractor.
func dateField(f func(t time.Time) int) Fn {
	return func(arg Val) (Val, error) {
		//nolint:gosec // calendar fields are within int range
		return int32(f(asDate(arg))), nil
	}
}

// dateYearDay is the zero-based day of the year (Go's is 1-based).
func dateYearDay(t time.Time) int {
	return t.YearDay() - 1
}

func dateMonthFn(arg Val) (Val, error) {
	return monthVal(asDate(arg).Month()), nil
}

func dateWeekDayFn(arg Val) (Val, error) {
	return weekdayVal(asDate(arg).Weekday()), nil
}

// dateIsDstFn is "Date.isDst d": always NONE, since a stored offset
// carries no daylight-saving information.
func dateIsDstFn(Val) (Val, error) {
	return noneVal, nil
}

// dateToStringFn is "Date.toString d": the ctime form, e.g.
// "Wed Dec 31 00:00:00 1969".
func dateToStringFn(arg Val) (Val, error) {
	return asDate(arg).Format(ctimeLayout), nil
}

// dateFromStringFn is "Date.fromString s": parses the ctime form
// (ignoring the weekday token), returning SOME date at UTC or NONE.
func dateFromStringFn(arg Val) (Val, error) {
	fields := strings.Fields(asString(arg))
	const ctimeTokens = 5
	if len(fields) != ctimeTokens {
		return noneVal, nil
	}
	month := monthFromName(fields[1])
	timeParts := strings.Split(fields[3], ":")
	const clockParts = 3
	if month == 0 || len(timeParts) != clockParts {
		return noneVal, nil
	}
	day, e1 := strconv.Atoi(fields[2])
	hour, e2 := strconv.Atoi(timeParts[0])
	minute, e3 := strconv.Atoi(timeParts[1])
	second, e4 := strconv.Atoi(timeParts[2])
	year, e5 := strconv.Atoi(fields[4])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		return noneVal, nil //nolint:nilerr // unparsable fields mean NONE
	}
	d, err := DateConstruct(year, month, day, hour, minute, second, time.UTC)
	if err != nil {
		return noneVal, nil //nolint:nilerr // out-of-range parses to NONE
	}
	return someVal(d), nil
}

// dateFmtFn is "Date.fmt s d": strftime-style formatting.
func dateFmtFn(s Val) (Val, error) {
	layout := asString(s)
	return Fn(func(d Val) (Val, error) {
		return dateFmt(layout, asDate(d)), nil
	}), nil
}

// dateFmt renders a date with a strftime-style format string.
func dateFmt(layout string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(layout); i++ {
		if layout[i] != '%' || i+1 >= len(layout) {
			b.WriteByte(layout[i])
			continue
		}
		i++
		dateFmtCode(&b, layout[i], t)
	}
	return b.String()
}

// dateFmtCode writes one strftime conversion.
func dateFmtCode(b *strings.Builder, code byte, t time.Time) {
	// lint: sort until '^	}' where '^	case '
	switch code {
	case '%':
		b.WriteByte('%')
	case 'A':
		b.WriteString(t.Format("Monday"))
	case 'B':
		b.WriteString(t.Format("January"))
	case 'H':
		fmt.Fprintf(b, "%02d", t.Hour())
	case 'I':
		fmt.Fprintf(b, "%02d", (t.Hour()+hoursPerHalfDay-1)%hoursPerHalfDay+1)
	case 'M':
		fmt.Fprintf(b, "%02d", t.Minute())
	case 'S':
		fmt.Fprintf(b, "%02d", t.Second())
	case 'Y':
		fmt.Fprintf(b, "%04d", t.Year())
	case 'Z':
		b.WriteString(formatOffset(t))
	case 'a':
		b.WriteString(t.Format("Mon"))
	case 'b', 'h':
		b.WriteString(t.Format("Jan"))
	case 'c':
		b.WriteString(t.Format(ctimeLayout))
	case 'd':
		fmt.Fprintf(b, "%02d", t.Day())
	case 'e':
		fmt.Fprintf(b, "%2d", t.Day())
	case 'j':
		fmt.Fprintf(b, "%03d", t.YearDay())
	case 'm':
		fmt.Fprintf(b, "%02d", int(t.Month()))
	case 'n':
		b.WriteByte('\n')
	case 'p':
		if t.Hour() < hoursPerHalfDay {
			b.WriteString("AM")
		} else {
			b.WriteString("PM")
		}
	case 't':
		b.WriteByte('\t')
	case 'w':
		fmt.Fprintf(b, "%d", int(t.Weekday())%daysPerWeek)
	case 'y':
		fmt.Fprintf(b, "%02d", t.Year()%yearsPerCentury)
	default:
		b.WriteByte('%')
		b.WriteByte(code)
	}
}
