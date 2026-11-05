package jsvalue

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Date at build time.
//
// A Date is evaluated in a fixed environment so a build gives the same output
// on every machine: the time zone is UTC (so local-time getters equal the UTC
// ones, getTimezoneOffset() is 0 and toString() prints GMT+0000) and the
// locale is en-US (toLocaleDateString() and friends). `new Date()`, `Date.now()`
// and `Date()` read the build clock (SOURCE_DATE_EPOCH, see clock.go), but only
// where the host allows volatile values (Interp.Volatile): text that is baked
// into HTML. Anything the evaluator cannot reproduce exactly, such as another
// locale or a time zone other than UTC, stays unresolved instead of guessing.

// DateValue is the state of a Date: milliseconds since the epoch, NaN when the
// Date is invalid.
type DateValue struct {
	ms float64
	// env marks a Date made where the build environment may be read (see
	// Interp.Volatile). Only such a Date answers questions that depend on the
	// time zone or locale: getHours(), toString(), toLocaleDateString(), ...
	// Any other Date leaves them unknown, because the same expression runs
	// again in the browser, which has its own zone and locale.
	env bool
}

// NewDate returns a Date for the time value ms (clipped like TimeClip).
func NewDate(ms float64) Value { return newDate(ms, false) }

func newDate(ms float64, env bool) Value {
	return Value{kind: KindDate, date: &DateValue{ms: timeClip(ms), env: env}}
}

// dateNeedsEnv is the Unknown a Date answers with for something that depends
// on the environment it is read in.
func dateNeedsEnv(what string) Value {
	return Dynamic(what + " depends on the time zone and locale it runs in")
}

// DateMS returns the time value of a Date value.
func (v Value) DateMS() (float64, bool) {
	if v.kind != KindDate || v.date == nil {
		return 0, false
	}
	return v.date.ms, true
}

const (
	msPerDay    = 86400000.0
	maxTimeMS   = 8.64e15
	msPerHour   = 3600000.0
	msPerMinute = 60000.0
)

func timeClip(t float64) float64 {
	if math.IsNaN(t) || math.IsInf(t, 0) || math.Abs(t) > maxTimeMS {
		return math.NaN()
	}
	return math.Trunc(t) + 0 // + 0 turns -0 into +0
}

func finite(xs ...float64) bool {
	for _, x := range xs {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// daysFromCivil returns the days since 1970-01-01 of a proleptic Gregorian date.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era = (y - 399) / 400
	}
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// civilFromDays is the inverse of daysFromCivil.
func civilFromDays(z int64) (y, m, d int64) {
	z += 719468
	era := z / 146097
	if z < 0 && z%146097 != 0 {
		era = (z - 146096) / 146097
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = doy - (153*mp+2)/5 + 1
	if mp < 10 {
		m = mp + 3
	} else {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

// makeDay is MakeDay: the day number of year/month/date, all of which may be
// out of range.
func makeDay(year, month, date float64) float64 {
	if !finite(year, month, date) {
		return math.NaN()
	}
	y, m, dt := math.Trunc(year), math.Trunc(month), math.Trunc(date)
	ym := y + math.Floor(m/12)
	if math.Abs(ym) > 400000 {
		return math.NaN()
	}
	mn := math.Mod(m, 12)
	if mn < 0 {
		mn += 12
	}
	days := daysFromCivil(int64(ym), int64(mn)+1, 1)
	return float64(days) + dt - 1
}

func makeTime(h, m, s, ms float64) float64 {
	if !finite(h, m, s, ms) {
		return math.NaN()
	}
	return math.Trunc(h)*msPerHour + math.Trunc(m)*msPerMinute + math.Trunc(s)*1000 + math.Trunc(ms)
}

func makeDate(day, tm float64) float64 {
	if !finite(day, tm) {
		return math.NaN()
	}
	t := day*msPerDay + tm
	if !finite(t) {
		return math.NaN()
	}
	return t
}

// dateFields are the broken-down parts of a valid time value.
type dateFields struct {
	year, month, day         int64 // month is 0-11, day 1-31
	hour, min, sec, ms, wday int64 // wday is 0 (Sunday) - 6
}

func fieldsOf(t float64) dateFields {
	days := math.Floor(t / msPerDay)
	rem := int64(t - days*msPerDay)
	y, m, d := civilFromDays(int64(days))
	wd := (int64(days) + 4) % 7
	if wd < 0 {
		wd += 7
	}
	return dateFields{
		year: y, month: m - 1, day: d,
		hour: rem / 3600000, min: rem / 60000 % 60, sec: rem / 1000 % 60, ms: rem % 1000,
		wday: wd,
	}
}

var (
	dayNames   = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// yearString formats a year like V8's toString: at least four digits, with a
// minus sign for years before 0.
func yearString(y int64) string {
	if y < 0 {
		return "-" + pad(-y, 4)
	}
	return pad(y, 4)
}

func dateISO(t float64) string {
	f := fieldsOf(t)
	var y string
	switch {
	case f.year >= 0 && f.year <= 9999:
		y = pad(f.year, 4)
	case f.year < 0:
		y = "-" + pad(-f.year, 6)
	default:
		y = "+" + pad(f.year, 6)
	}
	return y + "-" + pad(f.month+1, 2) + "-" + pad(f.day, 2) + "T" + pad(f.hour, 2) + ":" + pad(f.min, 2) + ":" + pad(f.sec, 2) + "." + pad(f.ms, 3) + "Z"
}

const tzSuffix = "GMT+0000 (Coordinated Universal Time)"

func dateString(t float64) string {
	if math.IsNaN(t) {
		return "Invalid Date"
	}
	return dateOnlyString(t) + " " + timeOnlyString(t)
}

func dateOnlyString(t float64) string {
	f := fieldsOf(t)
	return dayNames[f.wday][:3] + " " + monthNames[f.month][:3] + " " + pad(f.day, 2) + " " + yearString(f.year)
}

func timeOnlyString(t float64) string {
	f := fieldsOf(t)
	return pad(f.hour, 2) + ":" + pad(f.min, 2) + ":" + pad(f.sec, 2) + " " + tzSuffix
}

func dateUTCString(t float64) string {
	f := fieldsOf(t)
	return dayNames[f.wday][:3] + ", " + pad(f.day, 2) + " " + monthNames[f.month][:3] + " " + yearString(f.year) + " " + pad(f.hour, 2) + ":" + pad(f.min, 2) + ":" + pad(f.sec, 2) + " GMT"
}

// Parsing

// parseStatus tells what parseDate made of a string.
type parseStatus int

const (
	parsed      parseStatus = iota // a valid time value
	parseNaN                       // Date.parse would give NaN
	parseUnsure                    // a format the evaluator does not model
)

var isoRe = regexp.MustCompile(`^([+-]\d{6}|\d{4})(?:-(\d{2})(?:-(\d{2}))?)?(?:[Tt ](\d{2}):(\d{2})(?::(\d{2})(?:[.,](\d+))?)?\s*(Z|z|[+-]\d{2}:?\d{2})?)?$`)

// parseDate implements Date.parse for ISO 8601 strings and the formats Date's
// own toString/toUTCString/toDateString produce, plus "Month D, YYYY" and
// numeric M/D/YYYY. In the build environment local time is UTC.
//
// local is true when the string names no time zone, so JS reads it in the
// local zone: the result then depends on the environment.
func parseDate(s string) (t float64, status parseStatus, local bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return math.NaN(), parseNaN, false
	}
	if m := isoRe.FindStringSubmatch(s); m != nil {
		t, status = parseISOParts(s, m)
		// A date without a time is UTC; a date-time without an offset is local.
		return t, status, m[4] != "" && m[8] == ""
	}
	if t, local, ok := parseLegacyDate(s); ok {
		return t, parsed, local
	}
	// V8's legacy parser needs a number somewhere; a string without digits is
	// always an invalid date.
	if !strings.ContainsAny(s, "0123456789") {
		return math.NaN(), parseNaN, false
	}
	return math.NaN(), parseUnsure, false
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func parseISOParts(s string, m []string) (float64, parseStatus) {
	if m[1] == "-000000" {
		return math.NaN(), parseUnsure // V8 falls back to its legacy parser here
	}
	year := atoi(m[1])
	month, day := int64(1), int64(1)
	if m[2] != "" {
		month = atoi(m[2])
	}
	if m[3] != "" {
		day = atoi(m[3])
	}
	hasTime := m[4] != ""
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return math.NaN(), parseNaN
	}
	var h, mi, sec, ms int64
	if hasTime {
		h, mi = atoi(m[4]), atoi(m[5])
		if m[6] != "" {
			sec = atoi(m[6])
		}
		if m[7] != "" {
			frac := m[7]
			if len(frac) > 3 {
				frac = frac[:3]
			}
			for len(frac) < 3 {
				frac += "0"
			}
			ms = atoi(frac)
		}
		if h > 24 || mi > 59 || sec > 59 || (h == 24 && (mi != 0 || sec != 0 || ms != 0)) {
			return math.NaN(), parseNaN
		}
	}
	days := makeDay(float64(year), float64(month-1), float64(day))
	t := makeDate(days, makeTime(float64(h), float64(mi), float64(sec), float64(ms)))
	if tz := m[8]; tz != "" && tz != "Z" && tz != "z" {
		sign := 1.0
		if tz[0] == '-' {
			sign = -1
		}
		digits := strings.ReplaceAll(tz[1:], ":", "")
		th, tm := atoi(digits[:2]), atoi(digits[2:])
		if th > 23 || tm > 59 {
			return math.NaN(), parseNaN
		}
		t -= sign * (float64(th)*msPerHour + float64(tm)*msPerMinute)
	}
	return timeClip(t), parsed
}

var (
	monthIndex = func() map[string]int64 {
		m := map[string]int64{}
		for i, n := range monthNames {
			m[strings.ToLower(n)] = int64(i)
			m[strings.ToLower(n[:3])] = int64(i)
		}
		m["sept"] = 8
		return m
	}()
	legacyDayRe = `(?:(?:Sun|Mon|Tue|Wed|Thu|Fri|Sat)[a-z]*,?\s+)?`
	legacyTime  = `(?:\s+(\d{1,2}):(\d{2})(?::(\d{2}))?(?:\s*([AaPp][Mm]))?)?`
	legacyZone  = `(?:\s*(?:(GMT|UTC|Z)?([+-]\d{4})?)(?:\s*\([A-Za-z ]+\))?)?`
	// Thu, 05 Nov 2026 14:37:15 GMT   |   5 November 2026
	rfcRe = regexp.MustCompile(`^` + legacyDayRe + `(\d{1,2})\s+([A-Za-z]{3,9})\s+(-?\d{1,6})` + legacyTime + legacyZone + `$`)
	// Thu Nov 05 2026 14:37:15 GMT+0000 (...)   |   November 5, 2026 10:00 PM   |   Nov 5 2026
	mdyRe = regexp.MustCompile(`^` + legacyDayRe + `([A-Za-z]{3,9})\.?\s+(\d{1,2}),?\s+(-?\d{1,6})` + legacyTime + legacyZone + `$`)
	// 11/5/2026 10:00 PM   |   11/05/26
	slashMDY = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{1,4})` + legacyTime + `$`)
	// 2026/11/05 10:00
	slashYMD = regexp.MustCompile(`^(\d{4})/(\d{1,2})/(\d{1,2})` + legacyTime + `$`)
)

func parseLegacyDate(s string) (t float64, local, ok bool) {
	var y, mo, d int64
	var tm []string
	var zone []string
	switch {
	case rfcRe.MatchString(s):
		m := rfcRe.FindStringSubmatch(s)
		idx, ok := monthIndex[strings.ToLower(m[2])]
		if !ok {
			return 0, false, false
		}
		d, mo, y = atoi(m[1]), idx, atoi(m[3])
		tm, zone = m[4:8], m[8:10]
	case mdyRe.MatchString(s):
		m := mdyRe.FindStringSubmatch(s)
		idx, ok := monthIndex[strings.ToLower(m[1])]
		if !ok {
			return 0, false, false
		}
		mo, d, y = idx, atoi(m[2]), atoi(m[3])
		tm, zone = m[4:8], m[8:10]
	case slashMDY.MatchString(s):
		m := slashMDY.FindStringSubmatch(s)
		mo, d, y = atoi(m[1])-1, atoi(m[2]), atoi(m[3])
		if len(m[3]) <= 2 {
			// Two-digit years are 1950-2049 in V8.
			if y >= 50 {
				y += 1900
			} else {
				y += 2000
			}
		}
		tm = m[4:8]
	case slashYMD.MatchString(s):
		m := slashYMD.FindStringSubmatch(s)
		y, mo, d = atoi(m[1]), atoi(m[2])-1, atoi(m[3])
		tm = m[4:8]
	default:
		return 0, false, false
	}
	if mo < 0 || mo > 11 || d < 1 || d > 31 {
		return 0, false, false
	}
	var h, mi, sec int64
	if len(tm) == 4 && tm[0] != "" {
		h, mi = atoi(tm[0]), atoi(tm[1])
		if tm[2] != "" {
			sec = atoi(tm[2])
		}
		if ap := strings.ToLower(tm[3]); ap != "" {
			if h < 1 || h > 12 {
				return 0, false, false
			}
			h %= 12
			if ap == "pm" {
				h += 12
			}
		}
		if h > 23 || mi > 59 || sec > 59 {
			return 0, false, false
		}
	}
	t = makeDate(makeDay(float64(y), float64(mo), float64(d)), makeTime(float64(h), float64(mi), float64(sec), 0))
	local = true
	if len(zone) == 2 && (zone[1] != "" || zone[0] != "") {
		local = false
	}
	if len(zone) == 2 && zone[1] != "" {
		off := zone[1]
		sign := 1.0
		if off[0] == '-' {
			sign = -1
		}
		t -= sign * (float64(atoi(off[1:3]))*msPerHour + float64(atoi(off[3:5]))*msPerMinute)
	}
	return timeClip(t), local, true
}

// Constructor and statics

// dateFromComponents implements Date.UTC and new Date(y, m, ...): in the build
// environment local time and UTC coincide.
func dateFromComponents(args []Value) float64 {
	nums := make([]float64, 7)
	defaults := []float64{math.NaN(), 0, 1, 0, 0, 0, 0}
	for i := range nums {
		if i < len(args) {
			nums[i] = args[i].ToNumber()
		} else {
			nums[i] = defaults[i]
		}
	}
	y := nums[0]
	if !math.IsNaN(y) {
		if yi := math.Trunc(y); yi >= 0 && yi <= 99 {
			y = 1900 + yi
		}
	}
	return timeClip(makeDate(makeDay(y, nums[1], nums[2]), makeTime(nums[3], nums[4], nums[5], nums[6])))
}

// newDateFromArgs implements `new Date(...args)`. volatile says whether the
// build environment (clock, time zone) may be read.
func newDateFromArgs(args []Value, volatile bool) Value {
	for _, a := range args {
		if a.IsUnknown() {
			return a
		}
	}
	switch len(args) {
	case 0:
		if !volatile {
			return Dynamic("new Date() depends on the time it runs")
		}
		return newDate(nowMS(), true)
	case 1:
		a := args[0]
		if a.kind == KindDate {
			return newDate(a.date.ms, volatile || a.date.env)
		}
		p := a.ToPrimitive()
		if p.kind == KindString {
			t, st, local := parseDate(p.s)
			switch {
			case st == parseUnsure:
				return Unsupported("Date string format " + strconv.Quote(p.s) + " is not modelled at build time")
			case local && !volatile:
				return dateNeedsEnv("a date-time without a time zone")
			}
			return newDate(t, volatile)
		}
		return newDate(p.ToNumber(), volatile)
	}
	for _, a := range args {
		if a.kind == KindObject || a.kind == KindArray {
			return Unsupported("Date components that are objects")
		}
	}
	if !volatile {
		return dateNeedsEnv("new Date(year, month, ...)")
	}
	return newDate(dateFromComponents(args), true)
}

func dateCtor(in *Interp) Value {
	f := Func("Date", func([]Value) Value {
		// Date() without new: the current time as a string.
		if !in.Volatile {
			return Dynamic("Date() depends on the time it runs")
		}
		return String(dateString(timeClip(nowMS())))
	})
	f.fn.Construct = func(args []Value) Value { return newDateFromArgs(args, in.Volatile) }
	f.fn.Builtin = true
	f.fn.Props = map[string]Value{
		"now": Func("now", func([]Value) Value {
			if !in.Volatile {
				return Dynamic("Date.now() depends on the time it runs")
			}
			return Number(nowMS())
		}),
		"UTC": Func("UTC", func(args []Value) Value {
			for _, a := range args {
				if a.IsUnknown() {
					return a
				}
			}
			if len(args) == 0 {
				return Number(math.NaN())
			}
			return Number(dateFromComponents(args))
		}),
		"parse": Func("parse", func(args []Value) Value {
			a := arg(args, 0)
			if a.IsUnknown() {
				return a
			}
			t, st, local := parseDate(a.ToPrimitive().ToString())
			switch {
			case st == parseUnsure:
				return Unsupported("Date string format is not modelled at build time")
			case local && !in.Volatile:
				return dateNeedsEnv("a date-time without a time zone")
			}
			return Number(t)
		}),
	}
	return f
}

// Methods

var dateMethods = memberSet("getTime valueOf getFullYear getMonth getDate getDay getHours getMinutes getSeconds getMilliseconds getUTCFullYear getUTCMonth getUTCDate getUTCDay getUTCHours getUTCMinutes getUTCSeconds getUTCMilliseconds getYear getTimezoneOffset toISOString toJSON toString toDateString toTimeString toUTCString toGMTString toLocaleDateString toLocaleTimeString toLocaleString setTime setFullYear setMonth setDate setHours setMinutes setSeconds setMilliseconds setUTCFullYear setUTCMonth setUTCDate setUTCHours setUTCMinutes setUTCSeconds setUTCMilliseconds")

// dateBuiltinMembers are all Date.prototype members; those without an
// implementation stay unresolved.
var dateBuiltinMembers = memberSet("constructor hasOwnProperty isPrototypeOf propertyIsEnumerable toString valueOf __proto__ getTime getFullYear getMonth getDate getDay getHours getMinutes getSeconds getMilliseconds getUTCFullYear getUTCMonth getUTCDate getUTCDay getUTCHours getUTCMinutes getUTCSeconds getUTCMilliseconds getYear setYear getTimezoneOffset toISOString toJSON toDateString toTimeString toUTCString toGMTString toLocaleDateString toLocaleTimeString toLocaleString setTime setFullYear setMonth setDate setHours setMinutes setSeconds setMilliseconds setUTCFullYear setUTCMonth setUTCDate setUTCHours setUTCMinutes setUTCSeconds setUTCMilliseconds")

// dateEnvMethods read or write the local time, or print it: their result
// depends on the time zone and locale.
func dateEnvMethod(name string) bool {
	switch name {
	case "getFullYear", "getMonth", "getDate", "getDay", "getHours", "getMinutes", "getSeconds", "getMilliseconds", "getYear",
		"getTimezoneOffset", "setFullYear", "setMonth", "setDate", "setHours", "setMinutes", "setSeconds", "setMilliseconds",
		"toString", "toDateString", "toTimeString", "toLocaleDateString", "toLocaleTimeString", "toLocaleString":
		return true
	}
	return false
}

func dateMethod(recv Value, name string, args []Value) Value {
	d := recv.date
	t := d.ms
	valid := !math.IsNaN(t)
	if !d.env && dateEnvMethod(name) {
		return dateNeedsEnv("Date.prototype." + name)
	}
	switch name {
	case "getTime", "valueOf":
		return Number(t)
	case "getTimezoneOffset":
		if !valid {
			return Number(math.NaN())
		}
		return Number(0)
	case "toString":
		return String(dateString(t))
	case "toDateString":
		if !valid {
			return String("Invalid Date")
		}
		return String(dateOnlyString(t))
	case "toTimeString":
		if !valid {
			return String("Invalid Date")
		}
		return String(timeOnlyString(t))
	case "toUTCString", "toGMTString":
		if !valid {
			return String("Invalid Date")
		}
		return String(dateUTCString(t))
	case "toISOString":
		if !valid {
			return Unsupported("RangeError: Invalid time value")
		}
		return String(dateISO(t))
	case "toJSON":
		if !valid {
			return Null()
		}
		return String(dateISO(t))
	case "toLocaleDateString", "toLocaleTimeString", "toLocaleString":
		return dateToLocale(t, name, args)
	}
	if strings.HasPrefix(name, "get") {
		return Number(dateGetter(t, strings.TrimPrefix(name, "get")))
	}
	if strings.HasPrefix(name, "set") {
		return dateSetter(d, strings.TrimPrefix(name, "set"), args)
	}
	return Unsupported("Date.prototype." + name)
}

// dateGetter reads a field. Local and UTC getters agree in the build
// environment.
func dateGetter(t float64, field string) float64 {
	if math.IsNaN(t) {
		return math.NaN()
	}
	field = strings.TrimPrefix(field, "UTC")
	f := fieldsOf(t)
	switch field {
	case "FullYear":
		return float64(f.year)
	case "Year":
		return float64(f.year - 1900)
	case "Month":
		return float64(f.month)
	case "Date":
		return float64(f.day)
	case "Day":
		return float64(f.wday)
	case "Hours":
		return float64(f.hour)
	case "Minutes":
		return float64(f.min)
	case "Seconds":
		return float64(f.sec)
	case "Milliseconds":
		return float64(f.ms)
	}
	return math.NaN()
}

// dateSetter implements the set* methods (local = UTC). It returns the new
// time value.
func dateSetter(d *DateValue, field string, args []Value) Value {
	field = strings.TrimPrefix(field, "UTC")
	nums := make([]float64, len(args))
	for i, a := range args {
		nums[i] = a.ToNumber()
	}
	num := func(i int, def float64) float64 {
		if i < len(nums) {
			return nums[i]
		}
		return def
	}
	t := d.ms
	if field == "Time" {
		d.ms = timeClip(num(0, math.NaN()))
		return Number(d.ms)
	}
	if math.IsNaN(t) {
		if field != "FullYear" {
			return Number(math.NaN())
		}
		t = 0
	}
	if len(args) == 0 {
		d.ms = math.NaN()
		return Number(d.ms)
	}
	f := fieldsOf(t)
	year, month, day := float64(f.year), float64(f.month), float64(f.day)
	h, mi, s, ms := float64(f.hour), float64(f.min), float64(f.sec), float64(f.ms)
	switch field {
	case "FullYear":
		year, month, day = nums[0], num(1, month), num(2, day)
	case "Month":
		month, day = nums[0], num(1, day)
	case "Date":
		day = nums[0]
	case "Hours":
		h, mi, s, ms = nums[0], num(1, mi), num(2, s), num(3, ms)
	case "Minutes":
		mi, s, ms = nums[0], num(1, s), num(2, ms)
	case "Seconds":
		s, ms = nums[0], num(1, ms)
	case "Milliseconds":
		ms = nums[0]
	default:
		return Unsupported("Date.prototype.set" + field)
	}
	d.ms = timeClip(makeDate(makeDay(year, month, day), makeTime(h, mi, s, ms)))
	return Number(d.ms)
}
