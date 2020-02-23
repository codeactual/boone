// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone

import (
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	cage_file "github.com/codeactual/boone/internal/cage/os/file"
	"github.com/codeactual/boone/internal/cage/os/file/watcher"
	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
)

// Watcher listens for the write-activity of a single target's files/directories and sends Dispatcher
// requests to execute the activated targets.
//
// It does not itself monitor filesystem events and instead implements ca/cage/os/file/watcher.Subscriber
// to receive events/errors from the actual monitor (Watcher.watcher).
type Watcher struct {
	// Watcher is the actual filesystem monitor. Watcher is a subscriber of events emitted by the monitor.
	//
	// See NewDispatcher for how it and Watcher are wired together.
	watcher.Watcher

	// PanicCh transports messages from Watcher to the CLI to support cleaner shutdowns.
	PanicCh chan<- interface{}

	// ExecReqCh transports messages from Watcher to the Dispatcher to run activated targets.
	ExecReqCh chan<- ExecRequest

	// AddPathCh transports messages from Watcher to listeners which contain paths to newly
	// created files/directories that are now themselves watched for writes.
	//
	// It is currently only used by tests.
	AddPathCh chan<- string

	// Target is the scope of this Watcher's write-activity detection.
	Target Target

	// Log receives debug/info-level messages.
	Log *zap.Logger

	// include holds an index of watched file/dir paths to their related cage_filepath.Glob values.
	include sync.Map
}

// SetInclude assigns the inclusion patterns to use when filtering write-activity.
func (w *Watcher) SetInclude(i map[string]cage_filepath.Glob) {
	w.include = sync.Map{}
	for k, v := range i {
		w.include.Store(k, v)
	}
}

// Event receives activity descriptions from the filesystem monitor (Watcher.watcher).
//
// It implements ca/cage/os/file/watcher.Subscriber.
func (w *Watcher) Event(event watcher.Event) {
	defer func() { // let higher-level logic recover from this panic-heavy function/goroutine
		if r := recover(); r != nil {
			select { // Only send if there's a receiver.
			case w.PanicCh <- r:
			default:
			}
		}
	}()

	// fsnotify does not support the "bookkeeping" required to link the Rename of file X
	// with the Create of file X2. Also we will not attempt to support it here. So we will
	// avoid having the Rename event affect the debouncing. For now we will also not perform
	// any bookkeeping for Remove events until a correctness/performance status comes up.
	if event.Op != watcher.Write && event.Op != watcher.Create {
		return
	}

	exists, fi, err := cage_file.Exists(event.Path)
	if err != nil {
		panic(errors.Wrapf(err, "failed to verify target [%s] new file/dir [%s] exists", w.Target.Label, event.Path))
	}
	if !exists {
		return // assume it was deleted quickly
	}

	matchRes, err := w.Target.MatchPath(event.Path)
	if err != nil {
		panic(errors.Wrapf(err, "failed to verify target [%s] new file/dir [%s] should be watched", w.Target.Label, event.Path))
	}

	var include cage_filepath.Glob
	var found, addPath, sendExecReq bool
	var includeVal interface{}
	var ok bool

	if event.Op == watcher.Create {
		dir := filepath.Dir(event.Path)
		includeVal, found = w.include.Load(dir) // find the responsible Glob
		if found {
			include, ok = includeVal.(cage_filepath.Glob)
			if !ok {
				panic(errors.Errorf("failed to read target [%s] inclusion config for dir [%s]", w.Target.Label, dir))
			}
			if fi.IsDir() {
				// Always add new directories, erring on the side of monitoring too much over too little,
				// as long as it is not explicitly excluded.
				//
				// If not excluded it's unclear how to accurately decide because globs are usually focused
				// on files and do not prescribe which intermediate directories should be excluded. So watch
				// it in case files in the directory (at any potential depth) have a chance to match against
				// a file-centric glob, e.g. "**/*.go".
				if matchRes.Match || matchRes.Exclude == "" {
					err := w.AddPath(event.Path)
					if err != nil {
						panic(errors.Wrapf(err, "failed to watch target [%s] new dir [%s]", w.Target.Label, event.Path))
					}

					// It's unclear if this is the correct association to make in all cases.
					w.include.Store(event.Path, include)

					addPath = true

					// Only send if there's a receiver. Currently only tests use this channel in order to
					// synchronize prep/assert steps.
					select {
					case w.AddPathCh <- event.Path:
					default:
					}
				}

				// This assignment is redundant but added to document that dir creation is not
				// expected to trigger targets because there's no supported use case for executing
				// a command regardless of whether the dir is empty or not (which is the case for this
				// event type).
				sendExecReq = false
			} else {
				// This assignment is redundant but added to document that new files are not
				// added to the watcher because, based on cage/os/file/watcher_test.TestFileWrite,
				// the directory watch will also capture writes to the files, so in effect it's already watched ...
				addPath = false
				// ... but we will add the path to the include map so that the write-handling logic below
				// will recognize the path and provide the responsible include for logging.
				w.include.Store(event.Path, include)

				sendExecReq = matchRes.Match
			}
		}
	} else if event.Op == watcher.Write {
		includeVal, found = w.include.Load(event.Path)
		include, ok = includeVal.(cage_filepath.Glob)
		if !ok {
			w.Log.Info(
				"inclusion config not found, write-event handling skipped",
				zap.String("target", w.Target.Label),
				zap.String("path", event.Path),
			)
			return
		}
		sendExecReq = found && matchRes.Match
	}

	w.Log.Info(
		"watcher event",
		zap.String("target", w.Target.Label),
		zap.String("op", event.Op.String()),
		zap.String("path", event.Path),
		zap.String("includeGlob", include.Pattern),
		zap.Bool("addPath", addPath),
		zap.Bool("sendExecReq", sendExecReq),
	)

	if sendExecReq {
		w.ExecReqCh <- ExecRequest{
			Cause:   "watcher",
			Event:   event,
			Include: include,

			//
			// send only the required fields to avoid data races (versus sending a *Target)
			//
			TargetId:    w.Target.Id,
			TargetLabel: w.Target.Label,
			Tree:        append([]TargetTree{}, w.Target.Tree...),
			Debounce:    w.Target.debounce,
		}
	}
}

// Error receives errors from the filesystem monitor (Watcher.watcher).
//
// It implements ca/cage/os/file/watcher.Subscriber.
func (w *Watcher) Error(err error) {
	w.Log.Info(
		"watcher error",
		zap.String("target", w.Target.Label),
		zap.Error(err),
	)
}

var _ watcher.Subscriber = (*Watcher)(nil)
