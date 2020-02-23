// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package zap

import (
	std_zap "go.uber.org/zap"

	// Unclear why in 1.10.2 this is required since zap.Field is alias to it but
	// fails with "undefined" message during compilation.
	"go.uber.org/zap/zapcore"
)

const TagKey = "cageLogTag"

func Tag(tags ...string) zapcore.Field {
	return std_zap.Strings(TagKey, append([]string{}, tags...))
}
