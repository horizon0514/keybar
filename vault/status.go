package vault

import (
	"fmt"
	"time"
)

// State is a key's standing against its rotation interval and expiry.
type State int

const (
	OK      State = iota
	DueSoon       // within SoonDays of being due, or of expiring
	Overdue       // past its interval, or expired
)

// SoonDays is how early a key is "due soon".
const SoonDays = 14

func (s State) String() string {
	switch s {
	case DueSoon:
		return "due soon"
	case Overdue:
		return "overdue"
	}
	return "ok"
}

// Zh is the state in Chinese.
func (s State) Zh() string {
	switch s {
	case DueSoon:
		return "快到期"
	case Overdue:
		return "已超期"
	}
	return "正常"
}

// Status is a key's standing.
type Status struct {
	State   State
	Due     time.Time // when it should be rotated; zero: never
	Expires time.Time // when it expires; zero: unknown
	Pending int       // platforms still to update after the last rotation
	Why     string    // e.g. "超期 12 天", "3 天后过期"
}

// StatusOf is the key's standing at now.
func (inv *Inventory) StatusOf(k Key, now time.Time) Status {
	st := Status{Expires: k.Expires, Pending: len(k.Pending())}
	since := k.LastRotated
	if since.IsZero() {
		since = k.Created
	}
	if every := inv.RotateEvery(k); every > 0 && !since.IsZero() {
		st.Due = since.Add(every)
	}
	soon := now.Add(SoonDays * 24 * time.Hour)
	worst := func(s State, why string) {
		if s > st.State || st.Why == "" && s == st.State && s != OK {
			st.State, st.Why = s, why
		}
	}
	if !st.Expires.IsZero() {
		switch {
		case !now.Before(st.Expires):
			worst(Overdue, "已过期 "+Days(now.Sub(st.Expires)))
		case st.Expires.Before(soon):
			worst(DueSoon, Days(st.Expires.Sub(now))+"后过期")
		}
	}
	if !st.Due.IsZero() {
		switch {
		case !now.Before(st.Due):
			worst(Overdue, "超期 "+Days(now.Sub(st.Due)))
		case st.Due.Before(soon):
			worst(DueSoon, Days(st.Due.Sub(now))+"后该轮换")
		}
	}
	return st
}

// Days is a duration in days, at least one: "3 天".
func Days(d time.Duration) string {
	n := int(d.Hours() / 24)
	if n < 1 {
		return "不到 1 天"
	}
	return fmt.Sprintf("%d 天", n)
}

// Ago is how long ago t was: "今天", "3 天前".
func Ago(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	n := int(now.Sub(t).Hours() / 24)
	switch {
	case n <= 0:
		return "今天"
	case n == 1:
		return "昨天"
	}
	return fmt.Sprintf("%d 天前", n)
}
