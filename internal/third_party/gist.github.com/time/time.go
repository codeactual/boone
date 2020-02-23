package time

import (
	std_time "time"

	cage_time "github.com/codeactual/boone/internal/cage/time"
)

// Debounce returns a debounced version of the input function.
//
// One goroutine with a for-select loop is created for each returned function, RF, to communicate with.
// When RF is called, all it does is send the input data to the for-select in the goroutine. If a
// RF-stateful timer is nil, a timer is created with the debounce interval. But if the timer is non-nil,
// it is reset with the debounce interval. So if the RF is called twice within a total of two seconds,
// and the debounce interval is three seconds, the first RF call creates/starts the timer and the second
// call resets the timer. If the RF is no longer called and the timer is allowed to finish, the input
// function is finally invoked and the timer is set back to nil.
//
// Origin:
//   https://gist.github.com/leolara/d62b87797b0ef5e418cd#gistcomment-2243168
//   https://gist.github.com/alcore
//
// Changes:
//   - Provide interface{} argument as optional approach to link an invocation to an attempted value.
//   - Resolve timer data race.
//   - Inject a mockable clock.
//   - Add test.
func Debounce(clock cage_time.Clock, interval std_time.Duration, f func(interface{})) func(interface{}) {
	// Part of the data race fix: only one goroutine has access to the timer.
	timerEval := make(chan interface{}, 1)

	go func() {
		var timer cage_time.Timer // after expired, f may finally run

		timerClear := make(chan struct{}, 1)

		for {
			select {
			// In the original version, these timer writes happened in the same goroutine below that
			// executes the input function and created a potential data race.
			case <-timerClear:
				timer.Stop()
				timer = nil // so the next operation attempt will create a new timer
			case v := <-timerEval:
				if timer == nil {
					timer = clock.NewTimer(interval)

					// Run in a separate goroutine so if the timer expires (operation attempts have
					// "settled") we can finally execute the input function for the caller.
					go func(v interface{}) {
						<-timer.C() // block until time expires
						timerClear <- struct{}{}
						f(v)
					}(v)
				} else {
					// It's the 2nd+ time this function has been called and the timer has
					// not yet expired (which would have nil-ed the timer). Extend the wait time
					// because the operation attempts have not yet "settled."
					timer.Reset(interval)
				}
			}
		}
	}()

	return func(v interface{}) {
		// In the original version, this function executed the same steps as inside the goroutine above
		// and exposed a potential data race.
		timerEval <- v
	}
}
