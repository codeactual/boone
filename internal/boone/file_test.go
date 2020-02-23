// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/codeactual/boone/internal/boone"
	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
	cage_strings "github.com/codeactual/boone/internal/cage/strings"
	testkit_file "github.com/codeactual/boone/internal/cage/testkit/os/file"
	testkit_filepath "github.com/codeactual/boone/internal/cage/testkit/path/filepath"
)

type FileSuite struct {
	suite.Suite

	absPath1 string
	absPath2 string
	absPath3 string
	absPath4 string

	ancestorRoot string

	globalExcludeGlob1 string
	globalExcludeGlob2 string
}

func (suite *FileSuite) SetupTest() {
	t := suite.T()

	testkit_file.ResetTestdata(t)

	_, suite.absPath1 = testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "main.go")
	_, suite.absPath2 = testkit_file.CreateFile(t, "path", "to", "proj", "file.go")
	_, suite.absPath3 = testkit_file.CreateFile(t, "path", "to", "proj", "ci")
	_, suite.absPath4 = testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "mocks", "all.go")
	_, _ = testkit_file.CreateFile(t, "path", "to", "proj", "README.md")
	_, _ = testkit_file.CreateFile(t, "path", "to", "proj", "LICENSE")

	suite.ancestorRoot = testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"))

	suite.globalExcludeGlob1 = "**/mocks"
	suite.globalExcludeGlob2 = "**/mocks/**/*.go"
}

func (suite *FileSuite) requireGetTargetGlobExactly(table map[*boone.Target][]cage_filepath.GlobAnyOutput) {
	t := suite.T()

	for target, expectedGlobOuts := range table {
		actualGlobOuts, err := boone.GetTargetGlob(target.Include, target.Exclude)
		require.NoError(t, err)

		require.Exactly(t, len(expectedGlobOuts), len(actualGlobOuts))

		for outIdx, globOut := range expectedGlobOuts {
			failMsg := fmt.Sprintf("GlobAnyOutput[%d]", outIdx)
			require.Exactly(t, len(expectedGlobOuts[outIdx].Include), len(actualGlobOuts[outIdx].Include), failMsg)
			require.Exactly(t, len(expectedGlobOuts[outIdx].Exclude), len(actualGlobOuts[outIdx].Exclude), failMsg)

			includePaths := cage_strings.NewSet()
			for p := range globOut.Include {
				includePaths.Add(p)
			}
			for _, p := range includePaths.SortedSlice() {
				failMsg = fmt.Sprintf("GlobAnyOutput[%d].Include[%s]", outIdx, p)
				require.Exactly(t, expectedGlobOuts[outIdx].Include[p], actualGlobOuts[outIdx].Include[p], failMsg)
			}

			excludePaths := cage_strings.NewSet()
			for p := range globOut.Exclude {
				excludePaths.Add(p)
			}
			for _, p := range excludePaths.SortedSlice() {
				failMsg = fmt.Sprintf("GlobAnyOutput[%d].Exclude[%s]", outIdx, p)
				require.Exactly(t, expectedGlobOuts[outIdx].Exclude[p], actualGlobOuts[outIdx].Exclude[p], failMsg)
			}
		}
	}
}

func (suite *FileSuite) TestGetTargetGlob() {
	t := suite.T()
	var target *boone.Target

	table := make(map[*boone.Target][]cage_filepath.GlobAnyOutput)

	// case: include main.go and file.go (exclude nothing), verify ancestors included
	includeGlob := filepath.Join("**", "*.go")
	target = &boone.Target{
		Label: "some target",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{Pattern: includeGlob},
		},
		Exclude: []cage_filepath.Glob{
			{Pattern: suite.globalExcludeGlob1},
			{Pattern: suite.globalExcludeGlob2},
		},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "exit 0"}}},
		},
	}
	require.NoError(t, boone.FinalizeConfig([]*boone.Target{target}, &boone.Config{}))
	include := cage_filepath.Glob{
		Pattern: filepath.Join(target.Include[0].Root, includeGlob),
		Root:    target.Include[0].Root,
	}
	table[target] = []cage_filepath.GlobAnyOutput{
		{
			Include: map[string]cage_filepath.Glob{
				suite.absPath1: include,
				suite.absPath2: include,
				testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd")):         include,
				testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd", "proj")): include,
				suite.ancestorRoot: include,
			},
			Exclude: map[string]cage_filepath.Glob{
				suite.absPath4: {
					Pattern: filepath.Join(target.Include[0].Root, "**", "mocks", "**", "*.go"),
					Root:    target.Include[0].Root, // FinalizeConfig set Root because the input value was empty
				},
			},
		},
	}

	suite.requireGetTargetGlobExactly(table)
}

func (suite *FileSuite) TestGetTargetGlobExclude() {
	t := suite.T()
	var target *boone.Target

	table := make(map[*boone.Target][]cage_filepath.GlobAnyOutput)

	// Define target configuration.
	//
	// - include main.go
	// - exclude file.go
	// - verify ancestors included

	includeGlob1 := filepath.Join("**", "*.go")
	includeGlob2 := "*.go"
	exclude := "file.*"
	target = &boone.Target{
		Label: "some target",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{Pattern: includeGlob1},
			{Pattern: includeGlob2},
		},
		Exclude: []cage_filepath.Glob{
			{Pattern: exclude},
			{Pattern: suite.globalExcludeGlob1},
			{Pattern: suite.globalExcludeGlob2},
		},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "exit 0"}}},
		},
	}

	// Perform the same validation/finalization as the CLI.

	require.NoError(t, boone.FinalizeConfig([]*boone.Target{target}, &boone.Config{}))

	// Assert expected per-target output.

	expectedInclude := cage_filepath.Glob{
		Pattern: filepath.Join(target.Include[0].Root, includeGlob1),
		Root:    target.Include[0].Root,
	}

	table[target] = []cage_filepath.GlobAnyOutput{
		{
			Include: map[string]cage_filepath.Glob{
				suite.absPath1: expectedInclude,
				testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd")):         expectedInclude,
				testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd", "proj")): expectedInclude,
				suite.ancestorRoot: expectedInclude,
			},
			Exclude: map[string]cage_filepath.Glob{
				suite.absPath2: {
					Pattern: filepath.Join(target.Include[0].Root, exclude),
					Root:    target.Include[0].Root, // FinalizeConfig set Root because the input value was empty
				},
				suite.absPath4: {
					Pattern: filepath.Join(target.Include[0].Root, "**", "mocks", "**", "*.go"),
					Root:    target.Include[0].Root, // FinalizeConfig set Root because the input value was empty
				},
			},
		},
		{
			Include: map[string]cage_filepath.Glob{},
			Exclude: map[string]cage_filepath.Glob{
				suite.absPath2: {
					Pattern: filepath.Join(target.Include[0].Root, exclude),
					Root:    target.Include[0].Root, // FinalizeConfig set Root because the input value was empty
				},
			},
		},
	}

	suite.requireGetTargetGlobExactly(table)
}

func (suite *FileSuite) TestGetGlobIncludeInclude() {
	t := suite.T()
	var target *boone.Target

	table := make(map[*boone.Target]map[string]cage_filepath.Glob)

	// Define target configuration.

	target = &boone.Target{
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Label: "some target",
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
		Exclude: []cage_filepath.Glob{
			{Pattern: suite.globalExcludeGlob1},
			{Pattern: suite.globalExcludeGlob2},
		},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "exit 0"}}},
		},
	}

	// Perform the same validation/finalization as the CLI.

	require.NoError(t, boone.FinalizeConfig([]*boone.Target{target}, &boone.Config{}))

	// Assert expected per-target output.

	include := cage_filepath.Glob{
		Pattern: target.Include[0].Pattern,
		Root:    target.Include[0].Root,
	}
	table[target] = map[string]cage_filepath.Glob{
		// pattern matches
		suite.absPath1: include,
		suite.absPath2: include,

		// ancestors
		testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd")):         include,
		testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd", "proj")): include,

		// ancestor root itself
		suite.ancestorRoot: include,
	}

	for target, expectedPaths := range table {
		globs, err := boone.GetTargetGlob(target.Include, target.Exclude)
		require.NoError(t, err)

		actualPaths, err := boone.GetGlobInclude(globs)
		require.NoError(t, err)
		require.Exactly(t, expectedPaths, actualPaths)
	}
}

func (suite *FileSuite) TestGetGlobIncludeExclude() {
	t := suite.T()
	var target *boone.Target

	table := make(map[*boone.Target]map[string]cage_filepath.Glob)

	// Define target configuration.

	target = &boone.Target{
		Label: "some target",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{Pattern: "*.go"},
			{Pattern: filepath.Join("**", "*.go")},
		},
		Exclude: []cage_filepath.Glob{
			{Pattern: "file.*"},
			{Pattern: suite.globalExcludeGlob1},
			{Pattern: suite.globalExcludeGlob2},
		},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "exit 0"}}},
		},
	}

	// Perform the same validation/finalization as the CLI.

	require.NoError(t, boone.FinalizeConfig([]*boone.Target{target}, &boone.Config{}))

	// Assert expected per-target output.

	expectedInclude := cage_filepath.Glob{
		Pattern: target.Include[1].Pattern,
		Root:    target.Include[1].Root,
	}
	table[target] = map[string]cage_filepath.Glob{
		// pattern matches: file.go excluded
		suite.absPath1: expectedInclude,

		// ancestors
		testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd")):         expectedInclude,
		testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd", "proj")): expectedInclude,

		// ancestor root itself
		suite.ancestorRoot: expectedInclude,
	}

	for target, expectedPaths := range table {
		globs, err := boone.GetTargetGlob(target.Include, target.Exclude)
		require.NoError(t, err)

		actualPaths, err := boone.GetGlobInclude(globs)
		require.NoError(t, err)
		require.Exactly(t, expectedPaths, actualPaths)
	}
}

func (suite *FileSuite) TestGetGlobIncludeUnique() {
	t := suite.T()
	var target *boone.Target

	table := make(map[*boone.Target]map[string]cage_filepath.Glob)

	// Define target configuration.

	target = &boone.Target{
		Label: "some target",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{Pattern: filepath.Join("**", "*.go")},

			// overlaps with the preceding Include: main.go, cmd/, and cmd/proj/ would appear twice
			{Pattern: filepath.Join("**", "main*")},
		},
		Exclude: []cage_filepath.Glob{
			{Pattern: suite.globalExcludeGlob1},
			{Pattern: suite.globalExcludeGlob2},
		},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "exit 0"}}},
		},
	}

	// Perform the same validation/finalization as the CLI.

	require.NoError(t, boone.FinalizeConfig([]*boone.Target{target}, &boone.Config{}))

	// Assert expected per-target output.

	expectedInclude := cage_filepath.Glob{
		Pattern: target.Include[0].Pattern,
		Root:    target.Include[0].Root,
	}
	table[target] = map[string]cage_filepath.Glob{
		// pattern matches
		suite.absPath1: expectedInclude,
		suite.absPath2: expectedInclude,

		// ancestors
		testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd")):         expectedInclude,
		testkit_filepath.Abs(t, filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj", "cmd", "proj")): expectedInclude,

		// ancestor root itself
		suite.ancestorRoot: expectedInclude,
	}

	for target, expectedPaths := range table {
		globs, err := boone.GetTargetGlob(target.Include, target.Exclude)
		require.NoError(t, err)

		actualPaths, err := boone.GetGlobInclude(globs)
		require.NoError(t, err)
		require.Exactly(t, expectedPaths, actualPaths)
	}
}

func TestFileSuite(t *testing.T) {
	suite.Run(t, new(FileSuite))
}
