// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	tp_time "github.com/codeactual/boone/internal/third_party/gist.github.com/time"
	tp_sync "github.com/codeactual/boone/internal/third_party/github.com/sync"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	cage_zap "github.com/codeactual/boone/internal/cage/log/zap"
	cage_exec "github.com/codeactual/boone/internal/cage/os/exec"
	"github.com/codeactual/boone/internal/cage/os/file/watcher"
	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
	cage_shell "github.com/codeactual/boone/internal/cage/shell"
	cage_template "github.com/codeactual/boone/internal/cage/text/template"
	cage_time "github.com/codeactual/boone/internal/cage/time"
)

const (
	// ExecRequestQueueTick defines how often to dequeue exectution requests which have been
	// debounced and are ready to be fulfilled.
	ExecRequestQueueTick = time.Second
)

// TargetContext enables Dispatcher to cancel a target's command execution if its watched
// paths receive activity in the meantime, invalidating the current execution.
type TargetContext struct {
	// Ctx is initialized by Dispatcher when it starts the first handler.
	// If there are multiple handlers, they all share the same value in order
	// to allow Dispatcher to cancel the target regardless of which handler
	// is running.
	Ctx context.Context

	// Cancel is invoked to kill the running command.
	Cancel context.CancelFunc
}

// ExecRequest is a channel-sent request to run a target (and its downstream targets). Typically it orignates
// from a Watcher which detected target-specific activity, but it may also originate directly from
// sub-commands, e.g. to support AutoStartTarget.
type ExecRequest struct {
	// Origin is a free-form value, currently only for logging, which indicates the cause
	// of the request.
	Cause string

	// Debounce is how long to wait for file activity to stop before running the target.
	Debounce time.Duration

	// Event describes the filesystem operation which led to the request.
	Event watcher.Event

	// Include is the path pattern responsible for the Watcher capturing the activity.
	Include cage_filepath.Glob

	// RecvTime is when Dispatcher received the ExecRequest.
	//
	// It is used to cancel target runs before they start when the request has alraedy been sent to
	// Dispatcher.runTargetCh but should be ignored because a newer request was received in the meantime.
	RecvTime time.Time

	// Tree holds all targets downstream from the activity-triggered target.
	Tree []TargetTree

	// TargetId is a copy of the Id field of the activity-triggered Target.
	TargetId string

	// TargetLabel is a copy of the Label field of the activity-triggered Target.
	TargetLabel string
}

// Dispatcher receives ExecRequest messages from Watcher, runs/cancels target commands, and informs
// the UI of new target statuses via channels.
type Dispatcher struct {
	// Clock supports timer mocking for debounce-sensitive tests.
	Clock cage_time.Clock

	// Cooldown is how long to wait after one command finishes before starting another.
	Cooldown time.Duration

	// Executor supports os/exec.Cmd mocking for tests.
	Executor cage_exec.Executor

	// ExecReqCh receives requests from Watcher when a target has been triggered.
	// Sends are blocked only for as long as it takes to manually add them to a slice queue.
	ExecReqCh chan ExecRequest

	// Log receives debug/info-level messages.
	Log *zap.Logger

	// TreePassCh transports messages from the Dispatcher to the UI about the successful execution of all
	// commands of the activity-triggered target and all commands of downstream targets.
	TreePassCh chan TreePass

	// TargetStartCh transports messages from Dispatcher to the UI about newly pending targets and the start
	// of every executed command.
	TargetStartCh chan Status

	// TargetPassCh transports messages from the Dispatcher to the UI about the successful execution of all
	// commands of a single target.
	TargetPassCh chan TargetPass

	// TargetFailCh transports messages from the Dispatcher to the UI about the failed execution of a command.
	TargetFailCh chan Status

	// debouncedRunner indexes debounced version of Dispatcher.runTarget first (left-most) by target Id then path.
	// The target Id dimension is necessary to support shared paths across targets, and without per-path
	// granularity only the first path in a debounce "burst" will finally lead to an ExecRequest while
	// all others will get lost.
	debouncedRunner map[string]map[string]func(interface{})

	// done when closed will end the goroutine running Start and prevent new target invocations.
	// It is shared by both ExecReqCh and runTargetCh.
	done chan struct{}

	// runTargetCh periodically receieves requests in the same order as they were sent via ExecReqCh.
	// Sends are blocked until the all exec.Cmd in the tree exit.
	runTargetCh chan ExecRequest

	// targetCtx holds TargetContext values indexed by Target.Id.
	//
	// For data races between the goroutine in cage/time.Debounce and the one which runs Dispatcher methods.
	targetCtx sync.Map

	// panicCh transports messages from Watcher to the CLI to support cleaner shutdowns.
	panicCh chan<- interface{}
}

// Start debounces activity messages from Watcher, cancels in-progress commands if newer
// activity will make them redundant, and enqueues targets to run.
//
// It should run in its own goroutine because its for-select blocks.
func (d *Dispatcher) Start() {
	d.targetCtx = sync.Map{}
	d.done = make(chan struct{}, 1)
	d.runTargetCh = make(chan ExecRequest, 1)

	// queue allows channel-sends from the watcher to return immediately, activity-triggereed
	// cancellations to get processed mid-execution, and executions to happen in the same
	// order as requested from the watcher, by decoupling cancellation and execution
	// into separate for-select iterations.
	//
	// Trade-offs:
	//
	// - If an upstream A is requested immediately before downstream B, the downstream will
	//   run twice: once because it's in the tree of A, then one more time because B's execution
	//   request was in the queue next. Context-based cancellation of downstreams won't
	//   have any effect because the downstream B is not running yet and is queued in the
	//   channel.
	queue := tp_sync.NewSlice()
	ticker := time.NewTicker(ExecRequestQueueTick)

	reqLogAttrs := func(r ExecRequest) []zapcore.Field {
		return []zapcore.Field{
			cage_zap.Tag("dispatch"),
			zap.String("target", r.TargetLabel),
			zap.String("path", r.Event.Path),
			zap.String("op", r.Event.Op.String()),
			zap.String("cause", r.Cause),
			zap.Duration("debounce", r.Debounce),
		}
	}

	// Persistent goroutine 1 of 2: enqueue work from ExecRequest messages and cancel in-progress
	// (runTarget) work if present, and periodically send debounced requests to the 2nd persistent goroutine.

	go func() {
		for {
			select {
			case <-d.done:
				return
			case req := <-d.ExecReqCh:
				d.Log.Info("execution request", reqLogAttrs(req)...)

				req.RecvTime = time.Now()

				// If any target handler is currently in running, consider it stale and immediately cancel it.
				for _, t := range req.Tree {
					v, found := d.targetCtx.Load(t.Id)
					if found {
						c, ok := v.(TargetContext)
						if !ok {
							panic(errors.Errorf("failed to access context for target [%s]", t.Label))
						}
						d.Log.Info(
							"canceled target due to activity",
							cage_zap.Tag("dispatch"),
							zap.String("cancelledTarget", t.Label),
							zap.String("activatedTarget", req.TargetLabel),
						)
						c.Cancel()
					} else {
						d.Log.Debug(
							"no context found for activity cancellation",
							cage_zap.Tag("dispatch"),
							zap.String("cancelledTarget", t.Label),
							zap.String("activatedTarget", req.TargetLabel),
						)
					}
				}

				// If the target was queued out-of-band and was not in targetCtx for cancellationo above,
				// e.g. resumed at startup, then let it be replaced by this new request.
				var n int
				for item := range queue.Iter() {
					if item.Value.(ExecRequest).TargetId == req.TargetId {
						queue.Delete(n)
						break
					}
					n++
				}

				logAttrs := reqLogAttrs(req)

				enqueueStatus := func(queueItem ExecRequest) {
					d.Log.Info("enqueue, set pending", reqLogAttrs(queueItem)...)

					// Now that we know a target execution will happen after debounced, update the UI to reflect
					// the new state.
					pendingStatus := Status{
						TargetId:    queueItem.TargetId,
						TargetLabel: queueItem.TargetLabel,
						Cause:       TargetPending,
					}
					select { // Only send if there's a receiver.
					case d.TargetStartCh <- pendingStatus:
					default:
					}

					queue.Append(queueItem)
				}

				// If the target is configured to be debounced, only add it to the queue after requests "settle."
				if req.Debounce > 0 {
					if d.debouncedRunner == nil {
						d.debouncedRunner = make(map[string]map[string]func(interface{}))
					}

					if d.debouncedRunner[req.TargetId] == nil {
						d.debouncedRunner[req.TargetId] = make(map[string]func(interface{}))
					}

					if d.debouncedRunner[req.TargetId][req.Event.Path] == nil {
						d.debouncedRunner[req.TargetId][req.Event.Path] = tp_time.Debounce(d.Clock, req.Debounce, func(v interface{}) {
							d.Log.Debug("debounce settled", reqLogAttrs(v.(ExecRequest))...)

							enqueueStatus(v.(ExecRequest))
						})
					}

					d.Log.Debug("debounce reset", logAttrs...)

					d.debouncedRunner[req.TargetId][req.Event.Path](req)
				} else {
					enqueueStatus(req)
				}

			// Periodically check for work (requests that were enqueued after their debounces "settled").
			//
			// If work is found, effectively add it to another queue by sending it to the second persistent
			// goroutine via runTargetCh.
			//
			// This separation of work production (appending to the queue slice and sending runTargetCh
			// messages) from consumption (serialized execution of runTarget).

			case <-ticker.C:
				if first := queue.PopFirst(); first != nil {
					dequeued := first.(ExecRequest) //nolint:errcheck

					d.Log.Info("dequeue", reqLogAttrs(dequeued)...)

					go func(r ExecRequest) {
						d.runTargetCh <- r
					}(dequeued)
				}
			}
		}
	}()

	// Persistent goroutine 2 of 2: Execute runTarget with one request at a time.

	for {
		select {
		case <-d.done:
			return
		case req := <-d.runTargetCh:
			d.runTarget(req)
		}
	}
}

// Stop prevents the Dispatcher from receiving any more Watcher messages (and running any more targets),
// and kills the in-progress command if present.
func (d *Dispatcher) Stop() {
	close(d.done)

	d.targetCtx.Range(func(k, v interface{}) bool {
		c, ok := v.(TargetContext)
		if !ok {
			panic(errors.Errorf("failed to access target context for shutdown"))
		}
		c.Cancel()
		d.Log.Info(
			"canceled target due to shutdown",
			cage_zap.Tag("dispatch"),
			zap.String("targetId", fmt.Sprintf("%s", k)),
		)
		return false
	})
}

// runTarget attempts to execute all commands of the activity-triggered target and also the commands
// of all downstream targets (those configured with the activity-triggered Target.ID in the Upstream list).
//
// It communicates target statuses to the UI through channels.
func (d *Dispatcher) runTarget(v interface{}) {
	defer func() { // let higher-level logic recover from this panic-heavy function/goroutine
		if r := recover(); r != nil {
			select { // Only send if there's a receiver.
			case d.panicCh <- r:
			default:
			}
		}
	}()

	req := v.(ExecRequest) //nolint:errcheck

	treeLabels := []string{}
	for _, t := range req.Tree {
		treeLabels = append(treeLabels, t.Label)
	}

	d.Log.Info(
		"runTarget",
		cage_zap.Tag("dispatch"),
		zap.String("cause", req.Cause),
		zap.String("op", req.Event.Op.String()),
		zap.String("path", req.Event.Path),
		zap.String("target", req.TargetLabel),
		zap.Strings("tree", treeLabels),
	)

	// Use one context for all commands so that if any target in the tree receives activity, it will
	// cancel any further progress.
	//
	// This is based several assumptions:
	// - the cancellation has no effect on targets/handlers that already executed earlier in the loop
	// - same lack of effect on ones that haven't executed yet
	// - and any target (not just the one that was triggered initially) may receive activity mid-tree
	treeCtx, treeCancel := context.WithCancel(context.Background())
	defer treeCancel()

	for _, t := range req.Tree {
		targetStartTime := time.Now()

		// Allow activity on any target to cancel the tree as a whole. See comments above
		// where treeCtx/treeCancel are initialized.
		//
		// Delete the context once all handlers of this target have finished (successfully or not).
		// If it runs again, we can just create a new one with a fresh timeout clock.
		d.targetCtx.Store(t.Id, TargetContext{Ctx: treeCtx, Cancel: treeCancel})
		defer d.targetCtx.Delete(t.Id)

		for _, handler := range t.Handler {
			for _, e := range handler.Exec {
				tmplData := CmdTemplateData{
					Dir:          filepath.Dir(req.Event.Path),
					HandlerLabel: handler.Label,
					IncludeGlob:  req.Include.Pattern,
					IncludeRoot:  req.Include.Root,
					Path:         req.Event.Path,
					TargetLabel:  t.Label,
				}

				cmdBuf, err := cage_template.ExecuteBuffered(e.Cmd, tmplData)
				if err != nil {
					panic(errors.Wrapf(err, "failed to expand target [%s] command [%s]", t.Label, e.Cmd))
				}
				cmdExpanded := cmdBuf.String()

				cmdParsed, err := cage_shell.Parse(cmdExpanded)
				if err != nil {
					panic(errors.Wrapf(err, "failed to parse target[%s] command [%s]", t.Label, cmdExpanded))
				}

				// Add a timeout just for this command (not the target/tree as a whole).
				//
				// cmdCancel is not used because treeCancel provides the same functionality for
				// cancelling at the target level based on file activity.
				cmdCtx, cmdCancel := context.WithTimeout(treeCtx, e.timeout)
				defer cmdCancel()

				cmds := cage_exec.ArgToCmd(cmdCtx, cmdParsed...)

				cmdStrs := []string{}
				for _, cmd := range cmds {
					cmd.Env = append(os.Environ(), e.Env...)
					cmd.Dir = e.Dir

					cmdStrs = append(cmdStrs, cage_exec.CmdToString(cmd))
				}

				d.Log.Info(
					"starting handler command",
					cage_zap.Tag("dispatch"),
					zap.String("target", t.Label),
					zap.String("dispatchTarget", req.TargetLabel),
					zap.String("handler", handler.Label),
					zap.String("cmdExpanded", cmdExpanded),
					zap.Strings("cmdStrs", cmdStrs),
				)

				cmdStartTime := time.Now()
				select { // Only send if there's a receiver.
				case d.TargetStartCh <- Status{TargetId: t.Id, TargetLabel: t.Label, HandlerLabel: handler.Label, Path: req.Event.Path, StartTime: cmdStartTime, Cause: TargetStarted}:
				default:
				}
				stdout, stderr, res, err := d.Executor.Buffered(cmdCtx, cmds...)

				ctxErr := cmdCtx.Err()
				if ctxErr != nil {
					err = ctxErr
				}

				pids := []int{}
				pgids := []int{}
				codes := []int{}
				errs := []error{}
				for _, cmd := range cmds {
					pids = append(pids, res.Cmd[cmd].Pid)
					pgids = append(pgids, res.Cmd[cmd].Pgid)
					codes = append(codes, res.Cmd[cmd].Code)
					errs = append(errs, res.Cmd[cmd].Err)
				}

				d.Log.Info(
					"handler command finished",
					cage_zap.Tag("dispatch"),
					zap.String("target", t.Label),
					zap.String("dispatchTarget", req.TargetLabel),
					zap.String("handler", handler.Label),
					zap.String("cmdExpanded", cmdExpanded),
					zap.Strings("cmdStrs", cmdStrs),
					zap.String("stdout", stdout.String()),
					zap.String("stderr", stderr.String()),
					zap.String("runLen", time.Since(cmdStartTime).String()),
					zap.Ints("pids", pids),
					zap.Ints("pgids", pgids),
					zap.Ints("codes", codes),
					zap.Errors("processErrs", errs),
					zap.Error(err),
				)

				if err != nil {
					downLabels := []string{}
					for n, d := range req.Tree {
						if n > 0 {
							downLabels = append(downLabels, d.Label)
						}
					}

					cause := TargetFailed
					if ctxErr != nil {
						cause = TargetCanceled
					}

					status := Status{
						Cmd:                 cmdExpanded,
						Stdout:              stdout.String(),
						Stderr:              stderr.String(),
						Err:                 err.Error(),
						Cause:               cause,
						StartTime:           cmdStartTime,
						EndTime:             time.Now(),
						Pid:                 pids,
						RunLen:              time.Since(cmdStartTime),
						Include:             req.Include,
						TargetId:            t.Id,
						TargetLabel:         t.Label,
						HandlerLabel:        handler.Label,
						UpstreamTargetLabel: req.TargetLabel,
						Op:                  req.Event.Op.String(),
						Downstream:          downLabels,
					}

					select {
					case d.TargetFailCh <- status:
					default:
					}

					return // Only expose one problem per Target to the user
				}

				time.Sleep(d.Cooldown)
			}
		}

		select {
		case d.TargetPassCh <- TargetPass{TargetId: t.Id, RunLen: time.Since(targetStartTime)}:
		default:
		}
	}

	select {
	case d.TreePassCh <- TreePass{DispatchTargetId: req.TargetId}:
	default:
	}
}

// NewDispatcher returns a new instance which is already watching for writes to targets' configured
// paths and sending messages to its channels about target run starts, failures, etc.
func NewDispatcher(log *zap.Logger, targets []Target, panicCh chan interface{}, globalConfig GlobalConfig) (*Dispatcher, error) {
	execReqCh := make(chan ExecRequest, 1)
	targetStartCh := make(chan Status, 1)
	targetPassCh := make(chan TargetPass, 1)
	targetFailCh := make(chan Status, 1)
	treePassCh := make(chan TreePass, 1)

	for _, target := range targets {
		var err error

		globs, err := GetTargetGlob(target.Include, target.Exclude)
		if err != nil {
			return nil, errors.Wrapf(err, "[target: %s]: failed to get target globs", target.Label)
		}

		includes, err := GetGlobInclude(globs)
		if err != nil {
			return nil, errors.Wrapf(err, "[target: %s]: failed to get target includes", target.Label)
		}

		if len(includes) > 0 {
			fsnotify := new(watcher.Fsnotify)
			fsnotify.Debounce(PreDebounce)

			watch := Watcher{
				PanicCh:   panicCh,
				ExecReqCh: execReqCh,
				Target:    target,
				Watcher:   fsnotify,
				Log:       log,
			}
			watch.SetInclude(includes)

			watcherErr := fsnotify.AddSubscriber(&watch)
			if watcherErr != nil {
				return nil, errors.Wrapf(watcherErr, "[target: %s]: failed to configure watcher", target.Label)
			}

			for p := range includes {
				pathErr := watch.AddPath(p)
				if pathErr != nil {
					return nil, errors.Wrapf(pathErr, "[target: %s]: failed to watch path [%s]", target.Label, p)
				}

				log.Info(
					"added watch",
					cage_zap.Tag("init"),
					zap.String("target", target.Label),
					zap.String("path", p),
				)
			}
		} else { // support targets that only execute via "run" sub-command, e.g. for vim post-install
			log.Debug(
				"no includes, skipped watcher creation",
				cage_zap.Tag("init"),
				zap.String("target", target.Label),
			)
		}
	}

	return &Dispatcher{
		Clock:         cage_time.RealClock{},
		Cooldown:      globalConfig.GetCooldown(),
		Executor:      cage_exec.CommonExecutor{},
		Log:           log,
		ExecReqCh:     execReqCh,
		TargetStartCh: targetStartCh,
		TargetPassCh:  targetPassCh,
		TargetFailCh:  targetFailCh,
		TreePassCh:    treePassCh,
		panicCh:       panicCh,
	}, nil
}
