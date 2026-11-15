// Package calendar reads calendars published as links (the iCalendar format
// that Google, iCloud, Outlook and Synology Calendar all offer) and reports
// the next events.
package calendar

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Event is one occurrence of a calendar entry.
type Event struct {
	Title string    `json:"title"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// AllDay events have no time of day; Start is midnight of their first day.
	AllDay bool `json:"allDay"`
}

// State is what the dashboard receives.
type State struct {
	Events []Event `json:"events"`
}

// Options configure the provider.
type Options struct {
	// URLs are the calendar links.
	URLs []string
	// Days is how far ahead to look; Max how many events to report.
	Days, Max int
	Interval  time.Duration
	Client    *http.Client
	// Now is the clock, for tests.
	Now func() time.Time
}

// Provider fetches the calendars.
type Provider struct{ opt Options }

// New returns a calendar provider.
func New(opt Options) *Provider {
	if opt.Days <= 0 {
		opt.Days = 14
	}
	if opt.Max <= 0 {
		opt.Max = 8
	}
	if opt.Interval <= 0 {
		opt.Interval = 15 * time.Minute
	}
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Provider{opt: opt}
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "calendar" }

// Interval implements providers.Provider.
func (p *Provider) Interval() time.Duration { return p.opt.Interval }

// Fetch implements providers.Provider. One calendar that cannot be read does
// not hide the others; only when none can be read is that an error.
func (p *Provider) Fetch(ctx context.Context) (any, error) {
	now := p.opt.Now()
	var events []Event
	var firstErr error
	read := 0
	for _, u := range p.opt.URLs {
		found, err := p.fetchOne(ctx, u, now)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		read++
		events = append(events, found...)
	}
	if read == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].Start.Equal(events[j].Start) {
			return events[i].Start.Before(events[j].Start)
		}
		return events[i].Title < events[j].Title
	})
	if len(events) > p.opt.Max {
		events = events[:p.opt.Max]
	}
	if events == nil {
		events = []Event{}
	}
	return State{Events: events}, nil
}

// Count reads one calendar link and returns how many events it has coming
// up, for checking a link before it is saved.
func (p *Provider) Count(ctx context.Context, link string) (int, error) {
	events, err := p.fetchOne(ctx, link, p.opt.Now())
	return len(events), err
}

func (p *Provider) fetchOne(ctx context.Context, link string, now time.Time) ([]Event, error) {
	// Calendar programs hand out webcal:// links; they are ordinary https.
	link = strings.TrimSpace(link)
	if rest, ok := strings.CutPrefix(strings.ToLower(link), "webcal://"); ok {
		link = "https://" + link[len(link)-len(rest):]
	}
	if u, err := url.Parse(link); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("the calendar link is not a valid address; it starts with https:// or webcal://")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, errors.New("the calendar link is not a valid address")
	}
	req.Header.Set("User-Agent", "PiDash")
	resp, err := p.opt.Client.Do(req)
	if err != nil {
		return nil, errors.New("the calendar could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the calendar answered with error %d; check the link", resp.StatusCode)
	}
	entries, err := parse(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	return upcoming(entries, now, now.AddDate(0, 0, p.opt.Days)), nil
}

// entry is a VEVENT as written in the calendar, before repetition is worked out.
type entry struct {
	uid, title string
	start, end time.Time
	allDay     bool
	rule       string
	excluded   []time.Time
	// override is set on an entry that replaces one occurrence of a repeating
	// entry (a meeting moved once): the time of the occurrence it replaces.
	override  time.Time
	cancelled bool
}

var errNotCalendar = errors.New("the link does not lead to a calendar (expected iCalendar data)")

func parse(r io.Reader) ([]entry, error) {
	// Long lines are folded: a line starting with a space continues the last.
	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, errors.New("the calendar could not be read completely")
	}
	isCalendar := false
	var out []entry
	var cur *entry
	depth := 0 // inside a VEVENT: nested parts such as reminders are skipped
	var duration time.Duration
	for _, line := range lines {
		name, params, value := split(line)
		switch {
		case name == "BEGIN" && strings.EqualFold(value, "VCALENDAR"):
			isCalendar = true
		case name == "BEGIN" && strings.EqualFold(value, "VEVENT") && cur == nil:
			cur, depth, duration = &entry{}, 0, 0
		case name == "BEGIN" && cur != nil:
			depth++
		case name == "END" && cur != nil && depth > 0:
			depth--
		case name == "END" && cur != nil && strings.EqualFold(value, "VEVENT"):
			if !cur.start.IsZero() {
				switch {
				case !cur.end.IsZero():
				case duration > 0:
					cur.end = cur.start.Add(duration)
				case cur.allDay:
					cur.end = cur.start.AddDate(0, 0, 1)
				default:
					cur.end = cur.start
				}
				out = append(out, *cur)
			}
			cur = nil
		case cur == nil || depth > 0:
		case name == "UID":
			cur.uid = value
		case name == "SUMMARY":
			cur.title = unescape(value)
		case name == "STATUS":
			cur.cancelled = strings.EqualFold(value, "CANCELLED")
		case name == "DTSTART":
			cur.start, cur.allDay = parseTime(value, params)
		case name == "DTEND":
			cur.end, _ = parseTime(value, params)
		case name == "DURATION":
			duration = parseDuration(value)
		case name == "RRULE":
			cur.rule = value
		case name == "RECURRENCE-ID":
			cur.override, _ = parseTime(value, params)
		case name == "EXDATE":
			for _, v := range strings.Split(value, ",") {
				if t, _ := parseTime(v, params); !t.IsZero() {
					cur.excluded = append(cur.excluded, t)
				}
			}
		}
	}
	if !isCalendar {
		return nil, errNotCalendar
	}
	return out, nil
}

// split takes "NAME;PARAM=x:value" apart. A colon inside a quoted parameter
// does not end the name.
func split(line string) (name string, params map[string]string, value string) {
	quoted := false
	colon := -1
	for i := 0; i < len(line) && colon < 0; i++ {
		switch line[i] {
		case '"':
			quoted = !quoted
		case ':':
			if !quoted {
				colon = i
			}
		}
	}
	if colon < 0 {
		return "", nil, ""
	}
	head, value := line[:colon], line[colon+1:]
	parts := strings.Split(head, ";")
	params = map[string]string{}
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return strings.ToUpper(parts[0]), params, value
}

func unescape(s string) string {
	return strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// parseTime reads a date or a date with time. Times without a zone, or in a
// zone this machine does not know, are taken as local time.
func parseTime(value string, params map[string]string) (t time.Time, allDay bool) {
	value = strings.TrimSpace(value)
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	if len(value) == 8 || strings.EqualFold(params["VALUE"], "DATE") {
		d, err := time.ParseInLocation("20060102", value[:min(8, len(value))], time.Local)
		if err != nil {
			return time.Time{}, false
		}
		return d, true
	}
	if strings.HasSuffix(value, "Z") {
		if u, err := time.Parse("20060102T150405Z", value); err == nil {
			return u.In(time.Local), false
		}
		return time.Time{}, false
	}
	if l, err := time.ParseInLocation("20060102T150405", value, loc); err == nil {
		return l.In(time.Local), false
	}
	return time.Time{}, false
}

// parseDuration reads the durations calendars use: P1D, PT1H30M, P1W.
func parseDuration(v string) time.Duration {
	v = strings.TrimPrefix(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(v)), "+"), "P")
	var total time.Duration
	inTime := false
	num := ""
	for _, c := range v {
		switch {
		case c == 'T':
			inTime = true
		case c >= '0' && c <= '9':
			num += string(c)
		default:
			n, _ := strconv.Atoi(num)
			num = ""
			switch {
			case c == 'W':
				total += time.Duration(n) * 7 * 24 * time.Hour
			case c == 'D':
				total += time.Duration(n) * 24 * time.Hour
			case c == 'H' && inTime:
				total += time.Duration(n) * time.Hour
			case c == 'M' && inTime:
				total += time.Duration(n) * time.Minute
			case c == 'S' && inTime:
				total += time.Duration(n) * time.Second
			}
		}
	}
	return total
}

// upcoming works out which occurrences fall between from and to: events that
// start in that time, and ones that are still going on.
func upcoming(entries []entry, from, to time.Time) []Event {
	// Occurrences that were moved or cancelled once must not also appear at
	// their regular time.
	replaced := map[string][]time.Time{}
	for _, e := range entries {
		if !e.override.IsZero() && e.uid != "" {
			replaced[e.uid] = append(replaced[e.uid], e.override)
		}
	}
	within := func(start, end time.Time) bool {
		return start.Before(to) && (end.After(from) || (start.Equal(end) && !start.Before(from)))
	}
	var out []Event
	add := func(e entry, start time.Time) {
		length := e.end.Sub(e.start)
		end := start.Add(length)
		if e.allDay {
			// Whole days, also across a change of the clocks.
			end = start.AddDate(0, 0, max(1, int(length.Hours()+12)/24))
		}
		if !within(start, end) {
			return
		}
		title := strings.TrimSpace(e.title)
		if title == "" {
			title = "Busy"
		}
		out = append(out, Event{Title: title, Start: start, End: end, AllDay: e.allDay})
	}
	for _, e := range entries {
		if e.cancelled {
			continue
		}
		if e.rule == "" || !e.override.IsZero() {
			add(e, e.start)
			continue
		}
		opt, err := rrule.StrToROption(e.rule)
		if err != nil {
			add(e, e.start) // a rule this program cannot read: show at least the first time
			continue
		}
		opt.Dtstart = e.start
		rule, err := rrule.NewRRule(*opt)
		if err != nil {
			add(e, e.start)
			continue
		}
		length := e.end.Sub(e.start)
		skip := append(append([]time.Time{}, e.excluded...), replaced[e.uid]...)
	occurrences:
		for _, start := range rule.Between(from.Add(-length-time.Second), to, true) {
			for _, x := range skip {
				if x.Equal(start) {
					continue occurrences
				}
			}
			add(e, start)
		}
	}
	return out
}
