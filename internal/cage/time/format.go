// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package time

import "fmt"

// Datetime returns the UTC date+time in format YYYYMMDD-HHMM.
func Datetime(c Clock) string {
	t := c.Now()
	return fmt.Sprintf("%d%02d%02d-%02d%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute())
}
