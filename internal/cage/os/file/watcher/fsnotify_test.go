// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package watcher_test

import (
	"github.com/codeactual/boone/internal/cage/os/file/watcher"
)

type FsnotifySuite struct {
	WatcherSuite
}

func (s *FsnotifySuite) SetupTest() {
	s.WatcherSuite.SetupTest()
	s.w = new(watcher.Fsnotify)
}
