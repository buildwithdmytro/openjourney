package journey

import "time"

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

type FakeClock struct {
	now time.Time
}

func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

func (f *FakeClock) Now() time.Time {
	return f.now
}

func (f *FakeClock) Set(t time.Time) {
	f.now = t
}

func (f *FakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}
