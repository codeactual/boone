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
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/codeactual/boone/internal/boone"
	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
	testkit_file "github.com/codeactual/boone/internal/cage/testkit/os/file"
	cage_trace "github.com/codeactual/boone/internal/cage/trace"
)

var thisDir string

func init() {
	thisDir = filepath.Dir(cage_trace.ThisFile())
}

type TargetSuite struct {
	suite.Suite

	cfg boone.Config

	target0Root        string
	target1Root        string
	target2Root        string
	target3Root        string
	target3ExecDir     string
	target1IncludeRoot string
	target1ExcludeRoot string
}

func (suite *TargetSuite) SetupTest() {
	var err error

	t := suite.T()

	// e.g. for validation that target root dir exists
	testkit_file.ResetTestdata(t)
	_, suite.target0Root = testkit_file.CreateDir(t, "target", "0")
	_, suite.target1Root = testkit_file.CreateDir(t, "target", "1")
	_, suite.target2Root = testkit_file.CreateDir(t, "target", "2")
	_, suite.target3Root = testkit_file.CreateDir(t, "target", "3")
	_, suite.target1IncludeRoot = testkit_file.CreateDir(t, "target", "1", "include", "0", "root")
	_, suite.target1ExcludeRoot = testkit_file.CreateDir(t, "target", "1", "exclude", "0", "root")

	_, suite.target3ExecDir = testkit_file.CreateDir(t, "target", "3", "some", "rel", "dir")

	suite.cfg, err = boone.ReadConfigFile("./testdata/all.yaml")
	require.NoError(t, err)
}

func (suite *TargetSuite) requireHandlerExactly(expected, actual boone.Handler, baseCaseId string) {
	t := suite.T()
	handlerCaseId := fmt.Sprintf("%s handler [%s]", baseCaseId, expected.Label)
	require.Exactly(t, expected.Label, actual.Label, handlerCaseId)
	require.Exactly(t, len(expected.Exec), len(actual.Exec), handlerCaseId)
	for e, actualExec := range actual.Exec {
		execCaseId := fmt.Sprintf("%s exec %d", handlerCaseId, e)
		require.Exactly(t, expected.Exec[e].Cmd, actualExec.Cmd, execCaseId)
		require.Exactly(t, expected.Exec[e].Dir, actualExec.Dir, execCaseId)
		require.Exactly(t, expected.Exec[e].Timeout, actualExec.Timeout, execCaseId)
		expectedTimeoutDuration, err := time.ParseDuration(expected.Exec[e].Timeout)
		require.NoError(t, err)
		require.Exactly(t, expectedTimeoutDuration, actualExec.GetTimeout(), execCaseId)
	}
}

func (suite *TargetSuite) requireTargetExactly(expected, actual boone.Target) {
	t := suite.T()
	targetCaseId := fmt.Sprintf("target [%s]", expected.Label)

	require.Exactly(t, expected.Label, actual.Label, targetCaseId)
	require.Exactly(t, expected.Root, actual.Root, targetCaseId)
	require.Exactly(t, expected.Id, actual.Id, targetCaseId)

	require.Exactly(t, expected.Debounce, actual.Debounce, targetCaseId)
	expectedDebounceDuration, err := time.ParseDuration(expected.Debounce)
	require.NoError(t, err, targetCaseId)
	require.Exactly(t, expectedDebounceDuration, actual.GetDebounce(), targetCaseId)

	require.Exactly(t, expected.Upstream, actual.Upstream, targetCaseId)
	require.Exactly(t, expected.Include, actual.Include, targetCaseId)
	require.Exactly(t, expected.Exclude, actual.Exclude, targetCaseId)

	require.Exactly(t, len(expected.Handler), len(actual.Handler))
	for h, actualHandler := range actual.Handler {
		suite.requireHandlerExactly(expected.Handler[h], actualHandler, targetCaseId)
	}

	require.Exactly(t, len(expected.Tree), len(actual.Tree))
	for s, actualTarget := range actual.Tree {
		treeTargetCaseId := fmt.Sprintf("%s tree target %d", targetCaseId, s)

		require.Exactly(t, expected.Tree[s].Id, actualTarget.Id, treeTargetCaseId)
		require.Exactly(t, expected.Tree[s].Label, actualTarget.Label, treeTargetCaseId)

		require.Exactly(t, len(expected.Tree[s].Handler), len(actualTarget.Handler), treeTargetCaseId)
		for h, actualHandler := range actualTarget.Handler {
			suite.requireHandlerExactly(expected.Tree[s].Handler[h], actualHandler, treeTargetCaseId)
		}
	}
}

func (suite *TargetSuite) TestReadConfigFile() {
	t := suite.T()

	require.Exactly(
		t,
		boone.DataConfig{
			Session: boone.SessionConfig{File: "testdata/dynamic/path/to/boone/session"},
		},
		suite.cfg.Data,
	)

	require.Exactly(
		t,
		map[string]string{
			"debounce_profile": "10s",
			"custom_timeout":   "20m",
		},
		suite.cfg.Template,
	)

	expectedGlobal := boone.GlobalConfig{
		Cooldown: "10s",
		Exclude: []cage_filepath.Glob{
			{Pattern: "global/exclude/0/glob"},
			{Pattern: "global/exclude/1/glob"},
		},
	}
	require.Exactly(
		t,
		expectedGlobal.Cooldown,
		suite.cfg.Global.Cooldown,
	)
	require.Exactly(
		t,
		10*time.Second,
		suite.cfg.Global.GetCooldown(),
	)
	require.Exactly(
		t,
		expectedGlobal.Exclude,
		suite.cfg.Global.Exclude,
	)

	expectedTarget := []boone.Target{
		{
			Label:    "target 0 label",
			Root:     suite.target0Root,
			Id:       "target 0 id",
			Debounce: "10s",
			Include: []cage_filepath.Glob{
				{
					Pattern: suite.target0Root + "/include/0/glob",
					Root:    suite.target0Root,
				},
				{
					Pattern: suite.target0Root + "/include/1/glob",
					Root:    suite.target0Root,
				},
			},
			Exclude: []cage_filepath.Glob{
				{
					Pattern: suite.target0Root + "/exclude/0/glob",
					Root:    suite.target0Root,
				},
				{
					Pattern: suite.target0Root + "/exclude/1/glob",
					Root:    suite.target0Root,
				},
				{
					Pattern: suite.target0Root + "/global/exclude/0/glob",
					Root:    suite.target0Root,
				},
				{
					Pattern: suite.target0Root + "/global/exclude/1/glob",
					Root:    suite.target0Root,
				},
			},
			Handler: []boone.Handler{
				{
					Label: "target 0 handler 0 label",
					Exec: []boone.Exec{{
						Cmd:     "target 0 handler 0 cmd",
						Dir:     suite.target0Root,
						Timeout: "20m",
					}},
				},
				{
					Label: "target 0 handler 1 label",
					Exec: []boone.Exec{{
						Cmd:     "target 0 handler 1 cmd",
						Dir:     suite.target0Root,
						Timeout: "15m",
					}},
				},
			},
		},
		{
			Label:    "target 1 label",
			Root:     suite.target1Root,
			Debounce: "5s",
			Id:       "auto-generated Id: [target 1 label][" + suite.target1Root + "]",
			Include: []cage_filepath.Glob{
				{
					Pattern: suite.target1Root + "/include/0/root/include/0/glob",
					Root:    suite.target1IncludeRoot,
				},
			},
			Exclude: []cage_filepath.Glob{
				{
					Pattern: suite.target1Root + "/exclude/0/root/exclude/0/glob",
					Root:    suite.target1ExcludeRoot,
				},
				{
					Pattern: suite.target1Root + "/global/exclude/0/glob",
					Root:    suite.target1Root,
				},
				{
					Pattern: suite.target1Root + "/global/exclude/1/glob",
					Root:    suite.target1Root,
				},
			},
			Upstream: []string{"target 0 id"},
			Handler: []boone.Handler{
				{
					Label: "target 1 handler 0 label",
					Exec: []boone.Exec{{
						Cmd:     "target 1 handler 0 cmd",
						Dir:     suite.target1Root,
						Timeout: "6m",
					}},
				},
			},
			Downstream: []*boone.Target{},
		},
		{
			Label:    "target 2 label",
			Root:     suite.target2Root,
			Id:       "target 2 id",
			Debounce: "15s",
			Upstream: []string{"target 0 id"},
			Handler: []boone.Handler{
				{
					Label: "target 2 handler 0 label",
					Exec: []boone.Exec{{
						Cmd:     "target 2 handler 0 cmd",
						Dir:     suite.target2Root,
						Timeout: "15m",
					}},
				},
			},
			Exclude: []cage_filepath.Glob{
				{
					Pattern: suite.target2Root + "/global/exclude/0/glob",
					Root:    suite.target2Root,
				},
				{
					Pattern: suite.target2Root + "/global/exclude/1/glob",
					Root:    suite.target2Root,
				},
			},
			Downstream: []*boone.Target{},
		},
		{
			Label:    "target 3 label",
			Root:     suite.target3Root,
			Debounce: "15s",
			Id:       "target 3 id",
			Upstream: []string{"target 2 id"},
			Handler: []boone.Handler{
				{
					Label: "target 3 handler 0 label",
					Exec: []boone.Exec{
						{
							Cmd:     "target 3 handler 0 cmd",
							Dir:     suite.target3Root,
							Timeout: "15m",
						},
						{
							Cmd:     "target 3 handler 1 cmd",
							Dir:     suite.target3ExecDir,
							Timeout: "15m",
						},
					},
				},
			},
			Exclude: []cage_filepath.Glob{
				{
					Pattern: suite.target3Root + "/global/exclude/0/glob",
					Root:    suite.target3Root,
				},
				{
					Pattern: suite.target3Root + "/global/exclude/1/glob",
					Root:    suite.target3Root,
				},
			},
			Downstream: []*boone.Target{},
		},
	}

	expectedTarget[0].Downstream = []*boone.Target{&expectedTarget[1], &expectedTarget[2]}
	expectedTarget[2].Downstream = []*boone.Target{&expectedTarget[3]}

	expectedTarget[0].Tree = []boone.TargetTree{
		{
			Id:      expectedTarget[0].Id,
			Label:   expectedTarget[0].Label,
			Handler: expectedTarget[0].Handler,
		},
	}
	for _, d := range expectedTarget[0].Downstream {
		expectedTarget[0].Tree = append(
			expectedTarget[0].Tree,
			boone.TargetTree{
				Id:      d.Id,
				Label:   d.Label,
				Handler: d.Handler,
			},
		)
	}
	expectedTarget[0].Tree = append(
		expectedTarget[0].Tree,
		boone.TargetTree{ // transitive: downstream of downstream
			Id:      expectedTarget[3].Id,
			Label:   expectedTarget[3].Label,
			Handler: expectedTarget[3].Handler,
		},
	)
	expectedTarget[1].Tree = []boone.TargetTree{
		{
			Id:      expectedTarget[1].Id,
			Label:   expectedTarget[1].Label,
			Handler: expectedTarget[1].Handler,
		},
	}
	expectedTarget[2].Tree = []boone.TargetTree{
		{
			Id:      expectedTarget[2].Id,
			Label:   expectedTarget[2].Label,
			Handler: expectedTarget[2].Handler,
		},
	}
	for _, d := range expectedTarget[2].Downstream {
		expectedTarget[2].Tree = append(
			expectedTarget[2].Tree,
			boone.TargetTree{
				Id:      d.Id,
				Label:   d.Label,
				Handler: d.Handler,
			},
		)
	}
	expectedTarget[3].Tree = []boone.TargetTree{
		{
			Id:      expectedTarget[3].Id,
			Label:   expectedTarget[3].Label,
			Handler: expectedTarget[3].Handler,
		},
	}

	for pos := range expectedTarget {
		suite.requireTargetExactly(expectedTarget[pos], suite.cfg.Target[pos])
	}

	require.Exactly(t, []string{"target 2 id"}, suite.cfg.AutoStartTarget)
	startTarget := suite.cfg.GetStartTarget()
	require.Len(t, startTarget, 1)
	suite.requireTargetExactly(expectedTarget[2], startTarget[0])
}

func (suite *TargetSuite) TestContainsDownstream() {
	t := suite.T()

	require.False(t, suite.cfg.Target[0].ContainsDownstream(suite.cfg.Target[0].Id))
	require.True(t, suite.cfg.Target[0].ContainsDownstream(suite.cfg.Target[1].Id))
	require.True(t, suite.cfg.Target[0].ContainsDownstream(suite.cfg.Target[2].Id))
	require.True(t, suite.cfg.Target[0].ContainsDownstream(suite.cfg.Target[3].Id))

	require.False(t, suite.cfg.Target[1].ContainsDownstream(suite.cfg.Target[0].Id))
	require.False(t, suite.cfg.Target[1].ContainsDownstream(suite.cfg.Target[1].Id))
	require.False(t, suite.cfg.Target[1].ContainsDownstream(suite.cfg.Target[2].Id))
	require.False(t, suite.cfg.Target[1].ContainsDownstream(suite.cfg.Target[3].Id))

	require.False(t, suite.cfg.Target[2].ContainsDownstream(suite.cfg.Target[0].Id))
	require.False(t, suite.cfg.Target[2].ContainsDownstream(suite.cfg.Target[1].Id))
	require.False(t, suite.cfg.Target[2].ContainsDownstream(suite.cfg.Target[2].Id))
	require.True(t, suite.cfg.Target[2].ContainsDownstream(suite.cfg.Target[3].Id))

	require.False(t, suite.cfg.Target[3].ContainsDownstream(suite.cfg.Target[0].Id))
	require.False(t, suite.cfg.Target[3].ContainsDownstream(suite.cfg.Target[1].Id))
	require.False(t, suite.cfg.Target[3].ContainsDownstream(suite.cfg.Target[2].Id))
	require.False(t, suite.cfg.Target[3].ContainsDownstream(suite.cfg.Target[3].Id))
}

func (suite *TargetSuite) TestVisitDownstreamPass() {
	t := suite.T()

	actualVisited := []string{}
	visit := func(target *boone.Target) error {
		actualVisited = append(actualVisited, target.Label)
		return nil
	}

	expectedVisited := []string{"target 1 label", "target 2 label", "target 3 label"}
	err := boone.VisitDownstream(&suite.cfg.Target[0], visit)
	require.NoError(t, err)
	require.Exactly(t, expectedVisited, actualVisited)

	actualVisited = []string{}
	expectedVisited = []string{}
	err = boone.VisitDownstream(&suite.cfg.Target[1], visit)
	require.NoError(t, err)
	require.Exactly(t, expectedVisited, actualVisited)

	actualVisited = []string{}
	expectedVisited = []string{"target 3 label"}
	err = boone.VisitDownstream(&suite.cfg.Target[2], visit)
	require.NoError(t, err)
	require.Exactly(t, expectedVisited, actualVisited)

	actualVisited = []string{}
	expectedVisited = []string{}
	err = boone.VisitDownstream(&suite.cfg.Target[3], visit)
	require.NoError(t, err)
	require.Exactly(t, expectedVisited, actualVisited)
}

func (suite *TargetSuite) TestVisitDownstreamFail() {
	t := suite.T()

	expectedErr := errors.New("some error")

	actualVisited := []string{}
	visit := func(target *boone.Target) error {
		actualVisited = append(actualVisited, target.Label)
		return expectedErr
	}

	expectedVisited := []string{"target 1 label"}
	err := boone.VisitDownstream(&suite.cfg.Target[0], visit)
	require.EqualError(t, err, "some error")
	require.Exactly(t, expectedVisited, actualVisited)

	actualVisited = []string{}
	expectedVisited = []string{}
	err = boone.VisitDownstream(&suite.cfg.Target[1], visit)
	require.NoError(t, err) // visit never called
	require.Exactly(t, expectedVisited, actualVisited)

	actualVisited = []string{}
	expectedVisited = []string{"target 3 label"}
	err = boone.VisitDownstream(&suite.cfg.Target[2], visit)
	require.EqualError(t, err, "some error")
	require.Exactly(t, expectedVisited, actualVisited)

	actualVisited = []string{}
	expectedVisited = []string{}
	err = boone.VisitDownstream(&suite.cfg.Target[3], visit)
	require.NoError(t, err) // visit never called
	require.Exactly(t, expectedVisited, actualVisited)
}

func TestTargetSuite(t *testing.T) {
	suite.Run(t, new(TargetSuite))
}
