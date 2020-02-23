// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package boone contains sub-packages which provide the CLI commands, the internal API (internal/boone)
// which supports the CLI, and the internal "standard library" (all other internal/*) which is automatically
// extracted from a private monorepo.
package boone

// expand godoc content for the base import path
import (
	_ "github.com/codeactual/boone/cmd/boone/eval"
	_ "github.com/codeactual/boone/cmd/boone/root"
	_ "github.com/codeactual/boone/cmd/boone/run"
	_ "github.com/codeactual/boone/internal/boone"
)
