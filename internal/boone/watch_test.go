// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	"github.com/codeactual/boone/internal/boone"
	testecho "github.com/codeactual/boone/internal/cage/cmd/testecho"
	cage_os "github.com/codeactual/boone/internal/cage/os"
	cage_exec "github.com/codeactual/boone/internal/cage/os/exec"
	cage_exec_mocks "github.com/codeactual/boone/internal/cage/os/exec/mocks"
	cage_file "github.com/codeactual/boone/internal/cage/os/file"
	"github.com/codeactual/boone/internal/cage/os/file/watcher"
	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
	"github.com/codeactual/boone/internal/cage/testkit"
	testkit_file "github.com/codeactual/boone/internal/cage/testkit/os/file"
	testkit_filepath "github.com/codeactual/boone/internal/cage/testkit/path/filepath"
	testkit_time "github.com/codeactual/boone/internal/cage/testkit/time"
	cage_time_mocks "github.com/codeactual/boone/internal/cage/time/mocks"
)

type WatchSuite struct {
	suite.Suite

	absPath1 string
	absPath2 string
	absPath3 string

	executor        *cage_exec_mocks.Executor
	timer           *cage_time_mocks.Timer
	clock           *cage_time_mocks.Clock
	timerCh         chan time.Time
	timerChReadonly <-chan time.Time

	passTarget       boone.Target
	passAddPathCh    chan string
	passExecReqCh    chan boone.ExecRequest
	passTargetPassCh chan boone.TargetPass
	passTargetFailCh chan boone.Status
	passWatch        *watcher.Fsnotify

	log *zap.Logger
}

func (suite *WatchSuite) SetupTest() {
	t := suite.T()

	suite.log = testkit.NewZapLogger()

	// fake clock/timer to avoid actual intervals during debounce
	suite.timer, suite.clock, suite.timerCh, suite.timerChReadonly = testkit_time.NewDebounceTimer(&testkit_time.DebounceTimerOption{ResetReturnTrue: true})
	suite.timer.On("C").Return(suite.timerChReadonly)

	// create real files to watch
	testkit_file.ResetTestdata(t)
	_, suite.absPath1 = testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "main.go")
	_, suite.absPath2 = testkit_file.CreateFile(t, "path", "to", "proj", "file.go")
	_, suite.absPath3 = testkit_file.CreateFile(t, "path", "to", "proj", "ci")
	_, _ = testkit_file.CreateFile(t, "path", "to", "proj", "README.md")
	_, _ = testkit_file.CreateFile(t, "path", "to", "proj", "LICENSE")

	suite.passTarget = boone.Target{
		Label: "suite.passTarget Label",
		Id:    "suite.passTarget Id",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
	}
	suite.passTarget.Handler = []boone.Handler{ // must align with requireHandlerExec assertions
		{
			Label: "some handler",
			Exec: []boone.Exec{
				{Cmd: `echo "TargetLabel={{.TargetLabel}} HandlerLabel={{.HandlerLabel}} IncludeRoot={{.IncludeRoot}} Path={{.Path}} Dir={{.Dir}}"`},
			},
		},
	}
	require.NoError(t, boone.FinalizeConfig([]*boone.Target{&suite.passTarget}, &boone.Config{}))

	// perform the same target configuration steps as the CLI in boone.Init
	globs, err := boone.GetTargetGlob(suite.passTarget.Include, suite.passTarget.Exclude)
	require.NoError(t, err)
	includes, err := boone.GetGlobInclude(globs)
	require.NoError(t, err)

	suite.passAddPathCh = make(chan string, 1)
	suite.passExecReqCh = make(chan boone.ExecRequest, 1)
	suite.passTargetPassCh = make(chan boone.TargetPass, 1)
	suite.passTargetFailCh = make(chan boone.Status, 1)

	var dispatcher *boone.Dispatcher
	suite.passWatch, dispatcher = suite.newWatch(suite.passTarget, includes, suite.passAddPathCh, suite.passExecReqCh, suite.passTargetPassCh, suite.passTargetFailCh, 0, 1)
	go dispatcher.Start()
}

func (suite *WatchSuite) newWatch(target boone.Target, includes map[string]cage_filepath.Glob, addPathCh chan string, execReqCh chan boone.ExecRequest, targetPassCh chan boone.TargetPass, targetFailCh chan boone.Status, handlerExitCode int, handlerExitCount int) (watch *watcher.Fsnotify, dispatcher *boone.Dispatcher) {
	t := suite.T()

	// to verify the command string created from the template
	suite.executor = new(cage_exec_mocks.Executor)

	var expectedErr error
	if handlerExitCode != 0 {
		expectedErr = errors.Errorf("exit status %d", handlerExitCode)
	}

	if handlerExitCount > 0 {
		suite.executor.On("Buffered", mock.AnythingOfType("*context.timerCtx"), mock.AnythingOfType("*exec.Cmd")).Return(&bytes.Buffer{}, &bytes.Buffer{}, cage_exec.PipelineResult{}, expectedErr).Times(handlerExitCount)
	}

	watch = new(watcher.Fsnotify)
	watch.Debounce(boone.PreDebounce)
	sub := boone.Watcher{
		AddPathCh: addPathCh,
		ExecReqCh: execReqCh,
		Target:    target,
		Watcher:   watch,
		Log:       suite.log,
	}
	sub.SetInclude(includes)
	watch.AddSubscriber(&sub)
	for p := range includes {
		require.NoError(t, watch.AddPath(p))
	}

	dispatcher = &boone.Dispatcher{
		Clock:        suite.clock,
		Executor:     suite.executor,
		Log:          suite.log,
		ExecReqCh:    execReqCh,
		TargetPassCh: targetPassCh,
		TargetFailCh: targetFailCh,
	}

	return watch, dispatcher
}

func (suite *WatchSuite) TearDownTest() {
	if suite.passWatch != nil {
		suite.tearDownDefaultTarget()
	}
}

func (suite *WatchSuite) tearDownDefaultTarget() {
	suite.passWatch.Close()
	suite.passWatch = nil // for Close to avoid double-closing in TearDownTest
}

func (suite *WatchSuite) requireHandlerExec(callId int, expectedPath, expectedDir string) {
	t := suite.T()
	require.Exactly(
		t,
		[]string{
			"echo",
			fmt.Sprintf(
				"TargetLabel=%s HandlerLabel=%s IncludeRoot=%s Path=%s Dir=%s",
				suite.passTarget.Label,
				suite.passTarget.Handler[0].Label,
				testkit_filepath.Abs(t, suite.passTarget.Include[0].Root),
				expectedPath,
				expectedDir,
			),
		},
		suite.executor.Calls[callId].Arguments[1].(*exec.Cmd).Args,
	)
}

func (suite *WatchSuite) expectHandlerExec(fn func(args mock.Arguments)) {
	call := suite.executor.On("Buffered", mock.AnythingOfType("*context.timerCtx"), mock.AnythingOfType("*exec.Cmd"))
	call.Return(&bytes.Buffer{}, &bytes.Buffer{}, cage_exec.PipelineResult{}, nil)
	call.Run(fn).Once()
}

func (suite *WatchSuite) TestFileWrite() {
	t := suite.T()

	var wg sync.WaitGroup
	wg.Add(1)
	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	err := cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(0, suite.absPath1, filepath.Dir(suite.absPath1))
}

func (suite *WatchSuite) TestDirCreate() {
	t := suite.T()

	_, newDirAbs := testkit_file.CreateDir(t, "path", "to", "proj", "cmd", "proj", "newdir")

	require.Exactly(t, newDirAbs, <-suite.passAddPathCh)
}

func (suite *WatchSuite) TestFileCreate() {
	t := suite.T()

	var wg sync.WaitGroup
	wg.Add(1)
	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	_, newFileAbs := testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "newfile.go")

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(0, newFileAbs, filepath.Dir(newFileAbs))
}

func (suite *WatchSuite) TestFileCreateInAncestor() {
	t := suite.T()

	var wg sync.WaitGroup
	wg.Add(1)
	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	_, newFileAbs := testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "cmd.go")

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(0, newFileAbs, filepath.Dir(newFileAbs))
}

func (suite *WatchSuite) TestDirCreateInAncestor() {
	t := suite.T()

	_, newDirAbs := testkit_file.CreateDir(t, "path", "to", "proj", "cmd", "other_cmd_dir")

	require.Exactly(t, newDirAbs, <-suite.passAddPathCh)
}

// create new file in a new directory, asserting both were added to watch list
func (suite *WatchSuite) TestAddWatchForNewFileInNewDir() {
	t := suite.T()

	//
	// step: new dir
	//

	_, newDirAbs := testkit_file.CreateDir(t, "path", "to", "proj", "cmd", "proj", "newdir")

	require.Exactly(t, newDirAbs, <-suite.passAddPathCh)

	//
	// step: new file in the above new dir
	//

	var wg sync.WaitGroup
	wg.Add(1)

	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	_, newFileAbs := testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "newdir", "newfile.go")

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(0, newFileAbs, filepath.Dir(newFileAbs))

	//
	// step: write to new file in new dir
	//
	err := cage_file.AppendString(newFileAbs, "new text")
	require.NoError(t, err)

	// add another expected call to the one already scheduled by SetupTest
	wg.Add(1)
	suite.expectHandlerExec(func(_ mock.Arguments) {
		wg.Done()
	})

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(1, newFileAbs, newDirAbs)
}

// create new file in a new directory that was renamed, asserting new dir name was added to watch list
func (suite *WatchSuite) TestAddWatchForNewFileInRenamedDir() {
	t := suite.T()

	//
	// step: new dir
	//

	_, newDirAbs := testkit_file.CreateDir(t, "path", "to", "proj", "cmd", "proj", "newdir")

	require.Exactly(t, newDirAbs, <-suite.passAddPathCh)

	//
	// step: rename the new dir
	//

	err := os.Rename(newDirAbs, newDirAbs+"2")
	require.NoError(t, err)

	require.Exactly(t, newDirAbs+"2", <-suite.passAddPathCh)

	//
	// step: write to new file in renamed dir
	//

	var wg sync.WaitGroup
	wg.Add(1)

	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	_, newFileAbs := testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "newdir2", "newfile.go")

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(0, newFileAbs, newDirAbs+"2")
}

// write to a new file that was renamed, asserting new file name was added to watch list
func (suite *WatchSuite) TestAddWatchForRenamedFile() {
	t := suite.T()

	//
	// step: new file
	//

	var wg sync.WaitGroup
	wg.Add(1)
	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	_, newFileAbs := testkit_file.CreateFile(t, "path", "to", "proj", "cmd", "proj", "newfile.go")
	dirAbs := filepath.Dir(newFileAbs)

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(0, newFileAbs, dirAbs)

	//
	// step: rename the new file
	//

	wg.Add(1)
	suite.expectHandlerExec(func(_ mock.Arguments) {
		wg.Done()
	})

	renameFileAbs := strings.Replace(newFileAbs, "newfile", "newfile2", 1)

	err := os.Rename(newFileAbs, renameFileAbs)
	require.NoError(t, err)

	suite.timerCh <- time.Now() // pre-debounce the rename activity
	wg.Wait()                   // for an attempt to execute the handler

	//
	// step: write to renamed file
	//

	wg.Add(1)
	suite.expectHandlerExec(func(_ mock.Arguments) {
		wg.Done()
	})

	err = cage_file.AppendString(renameFileAbs, "new text")
	require.NoError(t, err)

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	suite.requireHandlerExec(1, renameFileAbs, dirAbs)
}

func (suite *WatchSuite) TestTargetPassChan() {
	t := suite.T()

	var wg sync.WaitGroup
	wg.Add(1)
	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	err := cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	suite.timerCh <- time.Now() // let debounced handler finally execute
	wg.Wait()                   // for an attempt to execute the handler

	require.NotNil(t, suite.passTarget.Id)
	require.Exactly(t, suite.passTarget.Id, (<-suite.passTargetPassCh).TargetId)
}

func (suite *WatchSuite) TestTargetFailChan() {
	t := suite.T()

	suite.tearDownDefaultTarget()

	//
	// same boilerplate as in SetupTest for suite.passTarget
	//
	failTarget := boone.Target{
		Label: suite.passTarget.Label,
		Id:    "failing target",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
	}
	failTarget.Handler = []boone.Handler{
		{Label: "some handler", Exec: []boone.Exec{{Cmd: "ls /not/found"}}},
	}
	require.NoError(t, boone.FinalizeConfig([]*boone.Target{&failTarget}, &boone.Config{}))

	globs, err := boone.GetTargetGlob(failTarget.Include, failTarget.Exclude)
	require.NoError(t, err)

	includes, err := boone.GetGlobInclude(globs)
	require.NoError(t, err)

	failAddPathCh := make(chan string, 1)
	failExecReqCh := make(chan boone.ExecRequest, 1)
	failTargetPassCh := make(chan boone.TargetPass, 1)
	failTargetFailCh := make(chan boone.Status, 1)
	failWatch, dispatcher := suite.newWatch(failTarget, includes, failAddPathCh, failExecReqCh, failTargetPassCh, failTargetFailCh, 1, 1)
	defer failWatch.Close()
	go dispatcher.Start()

	err = cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	//
	// trigger watcher
	//

	suite.timerCh <- time.Now() // let debounced handler finally execute

	actualStatus := <-failTargetFailCh
	require.Exactly(t, failTarget.Id, actualStatus.TargetId)
}

func (suite *WatchSuite) TestHandlerFailEndsTargetRun() {
	t := suite.T()

	suite.tearDownDefaultTarget()

	//
	// same boilerplate as in SetupTest for suite.passTarget
	//
	//
	failTarget := boone.Target{
		Label: suite.passTarget.Label,
		Id:    "failing target",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
	}
	failTarget.Handler = []boone.Handler{
		{Label: "always fails", Exec: []boone.Exec{{Cmd: "ls /not/found"}}},
		{Label: "always succeeds", Exec: []boone.Exec{{Cmd: "echo 'should not run'"}}},
	}
	require.NoError(t, boone.FinalizeConfig([]*boone.Target{&failTarget}, &boone.Config{}))

	globs, err := boone.GetTargetGlob(failTarget.Include, failTarget.Exclude)
	require.NoError(t, err)

	includes, err := boone.GetGlobInclude(globs)
	require.NoError(t, err)

	failAddPathCh := make(chan string, 1)
	failExecReqCh := make(chan boone.ExecRequest, 1)
	failTargetPassCh := make(chan boone.TargetPass, 1)
	failTargetFailCh := make(chan boone.Status, 1)
	failWatch, dispatcher := suite.newWatch(failTarget, includes, failAddPathCh, failExecReqCh, failTargetPassCh, failTargetFailCh, 1, 1)
	defer failWatch.Close()
	go dispatcher.Start()

	var wg sync.WaitGroup
	wg.Add(1)

	suite.executor.ExpectedCalls[0].Run(func(args mock.Arguments) {
		wg.Done()
	})

	suite.expectHandlerExec(func(_ mock.Arguments) {
		t.FailNow() // the failure of the 1st handler should prevent this one from being attempted
	})

	err = cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	//
	// trigger watcher
	//

	suite.timerCh <- time.Now() // let debounced handler finally execute

	wg.Wait()                         // for an attempt to execute the 1st handler
	time.Sleep(10 * time.Millisecond) // for an attempt to execute the 2nd handler (which is not expected)

	actualStatus := <-failTargetFailCh
	require.Exactly(t, failTarget.Id, actualStatus.TargetId)
}

func (suite *WatchSuite) TestHandlerCancelOnEvent() {
	t := suite.T()

	suite.tearDownDefaultTarget()

	//
	// same boilerplate as in SetupTest for suite.passTarget except for the selected command
	//
	target := boone.Target{
		Label: "Label: target canceled mid-exec by new activity",
		Id:    "Id: target canceled mid-exec by new activity",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
	}
	target.Handler = []boone.Handler{
		{
			Label: "some handler",
			Exec: []boone.Exec{
				// spawn a process group with the one child and one grandchild
				{Cmd: testecho.NewCmdString(testecho.Input{Spawn: true})},
			},
		},
	}
	require.NoError(t, boone.FinalizeConfig([]*boone.Target{&target}, &boone.Config{}))

	globs, err := boone.GetTargetGlob(target.Include, target.Exclude)
	require.NoError(t, err)

	includes, err := boone.GetGlobInclude(globs)
	require.NoError(t, err)

	addPathCh := make(chan string, 1)
	execReqCh := make(chan boone.ExecRequest, 1)
	targetPassCh := make(chan boone.TargetPass, 1)
	targetFailCh := make(chan boone.Status, 1)
	watch, dispatcher := suite.newWatch(target, includes, addPathCh, execReqCh, targetPassCh, targetFailCh, 0, 1)
	defer watch.Close()

	dispatcher.Executor = cage_exec.CommonExecutor{}
	go dispatcher.Start() // start after dispatcher.* field writes to avoid data race

	err = cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	suite.timerCh <- time.Now() // let debounced handler finally execute

	time.Sleep(boone.ExecRequestQueueTick + (50 * time.Millisecond)) // wait for testecho and its child process to start

	// trigger watcher again in order to cancel in-progress handler exec
	err = cage_file.AppendString(suite.absPath1, "more new text")
	require.NoError(t, err)

	actualStatus := <-targetFailCh
	require.Exactly(t, target.Id, actualStatus.TargetId)
	require.Exactly(t, "context canceled", actualStatus.Err)

	time.Sleep(cage_exec.SigKillDelay)

	// verify child process exited
	require.True(t, actualStatus.Pid[0] > 0)
	_, err = cage_os.FindProcess(actualStatus.Pid[0])
	require.Error(t, err)

	// verify grandchild process exited
	require.True(t, actualStatus.Stdout != "" && actualStatus.Stdout != "0", fmt.Sprintf("stdout: [%s]", actualStatus.Stdout))
	_, err = cage_os.StringPidToProcess(actualStatus.Stdout)
	require.Error(t, err)
}

func (suite *WatchSuite) TestUpstreamCancelDownstream() {
	t := suite.T()

	suite.tearDownDefaultTarget()
	upstream := boone.Target{
		Label: "some upstream",
		Id:    "some upstream",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("package.json"),
			},
		},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "exit 0"}}},
		},
	}
	downstream := boone.Target{
		Label: "some downstream",
		Id:    "some downstream",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
		Upstream: []string{"some upstream"},
		Handler: []boone.Handler{
			{Label: "some handler", Exec: []boone.Exec{{Cmd: "sleep 1000"}}},
		},
	}
	allTarget := []*boone.Target{&upstream, &downstream}
	addPathCh := make(chan string, 1)
	execReqCh := make(chan boone.ExecRequest, 1)
	passCh := make(chan boone.TargetPass, 1)
	failCh := make(chan boone.Status, 1)

	require.NoError(t, boone.FinalizeConfig(allTarget, &boone.Config{}))

	//
	// upstream target that triggers cancellation downstream
	// - same boilerplate as in SetupTest for suite.passTarget except for the selected command
	// - note that its handler is not executed (no suite.tim)
	//

	globs, err := boone.GetTargetGlob(upstream.Include, upstream.Exclude)
	require.NoError(t, err)

	includes, err := boone.GetGlobInclude(globs)
	require.NoError(t, err)

	upWatch, dispatcher := suite.newWatch(upstream, includes, addPathCh, execReqCh, passCh, failCh, 0, 1)
	defer upWatch.Close()

	timer, clock, timerCh, timerChReadonly := testkit_time.NewDebounceTimer(&testkit_time.DebounceTimerOption{ResetReturnTrue: true})
	timer.On("C").Return(timerChReadonly)

	dispatcher.Executor = cage_exec.CommonExecutor{}
	dispatcher.Clock = clock

	//
	// downstream target that gets canceled
	// - same boilerplate as in SetupTest for suite.passTarget
	//

	globs, err = boone.GetTargetGlob(downstream.Include, downstream.Exclude)
	require.NoError(t, err)

	includes, err = boone.GetGlobInclude(globs)
	require.NoError(t, err)

	// effectively shares the dispatcher of the upstream because the same channels are used and the
	// dispatcher created in this newWatch call is never started
	downWatch, _ := suite.newWatch(downstream, includes, addPathCh, execReqCh, passCh, failCh, 0, 1)
	defer downWatch.Close()

	//
	// start shared dispatcher (shared via execReqCh/passCh/failCh/etc.)
	//

	go dispatcher.Start() // start after dispatcher.* field writes to avoid data race

	//
	// trigger downstream handler
	//

	err = cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	timerCh <- time.Now() // let debounced handler finally execute

	time.Sleep(boone.ExecRequestQueueTick + (50 * time.Millisecond)) // let the handler start

	//
	// cancel downstream handler by triggering upstream handler
	//

	_, _ = testkit_file.CreateFile(t, "path", "to", "proj", "package.json")
	require.NoError(t, err)

	//
	// expect downstream cancellation
	//

	actualStatus := <-failCh
	require.Exactly(t, downstream.Id, actualStatus.TargetId)
	require.Exactly(t, "context canceled", actualStatus.Err) // flaky: sometimes it's 'context deadline exceeded'
}

func (suite *WatchSuite) TestHandlerCancelOnTimeout() {
	t := suite.T()

	suite.tearDownDefaultTarget()

	//
	// same boilerplate as in SetupTest for suite.passTarget except for the selected command
	//
	target := boone.Target{
		Label: "Label: target canceled mid-exec by timeout",
		Id:    "Id: target canceled mid-exec by timeout",
		Root:  filepath.Join(testkit_file.DynamicDataDir(), "path", "to", "proj"),
		Include: []cage_filepath.Glob{
			{
				Pattern: filepath.Join("**", "*.go"),
			},
		},
	}
	target.Handler = []boone.Handler{
		{
			Label: "some handler",
			Exec: []boone.Exec{
				// spawn a process group with the one child and one grandchild
				{
					Cmd:     testecho.NewCmdString(testecho.Input{Spawn: true}),
					Timeout: "1s",
				},
			},
		},
	}
	require.NoError(t, boone.FinalizeConfig([]*boone.Target{&target}, &boone.Config{}))

	globs, err := boone.GetTargetGlob(target.Include, target.Exclude)
	require.NoError(t, err)

	includes, err := boone.GetGlobInclude(globs)
	require.NoError(t, err)

	addPathCh := make(chan string, 1)
	execReqCh := make(chan boone.ExecRequest, 1)
	targetPassCh := make(chan boone.TargetPass, 1)
	targetFailCh := make(chan boone.Status, 1)
	watch, dispatcher := suite.newWatch(target, includes, addPathCh, execReqCh, targetPassCh, targetFailCh, 0, 1)
	defer watch.Close()

	dispatcher.Executor = cage_exec.CommonExecutor{}
	go dispatcher.Start() // start after dispatcher.* field writes to avoid data race

	err = cage_file.AppendString(suite.absPath1, "new text")
	require.NoError(t, err)

	suite.timerCh <- time.Now() // let debounced handler finally execute

	actualStatus := <-targetFailCh
	require.Exactly(t, target.Id, actualStatus.TargetId)
	require.Exactly(t, "context deadline exceeded", actualStatus.Err)

	time.Sleep(cage_exec.SigKillDelay)

	// verify child process exited
	require.True(t, actualStatus.Pid[0] > 0)
	_, err = cage_os.FindProcess(actualStatus.Pid[0])
	require.Error(t, err)

	// verify grandchild process exited
	require.True(t, actualStatus.Stdout != "" && actualStatus.Stdout != "0", actualStatus.Stdout)
	_, err = cage_os.StringPidToProcess(actualStatus.Stdout)
	require.Error(t, err)
}

func TestWatchSuite(t *testing.T) {
	suite.Run(t, new(WatchSuite))
}
