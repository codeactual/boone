package time

import (
	"math"
	std_time "time"
)

// SleepForever sleeps for about 300 years and avoids "fatal error: all goroutines are asleep - deadlock!"
// detection that other solutions do not
//
// Origin:
//   https://blog.sgmansfield.com/2016/06/how-to-block-forever-in-go/
func SleepForever() {
	<-std_time.After(std_time.Duration(math.MaxInt64))
}
