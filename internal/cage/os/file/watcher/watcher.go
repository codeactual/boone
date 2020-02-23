// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package watcher

import "time"

// Op is used for file/directory operation codes.
type Op uint8

const (
	Create Op = 1 << iota
	Rename
	Remove
	Write
)

func (o Op) String() string {
	switch o {
	case Create:
		return "Create"
	case Rename:
		return "Rename"
	case Remove:
		return "Remove"
	default:
		return "Write"
	}
}

// Event instances are passed to Subscriber implementations on file/directory activity.
type Event struct {
	// Path holds the absolute path to the file/directory.
	Path string

	// Op defines the file/directory operation.
	Op Op
}

// Subscriber implementations receive Event and error values.
type Subscriber interface {
	Event(Event)
	Error(error)
}

type Watcher interface {
	// AddSubscriber appends the list of subscribers that receive event/error details.
	AddSubscriber(Subscriber) error

	// AddPath appends the file/directory (non-recursive) to the watch list and
	// begins monitoring in a new goroutine.
	//
	// Absolute and relative paths are supported. However all paths are made absolute internally.
	AddPath(string) error

	// RemovePath stops the file/directory (non-recursive) from being watched.
	//
	// Absolute and relative paths are supported.
	RemovePath(string) error

	// Close ends all monitoring behavior and clears the watch/subscriber list.
	Close() error

	// Set the amount of time to wait for duplicate events (same Event.String output value)
	// to be received before broadcasting one Event value to subscribers.
	//
	// Implementations should not debounce events if this method is not called.
	Debounce(time.Duration)
}
