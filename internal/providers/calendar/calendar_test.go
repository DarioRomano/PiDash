package calendar

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Tuesday 10 March 2026, nine in the morning.
var now = time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)

const feed = `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:dentist
SUMMARY:Dentist\, Dr. Meier
DTSTART:20260311T140000
DTEND:20260311T150000
BEGIN:VALARM
TRIGGER:-PT15M
DESCRIPTION:SUMMARY:not the title
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:utc
SUMMARY:Call in world time
DTSTART:20260312T080000Z
DURATION:PT45M
END:VEVENT
BEGIN:VEVENT
UID:zone
SUMMARY:Flight from Lisbon
DTSTART;TZID=Europe/Lisbon:20260313T120000
DTEND;TZID="Europe/Lisbon":20260313T140000
END:VEVENT
BEGIN:VEVENT
UID:holiday
SUMMARY:School holiday with a very long name that the calendar program folds over
  two lines
DTSTART;VALUE=DATE:20260309
DTEND;VALUE=DATE:20260312
END:VEVENT
BEGIN:VEVENT
UID:birthday
SUMMARY:Anna's birthday
DTSTART;VALUE=DATE:19900315
RRULE:FREQ=YEARLY
END:VEVENT
BEGIN:VEVENT
UID:training
SUMMARY:Training
DTSTART:20260105T180000
DTEND:20260105T193000
RRULE:FREQ=WEEKLY;BYDAY=MO,TH
EXDATE:20260312T180000
END:VEVENT
BEGIN:VEVENT
UID:training
RECURRENCE-ID:20260316T180000
SUMMARY:Training (moved to the big hall)
DTSTART:20260316T190000
DTEND:20260316T203000
END:VEVENT
BEGIN:VEVENT
UID:ended
SUMMARY:Course that has ended
DTSTART:20260101T100000
DTEND:20260101T110000
RRULE:FREQ=WEEKLY;UNTIL=20260201T000000Z
END:VEVENT
BEGIN:VEVENT
UID:off
SUMMARY:Cancelled dinner
STATUS:CANCELLED
DTSTART:20260311T190000
DTEND:20260311T210000
END:VEVENT
BEGIN:VEVENT
UID:past
SUMMARY:Yesterday's meeting
DTSTART:20260309T100000
DTEND:20260309T110000
END:VEVENT
BEGIN:VEVENT
UID:far
SUMMARY:Next month
DTSTART:20260420T100000
DTEND:20260420T110000
END:VEVENT
END:VCALENDAR
`

func serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/family.ics":
			fmt.Fprint(w, strings.ReplaceAll(feed, "\n", "\r\n"))
		case "/work.ics":
			fmt.Fprint(w, "BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:Stand-up\nDTSTART:20260310T093000\nDTEND:20260310T094500\nEND:VEVENT\nEND:VCALENDAR\n")
		case "/page":
			fmt.Fprint(w, "<html>Sign in to see this calendar</html>")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func list(t *testing.T, opt Options) []string {
	t.Helper()
	opt.Now = func() time.Time { return now }
	st, err := New(opt).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range st.(State).Events {
		when := e.Start.Format("Mon 2 15:04")
		if e.AllDay {
			when = e.Start.Format("Mon 2") + " all day until " + e.End.Format("Mon 2")
		}
		out = append(out, when+" "+e.Title)
	}
	return out
}

func TestUpcomingEvents(t *testing.T) {
	srv := serve(t)
	got := list(t, Options{URLs: []string{srv.URL + "/family.ics"}, Max: 50})

	lisbon, _ := time.LoadLocation("Europe/Lisbon")
	flight := time.Date(2026, 3, 13, 12, 0, 0, 0, lisbon).In(time.Local).Format("Mon 2 15:04")
	call := time.Date(2026, 3, 12, 8, 0, 0, 0, time.UTC).In(time.Local).Format("Mon 2 15:04")
	want := []string{
		"Mon 9 all day until Thu 12 School holiday with a very long name that the calendar program folds over two lines", // still going on
		"Wed 11 14:00 Dentist, Dr. Meier",
		call + " Call in world time",
		flight + " Flight from Lisbon",
		"Sun 15 all day until Mon 16 Anna's birthday",
		"Mon 16 19:00 Training (moved to the big hall)", // moved once; not also at 18:00
		"Thu 19 18:00 Training",
		"Mon 23 18:00 Training",
	}
	// The order of the two events that depend on the zone of this machine may differ.
	has := map[string]bool{}
	for _, g := range got {
		has[g] = true
	}
	for _, w := range want {
		if !has[w] {
			t.Errorf("missing: %s", w)
		}
		delete(has, w)
	}
	for extra := range has {
		// Thursday the 12th is excluded, the dinner cancelled, the course over,
		// yesterday past and next month too far.
		t.Errorf("unexpected: %s", extra)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1][:3] == got[i][:3] && got[i-1] > got[i] && !strings.Contains(got[i-1], "all day") {
			t.Errorf("not in order: %q before %q", got[i-1], got[i])
		}
	}
}

func TestSeveralCalendarsAndTheLimit(t *testing.T) {
	srv := serve(t)
	webcal := "webcal://" + strings.TrimPrefix(srv.URL, "http://") + "/work.ics"
	_ = webcal // a webcal link means https, which the test server does not speak
	got := list(t, Options{URLs: []string{srv.URL + "/family.ics", srv.URL + "/work.ics", srv.URL + "/gone.ics"}, Max: 3})
	if len(got) != 3 || got[1] != "Tue 10 09:30 Stand-up" {
		t.Errorf("events = %q; want three, the stand-up second, despite one calendar being gone", got)
	}
}

func TestProblemsAreExplained(t *testing.T) {
	srv := serve(t)
	p := New(Options{Now: func() time.Time { return now }})
	for link, want := range map[string]string{
		srv.URL + "/gone.ics":      "error 404",
		srv.URL + "/page":          "does not lead to a calendar",
		"http://127.0.0.1:1/x.ics": "could not be reached",
		"not a link\n":             "not a valid address",
	} {
		if _, err := p.Count(context.Background(), link); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", link, err, want)
		}
	}
	if n, err := p.Count(context.Background(), srv.URL+"/work.ics"); err != nil || n != 1 {
		t.Errorf("count = %d, err %v", n, err)
	}
	// Every calendar failing is an error; the dashboard then keeps the last good list.
	if _, err := New(Options{URLs: []string{srv.URL + "/gone.ics"}}).Fetch(context.Background()); err == nil {
		t.Error("no error although no calendar could be read")
	}
}

func TestDurations(t *testing.T) {
	for in, want := range map[string]time.Duration{"PT45M": 45 * time.Minute, "P1D": 24 * time.Hour, "P1DT2H": 26 * time.Hour, "P2W": 14 * 24 * time.Hour, "PT1H30M15S": time.Hour + 30*time.Minute + 15*time.Second} {
		if got := parseDuration(in); got != want {
			t.Errorf("%s = %v, want %v", in, got, want)
		}
	}
}
