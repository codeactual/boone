// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package time_test

import (
	"testing"
	"time"

	cage_time "github.com/codeactual/boone/internal/cage/time"
)

type FixedClock struct {
	cage_time.RealClock // implement non-SUT behaviors to satisfy Clock interface

	Month                time.Month
	Year, Day, Hour, Min int
}

func (f FixedClock) Now() time.Time {
	return time.Date(f.Year, f.Month, f.Day, f.Hour, f.Min, 0, 0, time.UTC)
}

func TestDatetime(t *testing.T) {
	var c FixedClock
	var expected string
	var actual string

	c = FixedClock{Year: 2015, Month: 1, Day: 2, Hour: 3, Min: 4}
	expected = "20150102-0304"
	actual = cage_time.Datetime(c)
	if expected != actual {
		t.Errorf("expected %s, got %s", expected, actual)
	}

	c = FixedClock{Year: 2015, Month: 11, Day: 12, Hour: 13, Min: 14}
	expected = "20151112-1314"
	actual = cage_time.Datetime(c)
	if expected != actual {
		t.Errorf("expected %s, got %s", expected, actual)
	}
}
