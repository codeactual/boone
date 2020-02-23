// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tp_time "github.com/codeactual/boone/internal/third_party/gist.github.com/time"

	cage_time "github.com/codeactual/boone/internal/cage/time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

type Fsnotify struct {
	watcher     *fsnotify.Watcher
	subscribers []Subscriber
	done        chan struct{}

	// debouncers indexes cage/time.Debounce compatible functions by Event.String output strings.
	debouncers map[string]func(interface{})

	debounceInterval time.Duration
}

func (w *Fsnotify) AddSubscriber(sub Subscriber) error {
	if w.subscribers == nil {
		w.subscribers = []Subscriber{}
	}
	w.subscribers = append(w.subscribers, sub)
	return nil
}

func (w *Fsnotify) AddPath(name string) (err error) {
	if w.watcher == nil {
		w.watcher, err = fsnotify.NewWatcher()
		if err != nil {
			return errors.Wrap(err, "failed to create new watcher")
		}

		w.done = make(chan struct{}, 1)
		go w.monitor()
	}

	name, err = filepath.Abs(name)
	if err != nil {
		return errors.Wrapf(err, "failed to get absolute path of [%s]", name)
	}

	err = w.watcher.Add(name)
	if err != nil {
		return errors.Wrapf(err, "failed to add watcher path [%s]", name)
	}

	return nil
}

func (w *Fsnotify) RemovePath(name string) (err error) {
	name, err = filepath.Abs(name)
	if err != nil {
		return errors.Wrapf(err, "failed to get absolute path of [%s]", name)
	}

	err = w.watcher.Remove(name)
	if err != nil {
		return errors.Wrapf(err, "failed to remove watcher path [%s]", name)
	}

	return nil
}

func (w *Fsnotify) Close() (err error) {
	close(w.done)
	return errors.Wrap(w.watcher.Close(), "failed to close fsnotify watcher")
}

// monitor defines the goroutine that dispatches all event/error details to
// to subscribers.
func (w *Fsnotify) monitor() {
	for {
		select {
		case <-w.done:
			return
		case event := <-w.watcher.Events:
			if event.Name == "" {
				// (currently only a concern for Close-related tests)
				// E.g. if a directory is passed to AddPath and then Close is called, an empty Event
				// is still spammed here if a file is created in that directory after Close.
				// https://github.com/fsnotify/fsnotify/issues/140#issuecomment-217539670
				continue
			}

			op := w.filterOp(event.Op)
			if op == 0 {
				continue
			}

			filteredEvent := Event{Path: event.Name, Op: op}

			if w.debounceInterval > 0 {
				// Avoid sending double notification of an event when both the file and its directory
				// are watched.
				eventKey := event.String()
				if w.debouncers == nil {
					w.debouncers = make(map[string]func(interface{}))
				}
				if w.debouncers[eventKey] == nil {
					w.debouncers[eventKey] = tp_time.Debounce(cage_time.RealClock{}, w.debounceInterval, func(v interface{}) {
						w.broadcastEvent(v)
					})
				}
				w.debouncers[eventKey](filteredEvent)
			} else {
				w.broadcastEvent(filteredEvent)
			}
		case err := <-w.watcher.Errors:
			if err == nil {
				// (currently only a concern for Close-related tests)
				// E.g. if a directory is passed to AddPath and then Close is called, an empty Event
				// is still spammed here if a file is created in that directory after Close.
				// https://github.com/fsnotify/fsnotify/issues/140#issuecomment-217539670
				continue
			}
			for _, s := range w.subscribers {
				s.Error(err)
			}
		}
	}
}

func (w *Fsnotify) broadcastEvent(e interface{}) {
	event, ok := e.(Event)
	if !ok {
		fmt.Fprintf(os.Stderr, "skipped broadcast of non-Event value: %+v", e)
		return
	}
	for _, s := range w.subscribers {
		s.Event(event)
	}
}

// simplifyEvent reduces the types to only those defined in this package.
//
// fsnotify supports multi-events via bit masks and also chmod events, both of which
// are effectively filtered.
func (w *Fsnotify) filterOp(op fsnotify.Op) Op {
	if op&fsnotify.Remove == fsnotify.Remove {
		return Remove
	}
	if op&fsnotify.Rename == fsnotify.Rename {
		return Rename
	}
	if op&fsnotify.Create == fsnotify.Create {
		return Create
	}
	if op&fsnotify.Write == fsnotify.Write {
		return Write
	}
	return 0
}

func (w *Fsnotify) Debounce(d time.Duration) {
	w.debounceInterval = d
}
