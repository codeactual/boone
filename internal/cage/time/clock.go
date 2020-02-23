// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

//go:generate mockery -all
package time

import (
	std_time "time"
)

type Clock interface {
	Now() std_time.Time
	NewTimer(std_time.Duration) Timer
}

type RealClock struct{}

// Now returns the current UTC time.Time (unlike the standard lib which returns local).
func (r RealClock) Now() std_time.Time {
	return std_time.Now().UTC()
}

func (r RealClock) NewTimer(d std_time.Duration) Timer {
	return &RealTimer{t: std_time.NewTimer(d)}
}

var _ Clock = (*RealClock)(nil)
