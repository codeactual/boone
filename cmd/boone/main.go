// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"github.com/codeactual/boone/cmd/boone/eval"
	"github.com/codeactual/boone/cmd/boone/root"
	"github.com/codeactual/boone/cmd/boone/run"

	"github.com/pkg/errors"
)

func main() {
	rootCmd := root.NewCommand()
	rootCmd.AddCommand(run.NewCommand())
	rootCmd.AddCommand(eval.NewCommand())
	if err := rootCmd.Execute(); err != nil {
		panic(errors.Wrap(err, "failed to execute command"))
	}
}
