package jsvalue

import (
	"math"
	"strings"
)

// toLocaleDateString, toLocaleTimeString and toLocaleString for the en-US
// locale and the UTC time zone. Only option combinations whose output matches
// ICU exactly are produced (checked against Node in the differential test);
// anything else is reported as unsupported so it stays unresolved.

type localeOpts struct {
	weekday, year, month, day, hour, minute, second string
	zoneName, hourCycle, dateStyle, timeStyle       string
	hour12                                          *bool
}

var (
	optWeekday = map[string]bool{"narrow": true, "short": true, "long": true}
	optNumeric = map[string]bool{"numeric": true, "2-digit": true}
	optMonth   = map[string]bool{"numeric": true, "2-digit": true, "narrow": true, "short": true, "long": true}
	optStyle   = map[string]bool{"full": true, "long": true, "medium": true, "short": true}
)

func localeUnsupported(why string) Value {
	return Unsupported("Date locale formatting: " + why + " is not available at build time")
}

// supportedLocale reports whether a locales argument names en-US.
func supportedLocale(v Value) bool {
	if v.kind == KindArray && len(v.Elems()) > 0 {
		v = v.Elems()[0]
	}
	switch v.kind {
	case KindUndefined:
		return true
	case KindString:
		l := strings.ToLower(v.s)
		return l == "en-us" || l == "en"
	}
	return false
}

func dateToLocale(t float64, method string, args []Value) Value {
	if !supportedLocale(arg(args, 0)) {
		return localeUnsupported("a locale other than en-US")
	}
	opts, why := readLocaleOpts(arg(args, 1))
	if why != "" {
		return localeUnsupported(why)
	}
	required, defaults := "any", "all"
	switch method {
	case "toLocaleDateString":
		required, defaults = "date", "date"
	case "toLocaleTimeString":
		required, defaults = "time", "time"
	}
	needDefaults := true
	if (required == "date" || required == "any") && (opts.weekday != "" || opts.year != "" || opts.month != "" || opts.day != "") {
		needDefaults = false
	}
	if (required == "time" || required == "any") && (opts.hour != "" || opts.minute != "" || opts.second != "") {
		needDefaults = false
	}
	if opts.dateStyle != "" || opts.timeStyle != "" {
		needDefaults = false
		if (required == "date" && opts.timeStyle != "") || (required == "time" && opts.dateStyle != "") {
			return Unsupported("TypeError: Invalid option")
		}
		if opts.weekday != "" || opts.year != "" || opts.month != "" || opts.day != "" || opts.hour != "" || opts.minute != "" || opts.second != "" || opts.zoneName != "" {
			return Unsupported("TypeError: Can't set option together with dateStyle/timeStyle")
		}
	}
	if needDefaults && (defaults == "date" || defaults == "all") {
		opts.year, opts.month, opts.day = "numeric", "numeric", "numeric"
	}
	if needDefaults && (defaults == "time" || defaults == "all") {
		opts.hour, opts.minute, opts.second = "numeric", "numeric", "numeric"
	}
	if math.IsNaN(t) {
		return String("Invalid Date")
	}
	f := fieldsOf(t)
	s, ok := formatLocaleFields(f, opts)
	if !ok {
		return localeUnsupported("this option combination")
	}
	return String(s)
}

func readLocaleOpts(o Value) (localeOpts, string) {
	var opts localeOpts
	if o.kind == KindUndefined {
		return opts, ""
	}
	if o.kind != KindObject {
		return opts, "options that are not an object"
	}
	str := func(key string, allowed map[string]bool) (string, string) {
		v, ok := o.Get(key)
		if !ok || v.kind == KindUndefined {
			return "", ""
		}
		if v.kind != KindString || (allowed != nil && !allowed[v.s]) {
			return "", "the option " + key
		}
		return v.s, ""
	}
	for _, k := range o.Keys() {
		switch k {
		case "weekday", "year", "month", "day", "hour", "minute", "second", "timeZoneName", "hourCycle", "dateStyle", "timeStyle", "hour12", "timeZone", "localeMatcher", "formatMatcher":
		default:
			return opts, "the option " + k
		}
	}
	var why string
	if opts.weekday, why = str("weekday", optWeekday); why != "" {
		return opts, why
	}
	if opts.year, why = str("year", optNumeric); why != "" {
		return opts, why
	}
	if opts.month, why = str("month", optMonth); why != "" {
		return opts, why
	}
	if opts.day, why = str("day", optNumeric); why != "" {
		return opts, why
	}
	if opts.hour, why = str("hour", optNumeric); why != "" {
		return opts, why
	}
	if opts.minute, why = str("minute", optNumeric); why != "" {
		return opts, why
	}
	if opts.second, why = str("second", optNumeric); why != "" {
		return opts, why
	}
	if opts.zoneName, why = str("timeZoneName", map[string]bool{"short": true, "long": true}); why != "" {
		return opts, why
	}
	if opts.hourCycle, why = str("hourCycle", map[string]bool{"h23": true, "h12": true}); why != "" {
		return opts, why
	}
	if opts.dateStyle, why = str("dateStyle", optStyle); why != "" {
		return opts, why
	}
	if opts.timeStyle, why = str("timeStyle", optStyle); why != "" {
		return opts, why
	}
	if v, ok := o.Get("hour12"); ok && v.kind != KindUndefined {
		b := v.ToBoolean()
		opts.hour12 = &b
	}
	if v, ok := o.Get("timeZone"); ok && v.kind != KindUndefined {
		if v.kind != KindString {
			return opts, "the option timeZone"
		}
		switch strings.ToLower(v.s) {
		case "utc", "gmt", "etc/utc", "etc/gmt", "etc/uct", "uct", "z":
		default:
			return opts, "a time zone other than UTC"
		}
	}
	return opts, ""
}

func (o localeOpts) twentyFour() bool {
	if o.hour12 != nil {
		return !*o.hour12
	}
	return o.hourCycle == "h23"
}

func formatLocaleFields(f dateFields, o localeOpts) (string, bool) {
	if o.dateStyle != "" || o.timeStyle != "" {
		var date, tm string
		longDate := false
		switch o.dateStyle {
		case "full":
			date, longDate = dayNames[f.wday]+", "+monthNames[f.month]+" "+itoa(f.day)+", "+itoa(f.year), true
		case "long":
			date, longDate = monthNames[f.month]+" "+itoa(f.day)+", "+itoa(f.year), true
		case "medium":
			date = monthNames[f.month][:3] + " " + itoa(f.day) + ", " + itoa(f.year)
		case "short":
			date = itoa(f.month+1) + "/" + itoa(f.day) + "/" + pad(f.year%100, 2)
		}
		if o.timeStyle != "" {
			opts := localeOpts{hour: "numeric", minute: "numeric", hour12: o.hour12, hourCycle: o.hourCycle}
			if o.timeStyle != "short" {
				opts.second = "numeric"
			}
			switch o.timeStyle {
			case "full":
				opts.zoneName = "long"
			case "long":
				opts.zoneName = "short"
			}
			var ok bool
			if tm, ok = formatTimeFields(f, opts); !ok {
				return "", false
			}
		}
		switch {
		case date != "" && tm != "":
			if longDate {
				return date + " at " + tm, true
			}
			return date + ", " + tm, true
		case date != "":
			return date, true
		}
		return tm, true
	}
	hasDate := o.weekday != "" || o.year != "" || o.month != "" || o.day != ""
	hasTime := o.hour != "" || o.minute != "" || o.second != ""
	var date, tm string
	var ok bool
	if hasDate {
		if date, ok = formatDateFields(f, o); !ok {
			return "", false
		}
	}
	if hasTime {
		if tm, ok = formatTimeFields(f, o); !ok {
			return "", false
		}
	} else if o.zoneName != "" {
		return "", false
	}
	switch {
	case date != "" && tm != "":
		if o.month == "long" {
			return date + " at " + tm, true
		}
		return date + ", " + tm, true
	case date != "":
		return date, true
	}
	return tm, true
}

func itoa(n int64) string { return pad(n, 1) }

func formatDateFields(f dateFields, o localeOpts) (string, bool) {
	year := func() string {
		if o.year == "2-digit" {
			return pad(((f.year%100)+100)%100, 2)
		}
		return itoa(f.year)
	}
	day := func() string {
		if o.day == "2-digit" {
			return pad(f.day, 2)
		}
		return itoa(f.day)
	}
	monthNum := func() string {
		if o.month == "2-digit" {
			return pad(f.month+1, 2)
		}
		return itoa(f.month + 1)
	}
	weekday := func() string {
		switch o.weekday {
		case "long":
			return dayNames[f.wday]
		case "short":
			return dayNames[f.wday][:3]
		}
		return dayNames[f.wday][:1]
	}
	switch o.month {
	case "":
		switch {
		case o.weekday != "" && o.year == "" && o.day == "":
			return weekday(), true
		case o.weekday == "" && o.day == "" && o.year != "":
			return year(), true
		case o.weekday == "" && o.year == "" && o.day != "":
			return day(), true
		}
		return "", false
	case "narrow":
		if o.weekday == "" && o.year == "" && o.day == "" {
			return monthNames[f.month][:1], true
		}
		return "", false
	case "short", "long":
		name := monthNames[f.month]
		if o.month == "short" {
			name = name[:3]
		}
		out := name
		if o.day != "" {
			out += " " + day()
			if o.year != "" {
				out += ", " + year()
			}
		} else if o.year != "" {
			out += " " + year()
		}
		if o.weekday != "" {
			if o.day == "" {
				return "", false
			}
			out = weekday() + ", " + out
		}
		return out, true
	}
	// numeric and 2-digit months
	var parts []string
	parts = append(parts, monthNum())
	if o.day != "" {
		parts = append(parts, day())
	}
	if o.year != "" {
		if o.day == "" && o.weekday != "" {
			return "", false
		}
		parts = append(parts, year())
	}
	out := strings.Join(parts, "/")
	if o.weekday != "" {
		if o.day == "" {
			return "", false
		}
		out = weekday() + ", " + out
	}
	return out, true
}

func formatTimeFields(f dateFields, o localeOpts) (string, bool) {
	if o.hour == "" || (o.minute == "" && o.second != "") {
		return "", false
	}
	var out string
	if o.twentyFour() {
		out = pad(f.hour, 2)
	} else {
		h := f.hour % 12
		if h == 0 {
			h = 12
		}
		if o.hour == "2-digit" {
			out = pad(h, 2)
		} else {
			out = itoa(h)
		}
	}
	if o.minute != "" {
		out += ":" + pad(f.min, 2)
	}
	if o.second != "" {
		out += ":" + pad(f.sec, 2)
	}
	if !o.twentyFour() {
		out += " "
		if f.hour < 12 {
			out += "AM"
		} else {
			out += "PM"
		}
	}
	switch o.zoneName {
	case "short":
		out += " UTC"
	case "long":
		out += " Coordinated Universal Time"
	}
	return out, true
}
