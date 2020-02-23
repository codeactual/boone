// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package time

import (
	"time"

	"github.com/stretchr/testify/mock"

	cage_time_mocks "github.com/codeactual/boone/internal/cage/time/mocks"
)

// NewTimer returns a mock timer and a mock clock configured to provide it.
func NewTimer() (*cage_time_mocks.Timer, *cage_time_mocks.Clock) {
	timer := new(cage_time_mocks.Timer)
	clock := new(cage_time_mocks.Clock)
	clock.On("NewTimer", mock.AnythingOfType("time.Duration")).Return(timer)
	return timer, clock
}

// RWChanToROChan converts a bi-directional channel to a read-only one.
func RWChanToROChan(rw chan time.Time) <-chan time.Time {
	return rw
}

type DebounceTimerOption struct {
	ResetReturnTrue bool
}

// NewDebounceTimer expands on NewTimer by providing a channel to which tests can write
// in order to simulate a timer expiration.
func NewDebounceTimer(o *DebounceTimerOption) (*cage_time_mocks.Timer, *cage_time_mocks.Clock, chan time.Time, <-chan time.Time) {
	timer, clock := NewTimer()
	timer.On("Stop").Return(true)

	if o != nil {
		if o.ResetReturnTrue {
			timer.On("Reset", mock.AnythingOfType("time.Duration")).Return(true)
		}
	}

	// Create a channel that is a read-only "copy" of a another bi-directional one.
	// We'll use the read-only for the mock Timer to emit inside Debounce, while we can
	// write to bi-directional one in the test, effectively controlling the read-only
	// channel "remotely" at this layer.
	ch := make(chan time.Time, 1)

	return timer, clock, ch, RWChanToROChan(ch)
}
