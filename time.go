package berghain

import (
	"sync/atomic"
	realTime "time"
)

// timeCache stores a pointer to a time instance
// that is accurate to about one second.
type timeCache struct {
	p atomic.Pointer[realTime.Time]
}

func (tc *timeCache) Now() realTime.Time {
	return *tc.p.Load()
}

var tc = func() (tc timeCache) {
	refresh := func() {
		n := realTime.Now()
		tc.p.Store(&n)
	}

	refresher := func() {
		for {
			refresh()
			realTime.Sleep(realTime.Second)
		}
	}

	refresh()
	go refresher()
	return
}()
