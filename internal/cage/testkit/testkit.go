// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package testkit

import (
	"os"
	"testing"

	"go.uber.org/zap"
)

func FatalErrf(t *testing.T, err error, f string, v ...interface{}) {
	if err != nil {
		f = f + ": %+v"
		v = append(v, err)
		t.Fatalf(f, v...)
	}
}

// NewZapLogger writes to stdout if enabled via environment variable cage_testkit_log=1,
// or writes to nothing if disabled.
func NewZapLogger() *zap.Logger {
	if os.Getenv("cage_testkit_log") == "1" {
		l, err := zap.NewDevelopment()
		if err != nil {
			panic(err)
		}
		return l
	}
	return zap.NewNop()
}
