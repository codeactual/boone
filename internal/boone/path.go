// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone

import (
	"path/filepath"

	"github.com/pkg/errors"

	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
)

// GetTargetGlob searches for files which match at least one Include or one Exclude pattern.
//
// It returns one GlobAnyOutput per input inclusion Glob. GlobAnyOutput.Include holds the concrete
// paths which matched at least one inclusion pattern and no exclusion pattern. GlobAnyOutput.Exclude
// holds concrete paths which matched at least one inclusion pattern but was rejected because it
// matched at least one exclusion pattern.
func GetTargetGlob(include []cage_filepath.Glob, exclude []cage_filepath.Glob) (list []cage_filepath.GlobAnyOutput, err error) {
	for _, i := range include {
		i.Root, err = filepath.Abs(i.Root)
		if err != nil {
			return []cage_filepath.GlobAnyOutput{}, errors.Wrapf(err, "failed to get absolute path of root [%s]", i.Root)
		}

		globIn := cage_filepath.GlobAnyInput{
			Exclude: exclude,

			// We cannot pass all include patterns at once because each Root is associated with one pattern.
			// For example, this allows one Target to include globs for multiple git repos which may not share a
			// common ancestor that's desirable as a watch target (e.g. too top-level, close to /). Per-pattern
			// ancestor roots allow closer-fitting watch targets.
			Include: []cage_filepath.Glob{
				{
					Pattern: i.Pattern,
					Root:    i.Root,
				},
			},
		}

		globOut, err := cage_filepath.GlobAny(globIn)
		if err != nil {
			return []cage_filepath.GlobAnyOutput{}, errors.Wrapf(err, "failed to process glob [%s]", i.Pattern)
		}

		// Also include directories that are indirect targets because they contain at least one direct
		// target, i.e. glob match, as a descendant.
		//
		// This covers the case where an ancestor directory itself did not match a glob, but a file may be created
		// inside it (at some depth) which will match the glob. We include ancestor directories in order to catch
		// file/dir creation events in case one finally matches.
		//
		// For example, we may need to monitor all directories named "X" based a convention, but they may be created
		// at unpredictable locations in the watched tree(s). We need to monitor directories named "X" that already
		// exist, but also those that may be created in the future.
		for name, include := range globOut.Include {
			ancestor, ancestorErr := cage_filepath.FileAncestor(name, include.Root)
			if ancestorErr != nil {
				return []cage_filepath.GlobAnyOutput{}, errors.Wrapf(ancestorErr, "failed to find ancestors of [%s] under [%s]", name, include.Root)
			}
			for _, a := range ancestor {
				globOut.Include[a] = include
			}
		}

		// Even if there were no matches, e.g. the root is currently empty, include the root itself
		// because we assume it may host file creations.
		if len(globOut.Include) == 0 && len(globOut.Exclude) == 0 {
			for _, include := range globIn.Include {
				globOut.Include[i.Root] = include
			}
		}

		list = append(list, globOut)
	}

	return list, nil
}

// GetGlobInclude extracts all included file/directory paths from the GlobAny outputs.
//
// If a single path was covered by multiple globs, the first encountered Include will be output.
func GetGlobInclude(globs []cage_filepath.GlobAnyOutput) (paths map[string]cage_filepath.Glob, err error) {
	paths = make(map[string]cage_filepath.Glob)

	for _, glob := range globs {
		for p, include := range glob.Include {
			_, found := paths[p]
			if found {
				continue
			}
			paths[p] = include
		}
	}

	return paths, err
}
