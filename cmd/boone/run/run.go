// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Sub-command run executes the handlers configured for the input target. It supports
// testing of target configuration, similar to the "eval" sub-command, and also reuse of the
// handler sequence by offering a way to trigger its execution on-demand instead of after
// file activity.
//
// Usage:
//
//	boone run --config /path/to/config target_id_from_config
package run

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tp_time "github.com/codeactual/boone/internal/third_party/blog.sgmansfield.com/time"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/codeactual/boone/internal/boone"
	"github.com/codeactual/boone/internal/cage/cli/handler"
	handler_cobra "github.com/codeactual/boone/internal/cage/cli/handler/cobra"
	log_zap "github.com/codeactual/boone/internal/cage/cli/handler/mixin/log/zap"
	cage_exec "github.com/codeactual/boone/internal/cage/os/exec"
)

// Handler defines the sub-command flags and logic.
type Handler struct {
	handler.Session

	ConfigPath string

	Log *log_zap.Mixin
}

// Init defines the command, its environment variable prefix, etc.
//
// It implements cli/handler/cobra.Handler.
func (h *Handler) Init() handler_cobra.Init {
	h.Log = &log_zap.Mixin{}
	return handler_cobra.Init{
		Cmd: &cobra.Command{
			Use:   "run",
			Short: "Run a target",
			Example: strings.Join([]string{
				"boone run --config /path/to/config target_id_from_config",
			}, "\n"),
		},
		EnvPrefix: "BOONE",
		Mixins: []handler.Mixin{
			h.Log,
		},
	}
}

// BindFlags binds the flags to Handler fields.
//
// It implements cli/handler/cobra.Handler.
func (h *Handler) BindFlags(cmd *cobra.Command) []string {
	cmd.Flags().StringVarP(&h.ConfigPath, "config", "c", "", "viper-readable config file")
	return []string{"config"}
}

// Run performs the sub-command logic.
//
// It implements cli/handler/cobra.Handler.
func (h *Handler) Run(ctx context.Context, input handler.Input) {
	err := h.run(input.Args[0])
	if err != nil {
		panic(err)
	}
}

func (h Handler) run(id string) error {
	cfg, err := boone.ReadConfigFile(h.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config file [%s]: %s\n", h.ConfigPath, err)
		os.Exit(1)
	}

	var match *boone.Target
	for t, target := range cfg.Target {
		if target.Id == id {
			match = &cfg.Target[t]
			break
		}
	}
	if match == nil {
		fmt.Printf("Target with Id [%s] not found", id)
		return nil
	}

	dispatcher, err := boone.NewDispatcher(h.Log.Logger, cfg.Target, nil, cfg.Global)
	if err != nil {
		h.Log.Error("failed to init from config", zap.Error(err))
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)
	go func() {
		sig := <-sigCh
		dispatcher.Stop()
		fmt.Printf("Received signal (%v).\n", sig)

		fmt.Printf("Waiting %d seconds for processes to shutdown.", cage_exec.SigKillDelay/time.Second)
		time.Sleep(cage_exec.SigKillDelay)
		os.Exit(0)
	}()

	// It's assumed that it's OK if the dispatcher starts after one or more Watcher
	// events have been sent over execReqCh because those writes will simply block until the
	// dispatcher starts reading them.
	go dispatcher.Start()

	go func() {
		<-dispatcher.TreePassCh
		os.Exit(0)
	}()

	go func() {
		status := <-dispatcher.TargetFailCh
		fmt.Fprintf(os.Stderr, "run failed on command:\n%s", status.Cmd)
		if len(status.Stdout) > 0 {
			fmt.Fprintf(os.Stderr, "\n\nlast stdout:\n%s", status.Stdout)
		}
		if len(status.Stderr) > 0 {
			fmt.Fprintf(os.Stderr, "\n\nlast stderr:\n%s", status.Stderr)
		}
		if status.Err != "" {
			os.Exit(1)
		}
		os.Exit(0)
	}()

	dispatcher.ExecReqCh <- boone.ExecRequest{
		Cause:       "run",
		TargetId:    match.Id,
		TargetLabel: match.Label,
		Tree:        append([]boone.TargetTree{}, match.Tree...),
	}

	tp_time.SleepForever()

	return nil
}

// New returns a cobra command instance based on Handler.
func NewCommand() *cobra.Command {
	return handler_cobra.NewHandler(&Handler{
		Session: &handler.DefaultSession{},
	})
}

var _ handler_cobra.Handler = (*Handler)(nil)
