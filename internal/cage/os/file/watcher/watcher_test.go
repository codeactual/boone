// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package watcher_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	cage_file "github.com/codeactual/boone/internal/cage/os/file"
	"github.com/codeactual/boone/internal/cage/os/file/watcher"
	testkit_file "github.com/codeactual/boone/internal/cage/testkit/os/file"
	testkit_filepath "github.com/codeactual/boone/internal/cage/testkit/path/filepath"
)

const (
	DebounceInterval    = 500 * time.Millisecond
	UnexpectedEventWait = 50 * time.Millisecond
)

func TestSuite(t *testing.T) {
	suite.Run(t, new(FsnotifySuite))
}

// Subscriber is a fake that only captures events/errors and decrements WaitGroups
// to allow tests to wait until all expected events/errors are collected.
type Subscriber struct {
	Events   []watcher.Event
	EventsWg sync.WaitGroup

	sync.RWMutex

	Errors   []error
	ErrorsWg sync.WaitGroup
}

func (s *Subscriber) Event(event watcher.Event) {
	s.Lock()
	defer s.Unlock()
	s.Events = append(s.Events, event)
	s.EventsWg.Done()
}

func (s *Subscriber) Error(err error) {
	s.Lock()
	defer s.Unlock()
	s.Errors = append(s.Errors, err)
	s.ErrorsWg.Done()
}

// WatcherSuite is an implementation-agnostic suite which specific suites, e.g. FsnotifySuite,
// can embed and then extend if needed.
type WatcherSuite struct {
	suite.Suite

	w watcher.Watcher

	origFilename string
	newFilename  string
	origDirname  string
	newDirname   string
	origWrite    string
	newWrite     string
}

func (s *WatcherSuite) SetupTest() {
	t := s.T()

	testkit_file.ResetTestdata(t)

	s.origFilename = "orig_file"
	s.newFilename = "new_file"
	s.origDirname = "orig_dir"
	s.newDirname = "new_dir"
	s.origWrite = "orig_write"
	s.newWrite = "new_write"
}

func (s *WatcherSuite) TearDownTest() {
	// allow TestClose to not use s.w for its case and avoid closing an "unopened" watcher here
	// because s.w.AddPath (which starts the internal goroutine) was never called
	if s.w != nil {
		s.w.Close()
	}
}

func (s *WatcherSuite) TestFileCreate() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(1)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	_, absPath := testkit_file.CreateFile(t, s.origFilename)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 1)
	require.Exactly(t, watcher.Create, sub.Events[0].Op)
	require.Exactly(t, absPath, sub.Events[0].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestFileRename() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(3)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateFile(t, s.origFilename)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	newName := filepath.Join(testkit_file.DynamicDataDir(), s.newFilename)
	err = os.Rename(relPath, newName)
	require.NoError(t, err)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 3)
	require.Exactly(t, watcher.Rename, sub.Events[0].Op) // from dir or file watch
	require.Exactly(t, absPath, sub.Events[0].Path)
	require.Exactly(t, watcher.Create, sub.Events[1].Op) // from dir watch
	require.Exactly(t, testkit_filepath.Abs(t, newName), sub.Events[1].Path)
	require.Exactly(t, watcher.Rename, sub.Events[2].Op)
	require.Exactly(t, absPath, sub.Events[2].Path) // from dir or file watch

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestFileRemove() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(1)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateFile(t, s.origFilename)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	// Also watch the dir to verify whether one or two events are emitted.
	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	err = os.Remove(relPath)
	require.NoError(t, err)

	// Give some time for a potential 2nd event, due to the dir watch, to emit.
	// (The problem is that if the WaitGroup finishes before an unexpected event,
	// the test will pass.)
	time.Sleep(UnexpectedEventWait)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 1)
	require.Exactly(t, watcher.Remove, sub.Events[0].Op)
	require.Exactly(t, absPath, sub.Events[0].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestFileWriteDebounced() {
	t := s.T()

	s.w.Debounce(DebounceInterval)

	sub := Subscriber{}
	sub.EventsWg.Add(1)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateFile(t, s.origFilename)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	err = cage_file.AppendString(relPath, s.origWrite)
	require.NoError(t, err)

	// Give some time for a potential 2nd event, due to the dir watch, to emit.
	// (The problem is that if the WaitGroup finishes before an unexpected event,
	// the test will pass.)
	time.Sleep(UnexpectedEventWait)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 1)
	require.Exactly(t, watcher.Write, sub.Events[0].Op) // from dir or file watch
	require.Exactly(t, absPath, sub.Events[0].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestFileWriteNonDebounced() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(2)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateFile(t, s.origFilename)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	err = cage_file.AppendString(relPath, s.origWrite)
	require.NoError(t, err)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 2)
	require.Exactly(t, watcher.Write, sub.Events[0].Op) // from dir or file watch
	require.Exactly(t, absPath, sub.Events[0].Path)
	require.Exactly(t, watcher.Write, sub.Events[0].Op) // from dir or file watch
	require.Exactly(t, absPath, sub.Events[0].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestDirCreate() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(1)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	_, absPath := testkit_file.CreateDir(t, s.origDirname)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 1)
	require.Exactly(t, watcher.Create, sub.Events[0].Op)
	require.Exactly(t, absPath, sub.Events[0].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestDirRename() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(3)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateDir(t, s.origDirname)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	newName := filepath.Join(testkit_file.DynamicDataDir(), s.newDirname)
	err = os.Rename(relPath, newName)
	require.NoError(t, err)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 3)
	require.Exactly(t, watcher.Rename, sub.Events[0].Op) // from dir or file watch
	require.Exactly(t, absPath, sub.Events[0].Path)
	require.Exactly(t, watcher.Create, sub.Events[1].Op) // from dir watch
	require.Exactly(t, testkit_filepath.Abs(t, newName), sub.Events[1].Path)
	require.Exactly(t, watcher.Rename, sub.Events[2].Op)
	require.Exactly(t, absPath, sub.Events[2].Path) // from dir or file watch

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestDirRemoveDebounced() {
	t := s.T()

	s.w.Debounce(DebounceInterval)

	sub := Subscriber{}
	sub.EventsWg.Add(1)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateDir(t, s.origDirname)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	err = os.Remove(relPath)
	require.NoError(t, err)

	// Give some time for a potential 2nd event, due to the dir watch, to emit.
	// (The problem is that if the WaitGroup finishes before an unexpected event,
	// the test will pass.)
	time.Sleep(20 * time.Millisecond)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 1)
	require.Exactly(t, watcher.Remove, sub.Events[0].Op)
	require.Exactly(t, absPath, sub.Events[0].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestDirRemoveNonDebounced() {
	t := s.T()

	sub := Subscriber{}
	sub.EventsWg.Add(2)

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	relPath, absPath := testkit_file.CreateDir(t, s.origDirname)
	err = s.w.AddPath(relPath)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	err = os.Remove(relPath)
	require.NoError(t, err)

	sub.EventsWg.Wait()

	require.Len(t, sub.Events, 2)
	require.Exactly(t, watcher.Remove, sub.Events[0].Op)
	require.Exactly(t, absPath, sub.Events[0].Path)
	require.Exactly(t, watcher.Remove, sub.Events[1].Op)
	require.Exactly(t, absPath, sub.Events[1].Path)

	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestMultipleSubscribers() {
	t := s.T()

	sub1 := Subscriber{}
	sub2 := Subscriber{}
	sub1.EventsWg.Add(1)
	sub2.EventsWg.Add(1)

	err := s.w.AddSubscriber(&sub1)
	require.NoError(t, err)
	err = s.w.AddSubscriber(&sub2)
	require.NoError(t, err)

	err = s.w.AddPath(testkit_file.DynamicDataDir())
	require.NoError(t, err)

	_, absPath := testkit_file.CreateFile(t, s.origFilename)

	sub1.EventsWg.Wait()
	sub2.EventsWg.Wait()

	require.Len(t, sub1.Events, 1)
	require.Exactly(t, watcher.Create, sub1.Events[0].Op)
	require.Exactly(t, absPath, sub1.Events[0].Path)
	require.Len(t, sub2.Events, 1)
	require.Exactly(t, watcher.Create, sub2.Events[0].Op)
	require.Exactly(t, absPath, sub2.Events[0].Path)

	require.Len(t, sub1.Errors, 0)
	require.Len(t, sub2.Errors, 0)
}

func (s *WatcherSuite) TestRemoveDirPath() {
	t := s.T()

	sub := Subscriber{}

	err := s.w.AddSubscriber(&sub)
	require.NoError(t, err)

	dirPath := testkit_file.DynamicDataDir()

	err = s.w.AddPath(dirPath)
	require.NoError(t, err)

	err = s.w.RemovePath(dirPath)
	require.NoError(t, err)

	_, _ = testkit_file.CreateFile(t, s.origFilename)

	// Give some time for a potential event to emit.
	time.Sleep(50 * time.Millisecond)

	require.Len(t, sub.Events, 0)
	require.Len(t, sub.Errors, 0)
}

func (s *WatcherSuite) TestClose() {
	t := s.T()

	w := new(watcher.Fsnotify)
	s.w = nil

	sub := Subscriber{}

	err := w.AddSubscriber(&sub)
	require.NoError(t, err)

	dirPath := testkit_file.DynamicDataDir()

	err = w.AddPath(dirPath)
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	_, _ = testkit_file.CreateFile(t, s.origFilename)

	// Give some time for a potential event to emit.
	time.Sleep(50 * time.Millisecond)

	require.Len(t, sub.Events, 0)
	require.Len(t, sub.Errors, 0)
}
