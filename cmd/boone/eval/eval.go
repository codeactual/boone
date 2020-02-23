// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Sub-command eval checks whether a file/dir path would be monitored based on the configuration.
// It provides a way to test a configuration file without having to artificially
// create file activity.
//
// Usage:
//
//	boone eval --config /path/to/config /path/to/subject/file
package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/codeactual/boone/internal/boone"
	"github.com/codeactual/boone/internal/cage/cli/handler"
	handler_cobra "github.com/codeactual/boone/internal/cage/cli/handler/cobra"
)

// Handler defines the sub-command flags and logic.
type Handler struct {
	handler.Session

	ConfigPath string
}

// Init defines the command, its environment variable prefix, etc.
//
// It implements cli/handler/cobra.Handler.
func (h *Handler) Init() handler_cobra.Init {
	return handler_cobra.Init{
		Cmd: &cobra.Command{
			Use:   "eval",
			Short: "Check whether a file/dir path would be monitored based on the configuration",
			Example: strings.Join([]string{
				"boone eval --config /path/to/config /path/to/subject/file",
			}, "\n"),
		},
		EnvPrefix: "BOONE",
	}
}

// BindFlags binds the flags to Handler fields.
//
// It implements cli/handler/cobra.Handler.
func (l *Handler) BindFlags(cmd *cobra.Command) []string {
	cmd.Flags().StringVarP(&l.ConfigPath, "config", "c", "", "viper-readable config file")
	return []string{"config"}
}

// Run performs the sub-command logic.
//
// It implements cli/handler/cobra.Handler.
func (l *Handler) Run(ctx context.Context, input handler.Input) {
	err := l.run(input.Args[0])
	if err != nil {
		panic(err)
	}
}

func (l Handler) run(subjectPath string) error {
	subjectPath, err := filepath.Abs(subjectPath)
	if err != nil {
		return errors.Wrapf(err, "failed to get absolute path [%s]", subjectPath)
	}

	cfg, err := boone.ReadConfigFile(l.ConfigPath)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, target := range cfg.Target {
		globs, err := boone.GetTargetGlob(target.Include, target.Exclude)
		if err != nil {
			return errors.Wrapf(err, "[target: %s]: failed to get target globs", target.Label)
		}

		includes, err := boone.GetGlobInclude(globs)
		if err != nil {
			return errors.Wrapf(err, "[target: %s]: failed to get target includes", target.Label)
		}

		if len(includes) == 0 {
			continue
		}

		for p, i := range includes {
			if p == subjectPath {
				fmt.Printf("Matched with target [%s] glob [%s]\nTree:\n", target.Label, i.Pattern)
				for _, t := range target.Tree {
					fmt.Printf("\t- [%s]\n", t.Label)
				}
				return nil
			}
		}
	}

	fmt.Println("Target match not found")
	return nil
}

// New returns a cobra command instance based on Handler.
func NewCommand() *cobra.Command {
	return handler_cobra.NewHandler(&Handler{
		Session: &handler.DefaultSession{},
	})
}

var _ handler_cobra.Handler = (*Handler)(nil)
