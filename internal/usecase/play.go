package usecase

import "time"

// HoldSpeech keeps the assistant speaking while a reply is still playing on the column.
// A media hold that is waiting to resume is pushed past that same playback.
func (u *Utterance) HoldSpeech(d time.Duration) {
	if u == nil || d <= 0 {
		return
	}
	until := time.Now().Add(d)
	u.speechMu.Lock()
	if until.After(u.speechUntil) {
		u.speechUntil = until
	}
	u.speechMu.Unlock()
	u.extendMediaHold(d)
}

func (u *Utterance) beginSpeech() int {
	u.speechMu.Lock()
	defer u.speechMu.Unlock()
	u.speechBusy++
	return u.speechGen
}

func (u *Utterance) endSpeech() {
	u.speechMu.Lock()
	if u.speechBusy > 0 {
		u.speechBusy--
	}
	u.speechMu.Unlock()
}

func (u *Utterance) speaking() bool {
	u.speechMu.Lock()
	defer u.speechMu.Unlock()
	return u.speechBusy > 0 || time.Now().Before(u.speechUntil)
}

// suppressEcho is true while a reply is being synthesized or is still on the
// speaker. The last two seconds of the hold are the quiet tail: a phrase then
// is the next turn. Stop is decided before this and still aborts playback.
func (u *Utterance) suppressEcho() bool {
	u.speechMu.Lock()
	defer u.speechMu.Unlock()
	if u.speechBusy > 0 {
		return true
	}
	if u.speechUntil.IsZero() {
		return false
	}
	return time.Now().Before(u.speechUntil.Add(-2 * time.Second))
}

func (u *Utterance) speechStale(gen int) bool {
	u.speechMu.Lock()
	defer u.speechMu.Unlock()
	return gen != u.speechGen
}

func (u *Utterance) clearSpeech() {
	u.speechMu.Lock()
	u.speechGen++
	u.speechBusy = 0
	u.speechUntil = time.Time{}
	u.speechMu.Unlock()
	u.memMu.Lock()
	u.spoken = ""
	u.memMu.Unlock()
}
