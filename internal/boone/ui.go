// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone

import (
	"fmt"
	"time"

	tp_runes "github.com/codeactual/boone/internal/third_party/stackexchange/runes"

	"github.com/gdamore/tcell"
	"github.com/pkg/errors"
	"github.com/rivo/tview"
	"go.uber.org/zap"

	cage_zap "github.com/codeactual/boone/internal/cage/log/zap"
	cage_time "github.com/codeactual/boone/internal/cage/time"
)

const (
	// BodyBoxTopPad selects top-padding of ListItemWidget body areas.
	BodyBoxTopPad = 1

	// DetailListMaxLen is the static row length of the status-detail list.
	DetailListMaxLen = 3

	// DetailStderrPos positions standard error as the first status-detail list item.
	DetailStderrPos = 0

	// DetailStdoutPos positions standard error as the second status-detail list item.
	DetailStdoutPos = 1

	// DetailMiscPos positions misc. details as the third status-detail list item.
	DetailMiscPos = 2

	// ListItemWidgetPad is the all-sides padding of every ListItemWidget.
	ListItemWidgetPad = 1

	// StatusListMaxLen is the static row length of the status list.
	StatusListMaxLen = 9
)

// ListItemWidget is used to represent the status and status-detail lists.
type ListItemWidget struct {
	// Container is the flexible height/width box which bounds the Header and Body areas.
	Container *tview.Flex

	// Header areas are single-lined and display target labels/statuses/shortcuts in the status list,
	// and display detail types/shortcuts in the status-detail list.
	Header *tview.TextView

	// Body areas expand to use all space unused by the Header and display a snippet of standard error
	// in the status list, and display a detail snippets in the status-detail list.
	Body *tview.TextView
}

// NewListItemWidget returns a widget initialized with its container, header, and body areas.
func NewListItemWidget() *ListItemWidget {
	w := &ListItemWidget{}
	w.Container = tview.NewFlex()
	w.Container.SetDirection(tview.FlexRow)
	w.Container.SetBorderPadding(ListItemWidgetPad, ListItemWidgetPad, ListItemWidgetPad, ListItemWidgetPad)

	w.Header = tview.NewTextView()
	w.Header.SetWrap(true)
	w.Header.SetDynamicColors(true)

	w.Body = tview.NewTextView()
	w.Body.SetWrap(true)
	w.Body.SetDynamicColors(true)
	w.Body.SetBorderPadding(BodyBoxTopPad, 0, 0, 0)

	w.Container.AddItem(w.Header, 1, 0, false) // fixed height of 1
	w.Container.AddItem(w.Body, 0, 1, false)   // flexible height

	return w
}

// UI displays the status of targets which are scheduled, currently running, or have stopped due to an error.
// It maintains the data necessary to describe the target statuses based on channel messages from Dispatcher.
// It also responds to keyboard events in order to support screen navigation.
type UI struct {
	// log receives debug/info-level messages.
	log *zap.Logger

	// app is the top-level node which contains all widgets displayed in the UI.
	app *tview.Application

	// statusListWidget holds the list of statuses and is the widget shown at startup.
	//
	// Its contents are updated when the UI receives an Status or TargetPass over a channel.
	statusListWidget *tview.Flex

	// statusListItemWidget represents one status/item in statusListWidget.
	//
	// Its contents are updated when the UI receives an Status or TargetPass over a channel.
	statusListItemWidget [StatusListMaxLen]*ListItemWidget

	// detailListWidget holds the stderr/stdout/misc widgets and is shown when a specific
	// status/item in statusListWidget is selected via numbered keypress.
	//
	// Its contents are updated at keypress time.
	detailListWidget *tview.Flex

	// detailListItemWidget represent the individual stderr/stdout/misc widgets contained
	// inside detailListWidget.
	//
	// Its contents are updated at keypress time.
	detailListItemWidget [DetailListMaxLen]*ListItemWidget

	// exitCh lets UI communicate if Ctrl-C was captured.
	exitCh chan struct{}

	// sessionCh lets UI communicate its session state for saving it to disk.
	sessionCh chan Session

	// targetStartCh lets the UI add/replace list items for targets that are just starting.
	targetStartCh chan Status

	// targetPassCh lets the UI remove statuses which have been resolved and store run times.
	targetPassCh chan TargetPass

	// targetFailCh lets UI add/replace statuses which have been encountered.
	targetFailCh chan Status

	// statusList is the list most recently received over the resStatusList channel.
	//
	// It supports both the list and detail views.
	statusList []Status

	// activeWidget replaces use of tview.Box.HasFocus which is not predictable to know which
	// one is in the foreground (and there is no tview.Application.GetRoot or similar).
	activeWidget tview.Primitive

	// runLenHistory stores the duration of the target's last success, indexed by Target.Id.
	runLenHistory map[string]time.Duration
}

// ExitCh provides external listeners to know when the UI is shutting down based on a keyboard event.
func (u *UI) ExitCh() <-chan struct{} {
	return u.exitCh
}

// SessionCh provides external listeners to know when the newest session description is available
func (u *UI) SessionCh() <-chan Session {
	return u.sessionCh
}

// NewUI returns a UI instance configured to listen for status updates from the input channel.
func NewUI(log *zap.Logger, targetStartCh chan Status, targetPassCh chan TargetPass, targetFailCh chan Status, statusList []Status) *UI {
	return &UI{
		log:           log,
		targetStartCh: targetStartCh,
		targetPassCh:  targetPassCh,
		targetFailCh:  targetFailCh,
		exitCh:        make(chan struct{}, 1),
		sessionCh:     make(chan Session, 1),
		statusList:    statusList,
	}
}

// Init creates all the UI widgets and displays the status list.
func (u *UI) Init() {
	u.statusListWidget = tview.NewFlex()
	u.statusListWidget.SetDirection(tview.FlexRow)
	for pos := 0; pos < StatusListMaxLen; pos++ {
		u.statusListItemWidget[pos] = NewListItemWidget()
		u.statusListWidget.AddItem(u.statusListItemWidget[pos].Container, 0, 1, false)
	}
	u.statusListWidget.SetFullScreen(true)

	u.detailListWidget = tview.NewFlex()
	u.detailListWidget.SetDirection(tview.FlexRow)
	for pos := 0; pos < DetailListMaxLen; pos++ {
		u.detailListItemWidget[pos] = NewListItemWidget()
	}
	u.detailListWidget.AddItem(u.detailListItemWidget[DetailStderrPos].Container, 0, 1, false)
	u.detailListWidget.AddItem(u.detailListItemWidget[DetailStdoutPos].Container, 0, 1, false)
	u.detailListWidget.AddItem(u.detailListItemWidget[DetailMiscPos].Container, 0, 1, false)
	u.detailListWidget.SetFullScreen(true)

	u.app = tview.NewApplication().SetInputCapture(u.InputCapture)
	u.focusWidget(u.statusListWidget)

	u.runLenHistory = make(map[string]time.Duration)
}

// Start begins the goroutines which update the UI based on new data from a Dispatcher, periodically
// update the displayed relative times, and which render the UI.
//
// It blocks until the UI is exited via keyboard shortcut.
func (u *UI) Start() error {
	// potential race condition if widgets aren't expecting calls, e.g. AddItem, prior to Run
	go u.maintainStatusList()

	defer u.app.Stop() // ensure the terminal is cleaned up during panics (otherwise `reset` is needed)

	// support display of relative times
	go func() {
		u.renderStatusList()

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		tickerDone := make(chan struct{}, 1)
		for {
			select {
			case <-tickerDone:
				return
			case <-ticker.C:
				u.renderStatusList()
			}
		}
	}()

	if err := u.app.Run(); err != nil { // blocks on success due to tview's internal event loop
		return errors.Wrapf(err, "failed to init UI")
	}

	return nil
}

// Stop ends UI rendering and keyboard event capturing.
//
// It must be called to prevent corrupting the terminal such that `reset` is required. See tview's Fini
// function (https://github.com/rivo/tview/blob/11727c933b6d128d588006cc14105160c5413585/application.go#L90).
//
// It unblocks the goroutine which executes Start.
func (u *UI) Stop() {
	u.app.Stop()
}

// maintainStatusList add, replace, and remove statuses from the list data.
//
// It does not directly update the UI widgets which render the list data (see renderStatusList).
//
// It should run in its own goroutine because its for-select blocks.
func (u *UI) maintainStatusList() {
	insertItem := func(status Status) {
		// check if the target is already in the list
		targetPos := -1
		for pos, i := range u.statusList {
			if status.TargetId == i.TargetId {
				targetPos = pos
				break
			}
		}

		if targetPos == -1 {
			u.statusList = append([]Status{status}, u.statusList...) // prepend new item

			u.log.Info(
				"add target",
				cage_zap.Tag("ui"),
				zap.String("target", status.TargetLabel),
				zap.String("cause", string(status.Cause)),
			)
		} else {
			u.statusList[targetPos] = status // replace item, enforce policy of one item per target

			u.log.Info(
				"replace target (file activity)",
				cage_zap.Tag("ui"),
				zap.String("target", status.TargetLabel),
				zap.String("cause", string(status.Cause)),
			)
		}
		u.renderStatusList()
	}

	for {
		select {
		case status := <-u.targetStartCh:
			insertItem(status)
		case pass := <-u.targetPassCh:
			u.runLenHistory[pass.TargetId] = pass.RunLen

			foundPos := -1
			for pos, i := range u.statusList {
				if i.TargetId == pass.TargetId {
					foundPos = pos
					break
				}
			}
			if foundPos > -1 { // remove target's item, otherwise nothing to do
				var before, after []string
				removedTargetLabel := u.statusList[foundPos].TargetLabel
				for _, i := range u.statusList {
					before = append(before, i.TargetLabel+"/"+i.HandlerLabel)
				}
				u.statusList = append(u.statusList[:foundPos], u.statusList[foundPos+1:]...)
				for _, i := range u.statusList {
					after = append(after, i.TargetLabel+"/"+i.HandlerLabel)
				}
				u.log.Info(
					"removed target (pass)",
					cage_zap.Tag("ui"),
					zap.String("target", removedTargetLabel),
					zap.Int("foundPos", foundPos),
					zap.Strings("before", before),
					zap.Strings("after", after),
				)
				u.renderStatusList()
			}
		case status := <-u.targetFailCh:
			// If the target received file activity while it was running and the list was already updated
			// to reflect the pending state, retain that state to avoid it flipping from started to pending to failed.
			var pending bool
			for _, i := range u.statusList {
				if i.TargetId == status.TargetId && i.Cause == TargetPending {
					pending = true
				}
			}

			if !pending {
				insertItem(status)
			}
		}
	}
}

// renderStatusList complements maintainStatusList by rendering the current list data.
//
// It also sends Session messages in case the CLI is configured to write session files,
// aiming for those files to be as up-to-date as possible.
func (u *UI) renderStatusList() {
	u.app.QueueUpdateDraw(func() {
		listLen := len(u.statusList)

		u.log.Debug(
			"renderStatusList",
			cage_zap.Tag("ui"),
			zap.Int("listLen", listLen),
		)

		for pos := 0; pos < StatusListMaxLen; pos++ {
			if pos >= listLen {
				u.statusListItemWidget[pos].Header.SetText("")
				u.statusListItemWidget[pos].Body.SetText("")
				continue
			}

			status := u.statusList[pos]
			u.log.Debug(
				"render item",
				cage_zap.Tag("ui"),
				zap.String("target", status.TargetLabel),
				zap.String("handler", status.HandlerLabel),
			)

			if status.Cause == TargetStarted {
				var priorRunLenStr string
				priorRunLen, ok := u.runLenHistory[status.TargetId]
				if ok {
					priorRunLenStr = " | last took " + cage_time.DurationShort(priorRunLen)
				} else {
					priorRunLenStr = ""
				}

				var startTime string
				age := time.Since(status.StartTime)
				if age < time.Minute {
					startTime = "now"
				} else {
					startTime = cage_time.DurationShort(age) + " ago"
				}

				t := fmt.Sprintf( // Only use darkgray so it draws the eye less
					"[darkgray]%d) %s | %s | %s @ %s%s",
					pos+1, status.TargetLabel, status.HandlerLabel, status.Cause, startTime, priorRunLenStr,
				)

				u.statusListItemWidget[pos].Header.SetText(t)
				u.statusListItemWidget[pos].Body.SetText("")

				continue
			} else if status.Cause == TargetResumed {
				// Support case where handler label is empty, e.g. status was in a pending state
				// prior to shutdown and had not yet executed any handler.
				var resumeHandler string
				if status.HandlerLabel == "" {
					resumeHandler = " |"
				} else {
					resumeHandler = " | " + status.HandlerLabel + " |"
				}

				t := fmt.Sprintf( // Only use darkgray so it draws the eye less
					"[darkgray]%d) %s%s scheduled resume",
					pos+1, status.TargetLabel, resumeHandler,
				)

				u.statusListItemWidget[pos].Header.SetText(t)
				u.statusListItemWidget[pos].Body.SetText("")

				continue
			} else if status.Cause == TargetPending {
				t := fmt.Sprintf( // Only use darkgray so it draws the eye less
					"[darkgray]%d) %s | pending",
					pos+1, status.TargetLabel,
				)

				u.statusListItemWidget[pos].Header.SetText(t)
				u.statusListItemWidget[pos].Body.SetText("")

				continue
			}

			snip := status.Stderr
			if snip == "" {
				snip = status.Stdout
			}
			if snip == "" {
				snip = "<empty stdout/stderr>"
			}

			var endTime string
			age := time.Since(status.EndTime)
			if age < time.Minute {
				endTime = "now"
			} else {
				endTime = cage_time.DurationShort(age) + " ago"
			}

			header := fmt.Sprintf(
				"[darkgray]%d) [green]%s[white] | [darkgreen]%s[white] | [darkgray]%s after %s[lightgray] @ %s",
				pos+1, status.TargetLabel, status.HandlerLabel, status.Cause, cage_time.DurationShort(status.RunLen), endTime,
			)

			u.statusListItemWidget[pos].Header.SetText(header)
			u.statusListItemWidget[pos].Body.SetText(snip)
			u.statusListItemWidget[pos].Body.ScrollToEnd()
		}
	})

	u.sessionCh <- Session{Statuses: u.statusList}
}

// InputCapture listens for keyboard events from all screens.
func (u *UI) InputCapture(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlC || event.Rune() == 'q' { // Allow exit from anywhere
		u.exitCh <- struct{}{}
		return &tcell.EventKey{} // prevent tview from internally calling Stop on the app
	}

	switch u.activeWidget {
	case u.detailListWidget:
		// Use KeyBackSpace2 because KeyBackspace is actually Ctrl-H (https://github.com/gdamore/tcell/statuses/127)
		if event.Key() == tcell.KeyBackspace2 {
			u.focusWidget(u.statusListWidget)
			return event
		}

		pos, err := tp_runes.ToInt(event.Rune())

		if err == nil && pos > 0 && pos-1 < DetailListMaxLen {
			u.detailListItemWidget[pos-1].Body.ScrollToEnd()
			u.focusWidget(u.detailListItemWidget[pos-1].Body)
		}

		return event
	case u.statusListWidget:
		pos, err := tp_runes.ToInt(event.Rune())

		if err == nil && pos > 0 && pos-1 < len(u.statusList) {
			status := u.statusList[pos-1]

			if status.Cause == TargetStarted || status.Cause == TargetPending {
				return event
			}

			downstream := "<none>"
			if len(status.Downstream) > 0 {
				downstream = ""
				for _, label := range status.Downstream {
					downstream += "\n  - " + label
				}
			}

			u.detailListItemWidget[DetailStderrPos].Header.SetText(fmt.Sprintf(
				"[darkgray]1) [green]stderr[lightgray] (length: %d)",
				len(status.Stderr),
			))
			u.detailListItemWidget[DetailStderrPos].Body.SetText(status.Stderr)
			u.detailListItemWidget[DetailStderrPos].Body.ScrollToEnd()

			u.detailListItemWidget[DetailStdoutPos].Header.SetText(fmt.Sprintf(
				"[darkgray]2) [green]stdout[lightgray] (length: %d)",
				len(status.Stdout),
			))
			u.detailListItemWidget[DetailStdoutPos].Body.SetText(status.Stdout)
			u.detailListItemWidget[DetailStdoutPos].Body.ScrollToEnd()

			var upstream string
			if status.TargetLabel == status.UpstreamTargetLabel {
				upstream = "<none>"
			} else {
				upstream = status.UpstreamTargetLabel
			}

			u.detailListItemWidget[DetailMiscPos].Header.SetText("[darkgray]3) [green]more[lightgray]")
			u.detailListItemWidget[DetailMiscPos].Body.SetText(fmt.Sprintf(
				"- Error: %s\n"+
					"- Activity: %s (%s)\n"+
					"- Include: %s\n"+
					"- Upstream: %s\n"+
					"- Downstream: %s",
				status.Err,
				status.Path, status.Op,
				status.Include.Pattern,
				upstream,
				downstream,
			))
			u.detailListItemWidget[DetailMiscPos].Body.ScrollToBeginning()

			u.focusWidget(u.detailListWidget)
		}
		return event
	}

	// Use KeyBackSpace2 because KeyBackspace is actually Ctrl-H (https://github.com/gdamore/tcell/statuses/127)
	if event.Key() == tcell.KeyBackspace2 {
		for pos := 0; pos < DetailListMaxLen; pos++ {
			if u.activeWidget == u.detailListItemWidget[pos].Body {
				u.focusWidget(u.detailListWidget)
			}
		}
	}
	return event
}

// focusWidget selects a widget to display and listen to for keyboard events.
func (u *UI) focusWidget(w tview.Primitive) {
	u.app.SetRoot(w, true)
	u.activeWidget = w
}
