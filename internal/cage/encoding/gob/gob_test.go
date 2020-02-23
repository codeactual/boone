// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package gob_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	cage_gob "github.com/codeactual/boone/internal/cage/encoding/gob"
	testkit_file "github.com/codeactual/boone/internal/cage/testkit/os/file"
)

type SomeStruct struct {
	Num  int
	Text string
}

func TestStruct(t *testing.T) {
	testkit_file.ResetTestdata(t)

	expectedValue := SomeStruct{Num: 7, Text: "seven"}
	var actualValue SomeStruct
	_, name := testkit_file.CreatePath(t, "somefile")

	require.NoError(t, cage_gob.EncodeToFile(name, expectedValue))

	dec, err := cage_gob.DecodeFromFile(name)
	require.NoError(t, err)
	require.NoError(t, dec.Decode(&actualValue))

	require.Exactly(t, expectedValue, actualValue)
}
