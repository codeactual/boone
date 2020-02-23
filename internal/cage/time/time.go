// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package time

import (
	std_time "time"

	"github.com/hako/durafmt"
)

func DurationShort(d std_time.Duration) string {
	// Workaround for durafmt panic caused by lack of support for microseconds, e.g. if an error
	// causes a timed operation to exit early.
	if d < std_time.Millisecond {
		d = 0
	}
	return durafmt.ParseShort(d).String()
}
