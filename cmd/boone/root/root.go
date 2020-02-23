// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Root command boone starts the UI and begins monitoring configured paths.
//
// Usage:
//
//	boone --config /path/to/config
package root

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/codeactual/boone/internal/boone"
	"github.com/codeactual/boone/internal/cage/cli/handler"
	handler_cobra "github.com/codeactual/boone/internal/cage/cli/handler/cobra"
	log_zap "github.com/codeactual/boone/internal/cage/cli/handler/mixin/log/zap"
	cage_gob "github.com/codeactual/boone/internal/cage/encoding/gob"
	cage_zap "github.com/codeactual/boone/internal/cage/log/zap"
	cage_exec "github.com/codeactual/boone/internal/cage/os/exec"
	cage_file "github.com/codeactual/boone/internal/cage/os/file"
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
			Use:   "boone",
			Short: "Start monitoring",
			Example: strings.Join([]string{
				"boone --config /path/to/config",
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
func (l *Handler) BindFlags(cmd *cobra.Command) []string {
	cmd.Flags().StringVarP(&l.ConfigPath, "config", "c", "", "viper-readable config file")
	return []string{"config"}
}

// Run performs the sub-command logic.
//
// It implements cli/handler/cobra.Handler.
func (h *Handler) Run(ctx context.Context, input handler.Input) {
	cfg, err := boone.ReadConfigFile(h.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config file [%s]: %s\n", h.ConfigPath, err)
		os.Exit(1)
	}

	panicCh := make(chan interface{}, 1)

	dispatcher, err := boone.NewDispatcher(h.Log.Logger, cfg.Target, panicCh, cfg.Global)
	if err != nil {
		h.Log.Error("failed to init from config", zap.Error(err))
		os.Exit(1)
	}

	var resumeExecReq []boone.ExecRequest

	// Seed the UI with statuses from the prior session if it exists, pruning statuses from targets
	// that are not in the current config.
	var seedStatusList []boone.Status

	if cfg.Data.Session.File != "" {
		exists, fi, err := cage_file.Exists(cfg.Data.Session.File)
		if err != nil {
			panic(errors.Wrapf(err, "failed to inspect session file"))
		}
		if exists && fi.Size() > 0 {
			var sessionTarget []string

			dec, decodeErr := cage_gob.DecodeFromFile(cfg.Data.Session.File)
			if decodeErr != nil {
				panic(errors.Wrapf(decodeErr, "failed to create session file decoder"))
			}

			var decSession boone.Session
			if decodeErr = dec.Decode(&decSession); decodeErr != nil {
				panic(errors.Wrapf(decodeErr, "failed to decode session file"))
			}

			h.Log.Debug(
				"decoded session",
				zap.Namespace("session"),
				zap.Int("version", decSession.Version),
				zap.Int("statusesLen", len(decSession.Statuses)),
			)

			for _, status := range decSession.Statuses {
				// Handle case where status was resumed in a prior session but never executed because the program shutdown,
				// or pending but not yet started before the shutdown.
				//
				// Switch the cause back to TargetStarted so the rest of the logic treats the status like it's the first time.
				if status.Cause == boone.TargetResumed || status.Cause == boone.TargetPending {
					status.Cause = boone.TargetStarted
				}

				var valid bool
				for _, t := range cfg.Target {
					if t.Id == status.TargetId {
						valid = true
						if status.Cause == boone.TargetStarted { // Resume targets canceled by shutdown.
							status.Cause = boone.TargetResumed

							h.Log.Info("resume scheduled", zap.String("targetId", status.TargetId))

							resumeExecReq = append(resumeExecReq, boone.ExecRequest{
								Cause:       "resume",
								TargetId:    status.TargetId,
								TargetLabel: status.TargetLabel,
								Tree:        append([]boone.TargetTree{}, t.Tree...),
							})
						}
						break
					}
				}
				if valid {
					sessionTarget = append(sessionTarget, status.TargetLabel)
					seedStatusList = append(seedStatusList, status)
				} else {
					h.Log.Debug("prune unknown target before session resume", zap.String("targetId", status.TargetId))
				}
			}
			h.Log.Info("resuming session", zap.Strings("targets", sessionTarget))
		}
	}

	ui := boone.NewUI(h.Log.Logger, dispatcher.TargetStartCh, dispatcher.TargetPassCh, dispatcher.TargetFailCh, seedStatusList)
	ui.Init()

	shutdown := func() {
		dispatcher.Stop()
		ui.Stop()
	}

	go func() {
		for {
			select {
			case r := <-panicCh:
				shutdown()
				fmt.Printf("panic from watcher: %+v\n", r)
			case session := <-ui.SessionCh():
				var sessionTarget []string
				for _, status := range session.Statuses {
					sessionTarget = append(sessionTarget, status.TargetLabel)
				}
				if encodeErr := cage_gob.EncodeToFile(cfg.Data.Session.File, session); encodeErr != nil {
					h.Log.Error(
						"failed to encode session file",
						cage_zap.Tag("root"),
						zap.Error(encodeErr),
					)
				} else {
					h.Log.Debug("saving session", zap.Strings("targets", sessionTarget))
				}
			case <-ui.ExitCh():
				shutdown()

				// End the loop to avoid a semantic race: prevent the UI from updating the session, in the above case,
				// after the dispatcher notifies the UI of the canceled status(s). Without this prevention,
				// the session may include in-progress targets as failed (Cause=TargetFailed/TargetCanceled) instead of
				// running (Cause=TargetStarted). If that happens then the targets will not be restarted due to the
				// restart logic that relies on Cause values.
				return
			}
		}
	}()

	go dispatcher.Start()

	go func() {
		for _, t := range cfg.GetStartTarget() {
			dispatcher.ExecReqCh <- boone.ExecRequest{
				Cause:       "start",
				TargetId:    t.Id,
				TargetLabel: t.Label,
				Tree:        append([]boone.TargetTree{}, t.Tree...),
			}
		}
		for _, r := range resumeExecReq {
			dispatcher.ExecReqCh <- r
		}
	}()

	shutdownOnSignal := func(s os.Signal) {
		shutdown()
		fmt.Printf("Received signal (%v).\n", s) // after shutdown to allow tview to clean up the term
	}
	h.OnSignal(syscall.SIGTERM, shutdownOnSignal)
	h.OnSignal(syscall.SIGINT, shutdownOnSignal)

	err = ui.Start() // blocks on success due to tview's internal event loop
	if err != nil {
		h.Log.Error("failed to start UI", zap.Error(err))
		os.Exit(1)
	}

	fmt.Printf("Waiting %d seconds for processes to shutdown.", cage_exec.SigKillDelay/time.Second)
	time.Sleep(cage_exec.SigKillDelay)
}

// New returns a cobra command instance based on Handler.
func NewCommand() *cobra.Command {
	return handler_cobra.NewHandler(&Handler{
		Session: &handler.DefaultSession{},
	})
}

var _ handler_cobra.Handler = (*Handler)(nil)
