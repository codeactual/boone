// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package time

import (
	std_time "time"
)

type Timer interface {
	Reset(std_time.Duration) bool
	Stop() bool
	C() <-chan std_time.Time
}

type RealTimer struct {
	t *std_time.Timer
}

func (f *RealTimer) Reset(d std_time.Duration) bool {
	return f.t.Reset(d)
}

func (f *RealTimer) Stop() bool {
	return f.t.Stop()
}

func (f *RealTimer) C() <-chan std_time.Time {
	return f.t.C
}

var _ Timer = (*RealTimer)(nil)
