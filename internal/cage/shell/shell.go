// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package shell

import (
	shellwords "github.com/mattn/go-shellwords"
	"github.com/pkg/errors"
)

// Parse returns a slice of argument slices, one argument slice per pipeline process/stage.
func Parse(s string) (args [][]string, err error) {
	parser := shellwords.NewParser()
	parser.ParseEnv = true // use os.GetEnv to expand variables

	args = [][]string{} // simplify test expectations, e.g. no nil slices expected

	// Use recommended approach https://github.com/mattn/go-shellwords/issues/4#issuecomment-275275660
	// to support "|".
	for {
		parsed, err := parser.Parse(s)
		if err != nil {
			return [][]string{}, errors.Wrapf(err, "failed to parse [%s]", s)
		}

		if len(parsed) == 0 {
			break
		}

		args = append(args, parsed)

		if parser.Position == -1 {
			break
		}

		if s[parser.Position] == '|' {
			parser.Position++
			s = s[parser.Position:]
		}
	}

	return args, nil
}
