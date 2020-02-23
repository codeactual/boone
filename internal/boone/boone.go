// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package boone provides mechanisms for configuration, file-activity monitoring, command execution, and UI.
package boone

import (
	"time"

	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
)

// TargetStatus explains why a target is listed in the UI on the initial screen.
type TargetStatus string

const (
	// PreDebounce prevents cage/os/file/watcher.Fsnotify from sending duplicate events,
	// in some situations, when both a file and its directory are watched.
	//
	// This value was selected because it's assumed to be long enough to capture all the
	// duplicates and less than user-selected per-Target debounce values.
	PreDebounce = 500 * time.Millisecond

	// SessionVersion is included in the encoded Session file to support potential compatibility work.
	SessionVersion = 1

	// Target
	TargetCanceled TargetStatus = "canceled"

	// TargetFailed indicates a target command returned a non-zero exit code and the Dispatcher
	// will not proceed any further with that target until it is activated again.
	TargetFailed TargetStatus = "failed"

	// TargetPending indicates the target's latest file activity has been debounced, the target
	// has been enqueued to run, and it is waiting to start.
	TargetPending TargetStatus = "pending"

	// TargetResumed indicates the program saved a TargetStarted-status target in its session file
	// (if configured) at shutdown, then enqueued it during startup.
	TargetResumed TargetStatus = "resumed"

	// TargetStarted indicates a Dispatcher has started running the target's command(s).
	TargetStarted TargetStatus = "started"
)

// Handler defines one or more commands that must execute in response to a target trigger.
type Handler struct {
	// Label is displayed to users in output for reference/debugging/etc. and also
	// provides a documentation in the config file on the intent.
	//
	// It is a required field.
	Label string

	// Exec defines the commands to execute.
	Exec []Exec
}

// Exec defines what command to run and how to run it.
type Exec struct {
	// Cmd holds a single command or multiple commands in a "|" pipeline.
	Cmd string

	// Dir is the working directory.
	//
	// It is relative to Target.Root and Target.Root by default.
	Dir string

	// Timeout is a time.Duration compatible string from the config file that defines
	// how long to wait before cancelling the command.
	Timeout string

	// Env holds "KEY=VALUE" pairs to overwrite in the current environment.
	Env []string

	// timeout is the parsed version of Timeout.
	timeout time.Duration
}

// GetTimeout returns the parsed value of Timeout.
func (e Exec) GetTimeout() time.Duration {
	return e.timeout
}

// Status describes a target listed in the UI on its initial screen.
type Status struct {
	// Cause explains why the status is in the list.
	Cause TargetStatus

	// Cmd was the final command string after template expansion.
	Cmd string

	// Downstream holds labels of all downstream targets included in the run.
	Downstream []string

	// EndTime is when the Cmd finished.
	EndTime time.Time

	// Err is non-nil if Cmd failed.
	Err string

	// HandlerLabel is from the source of the status.
	HandlerLabel string

	// Include is the one responsible for the capturing the file activity which led to running the target.
	Include cage_filepath.Glob

	// Op is the type of filesystem operation which led to target execution.
	//
	// It is "Create", "Rename", or "Write"
	Op string

	// Path identifies the file whose Op activity triggered the target.
	Path string

	// Pid is the process Id of Cmd.
	Pid []int

	// RunLen is how long Cmd ran.
	RunLen time.Duration

	// StartTime is when Cmd started.
	StartTime time.Time

	// Stderr is collected from Cmd exceution.
	Stderr string

	// Stdout is collected from Cmd exceution.
	Stdout string

	// TargetId is from the source of the status.
	TargetId string

	// TargetLabel is from the source of the status.
	TargetLabel string

	// UpstreamTargetLabel is the one whose activity triggered the handler execution flow
	// that may include one or more (of its) downstream targets. If there were no
	// downstream targets, it should equal the Target field.
	UpstreamTargetLabel string
}

// TargetPass describes a target whose commands all finished successfully.
type TargetPass struct {
	// RunLen is how long it took to run a target's command list.
	RunLen time.Duration

	// TargetId is a copy of Target.Id.
	TargetId string
}

// TreePass describes a set of targets (activity-triggered target and all its downstream targets) whose commands
// all finished successfully.
type TreePass struct {
	// DispatchTargetId is the first target in the tree and whose activity led to the tree execution.
	DispatchTargetId string
}

// Session is written to file periodically to support resumption of targets which were pending/running, and
// tracking unresolved target failures.
type Session struct {
	// Statuses holds one Status per Target which was displayed in the UI when the Session value is
	// created.
	Statuses []Status

	// Version is a copy of the SessionVersion constant when the Session value is created.
	Version int
}

// CmdTemplateData describes the built-in template variables available in Target.Handler.Exec.Cmd config strings.
type CmdTemplateData struct {
	// Dir is the absolute path of the parent directory of Path.
	Dir string

	// HandlerLabel is a copy of Target.Handler.Label.
	HandlerLabel string

	// IncludeGlob pattern from the config file Include responsible for the command being triggered.
	IncludeGlob string

	// IncludeRoot is the ancestor root directory from the config file Include responsible for the
	// command being triggered.
	IncludeRoot string

	// Path is the absolute path of the file/directory that was created or written to.
	Path string

	// TargetLabel is a copy of Target.Label.
	TargetLabel string
}
