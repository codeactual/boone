// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package require

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/davecgh/go-spew/spew"
	std_require "github.com/stretchr/testify/require"

	cage_strings "github.com/codeactual/boone/internal/cage/strings"
)

func StringSortedSliceExactly(t *testing.T, expected []string, actual []string) {
	e := make([]string, len(expected))
	copy(e, expected[:])
	cage_strings.SortStable(e)

	a := make([]string, len(actual))
	copy(a, actual[:])
	cage_strings.SortStable(a)

	StringSliceExactly(t, e, a)
}

func StringSliceExactly(t *testing.T, expected []string, actual []string) {
	std_require.Exactly(t, expected, actual, fmt.Sprintf(
		"expect: %s\nactual: %s\n", spew.Sdump(expected), spew.Sdump(actual),
	))
}

func MatchRegexp(t *testing.T, subject string, expectedReStr ...string) {
	for _, reStr := range expectedReStr {
		std_require.True(
			t,
			regexp.MustCompile(reStr).MatchString(subject),
			fmt.Sprintf("subject [%s]\nregexp [%s]", subject, reStr),
		)
	}
}
