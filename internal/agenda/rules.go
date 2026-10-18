// Package agenda holds the pure scheduling rules of the appointments module
// (deadlines, free slots). It only depends on the standard library so it can
// be unit-tested without a database. All times are interpreted in whatever
// location the caller passes in (the app always uses Colombia's UTC-5).
package agenda

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// ShortWindow is how long the business has to answer a request for an
	// appointment that starts in less than two hours.
	ShortWindow = 30 * time.Minute
	// SameDayWindow is the answer window for an appointment later today.
	SameDayWindow = 2 * time.Hour
	// LeadBeforeStart is how long before an other-day appointment the request expires.
	LeadBeforeStart = 2 * time.Hour
	// MaxWait is the longest a customer ever waits for an answer.
	MaxWait = 24 * time.Hour
	// MinWindow is the shortest answer window ever given.
	MinWindow = 15 * time.Minute
	// AfterOpeningGrace is added to the next opening when a deadline falls
	// outside business hours.
	AfterOpeningGrace = 2 * time.Hour
	// FirstSlotsBlocked: while the business is closed, appointments starting in
	// this window right after opening cannot be requested (they would expire
	// at the very moment the business opens).
	FirstSlotsBlocked = 30 * time.Minute
	// MinNoticeFloor is the lowest allowed min_notice_min (15 to answer + 30 to arrive).
	MinNoticeFloor = 45
	// SilenceStartHour / SilenceEndHour: no deadline reminders in between.
	SilenceStartHour = 21
	SilenceEndHour   = 7
)

// Hours is one weekday of a business's weekly schedule (Go weekday numbering:
// 0 = Sunday). Open/Close are "HH:MM"; Close <= Open means it crosses midnight.
type Hours struct {
	Weekday int
	Closed  bool
	Is24h   bool
	Open    string
	Close   string
}

// Interval is a half-open time range [Start, End).
type Interval struct {
	Start time.Time
	End   time.Time
}

// Overlaps reports whether two half-open intervals intersect.
func (a Interval) Overlaps(b Interval) bool {
	return a.Start.Before(b.End) && b.Start.Before(a.End)
}

func parseClock(s string) (time.Duration, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, true
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.In(a.Location()).Date()
	return ay == by && am == bm && ad == bd
}

// WindowOn returns the opening window that STARTS on the calendar day of
// `day` (a window that crosses midnight ends the next day). ok is false when
// the business does not open that day.
func WindowOn(hours []Hours, day time.Time) (open, close time.Time, ok bool) {
	base := startOfDay(day)
	wd := int(base.Weekday())
	for _, h := range hours {
		if h.Weekday != wd {
			continue
		}
		if h.Closed {
			return time.Time{}, time.Time{}, false
		}
		if h.Is24h {
			return base, base.AddDate(0, 0, 1), true
		}
		o, ok1 := parseClock(h.Open)
		c, ok2 := parseClock(h.Close)
		if !ok1 || !ok2 {
			return time.Time{}, time.Time{}, false
		}
		open = base.Add(o)
		close = base.Add(c)
		if c <= o {
			close = startOfDay(base.AddDate(0, 0, 1)).Add(c)
		}
		return open, close, true
	}
	return time.Time{}, time.Time{}, false
}

// IsOpenAt reports whether the business is open at instant t, including a
// window from the previous day that is still running past midnight.
func IsOpenAt(hours []Hours, t time.Time) bool {
	for _, d := range []time.Time{t.AddDate(0, 0, -1), t} {
		if o, c, ok := WindowOn(hours, d); ok && !t.Before(o) && t.Before(c) {
			return true
		}
	}
	return false
}

// NextOpening returns the first opening strictly after `after`.
func NextOpening(hours []Hours, after time.Time) (time.Time, bool) {
	for i := 0; i <= 8; i++ {
		if o, _, ok := WindowOn(hours, after.AddDate(0, 0, i)); ok && o.After(after) {
			return o, true
		}
	}
	return time.Time{}, false
}

// ComputeExpiry returns when a new appointment request expires.
//
//   - starts in under 2h        -> now + 30 min
//   - later today               -> now + 2h
//   - another calendar day      -> min( max(start-2h, opening of that day), now+24h, start ),
//     and, if the 24h cap is what decided it and it lands while the business
//     is closed, the next opening + 2h.
//
// Always at least now + 15 min and never after the start.
func ComputeExpiry(now, start time.Time, hours []Hours) time.Time {
	now = now.In(start.Location())
	var e time.Time
	switch {
	case start.Sub(now) < 2*time.Hour:
		e = now.Add(ShortWindow)
	case sameDay(now, start):
		e = now.Add(SameDayWindow)
	default:
		e = start.Add(-LeadBeforeStart)
		if open, _, ok := WindowOn(hours, start); ok && e.Before(open) {
			e = open
		}
		if cap := now.Add(MaxWait); cap.Before(e) {
			e = cap
			if len(hours) > 0 && !IsOpenAt(hours, e) {
				if next, ok := NextOpening(hours, e); ok {
					e = next.Add(AfterOpeningGrace)
				}
			}
		}
	}
	if e.After(start) {
		e = start
	}
	if floor := now.Add(MinWindow); e.Before(floor) {
		e = floor
	}
	if e.After(start) {
		e = start
	}
	return e
}

// HalfwayPoint is when the "mid-window" reminder should go out. For long
// windows it is capped at 12 hours after the request.
func HalfwayPoint(created, expires time.Time) time.Time {
	half := expires.Sub(created) / 2
	if half > 12*time.Hour {
		half = 12 * time.Hour
	}
	return created.Add(half)
}

// InSilence reports whether t falls in the quiet hours (21:00–07:00) when
// deadline reminders are held back.
func InSilence(t time.Time) bool {
	h := t.Hour()
	return h >= SilenceStartHour || h < SilenceEndHour
}

// SlotParams describes everything needed to compute the free start times of one day.
type SlotParams struct {
	Day          time.Time
	Now          time.Time
	Hours        []Hours
	SlotMinutes  int
	DurationMin  int
	BufferMin    int
	LunchStart   string // "HH:MM" or ""
	LunchEnd     string
	MinNoticeMin int
	HorizonDays  int
	// Busy are the occupied ranges [starts_at, ends_at+buffer) of active rows.
	Busy []Interval
}

// FreeSlots returns the start times a customer can request on p.Day, ascending.
func FreeSlots(p SlotParams) []time.Time {
	open, close, ok := WindowOn(p.Hours, p.Day)
	if !ok || p.SlotMinutes <= 0 || p.DurationMin <= 0 {
		return nil
	}
	loc := p.Day.Location()
	now := p.Now.In(loc)
	notice := p.MinNoticeMin
	if notice < MinNoticeFloor {
		notice = MinNoticeFloor
	}
	earliest := now.Add(time.Duration(notice) * time.Minute)
	latest := now.AddDate(0, 0, p.HorizonDays)
	closedNow := !IsOpenAt(p.Hours, now)

	var lunch *Interval
	if ls, ok1 := parseClock(p.LunchStart); ok1 {
		if le, ok2 := parseClock(p.LunchEnd); ok2 && le > ls {
			base := startOfDay(p.Day)
			lunch = &Interval{Start: base.Add(ls), End: base.Add(le)}
		}
	}

	step := time.Duration(p.SlotMinutes) * time.Minute
	dur := time.Duration(p.DurationMin) * time.Minute
	buf := time.Duration(p.BufferMin) * time.Minute
	var out []time.Time
	for s := open; !s.Add(dur).After(close); s = s.Add(step) {
		if s.Before(earliest) || s.After(latest) {
			continue
		}
		if closedNow && s.Before(open.Add(FirstSlotsBlocked)) {
			continue
		}
		occupied := Interval{Start: s, End: s.Add(dur + buf)}
		if lunch != nil && occupied.Start.Before(lunch.End) && lunch.Start.Before(s.Add(dur)) {
			continue
		}
		clash := false
		for _, b := range p.Busy {
			if occupied.Overlaps(b) {
				clash = true
				break
			}
		}
		if !clash {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// Closest returns up to n slots nearest to target, ordered by distance.
func Closest(slots []time.Time, target time.Time, n int) []time.Time {
	cp := append([]time.Time(nil), slots...)
	abs := func(d time.Duration) time.Duration {
		if d < 0 {
			return -d
		}
		return d
	}
	sort.Slice(cp, func(i, j int) bool { return abs(cp[i].Sub(target)) < abs(cp[j].Sub(target)) })
	if len(cp) > n {
		cp = cp[:n]
	}
	return cp
}

// Outcome decides the final status of a confirmed appointment from the two
// attendance answers (nil = no answer). final is true when the deadline passed.
// Returns "" while it is still waiting for an answer.
func Outcome(customer, business *bool, final bool) string {
	switch {
	case customer != nil && business != nil:
		if *customer == *business {
			if *customer {
				return "completed"
			}
			return "no_show"
		}
		return "disputed"
	case !final:
		return ""
	case customer != nil:
		if *customer {
			return "completed"
		}
		return "no_show"
	case business != nil:
		if *business {
			return "completed"
		}
		return "no_show"
	default:
		return "completed"
	}
}
