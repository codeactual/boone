// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package shell_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/codeactual/boone/internal/cage/shell"
	cage_strings "github.com/codeactual/boone/internal/cage/strings"
)

func TestTable(t *testing.T) {
	cases := []struct {
		input    string
		expected [][]string
	}{
		{
			input: `go list github.com/codeactual/boone/internal/cage/... | egrep -v /exp/ | xargs go install -v`,
			expected: cage_strings.SliceOfSlice(
				[]string{
					"go", "list", "github.com/codeactual/boone/internal/cage/...",
				},
				[]string{
					"egrep", "-v", `/exp/`,
				},
				[]string{
					"xargs", "go", "install", "-v",
				},
			),
		},
		{
			input:    `grep -nr "hello world" file`,
			expected: cage_strings.SliceOfSlice([]string{"grep", "-nr", "hello world", "file"}),
		},

		// verify "|" logic doesn't loop forever
		{
			input:    ` `,
			expected: [][]string{},
		},
		{
			input:    ` `,
			expected: [][]string{},
		},
	}
	for _, c := range cases {
		actual, err := shell.Parse(c.input)
		require.NoError(t, err)
		require.Exactly(t, c.expected, actual)
	}
}
