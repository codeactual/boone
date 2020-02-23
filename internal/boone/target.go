// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone

import (
	"time"

	"github.com/pkg/errors"

	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
	cage_template "github.com/codeactual/boone/internal/cage/text/template"
)

// TargetTree is a limited copy of Target fields which describe a single downstream target.
//
// It is used in Target as an abbreviated inventory of which targets should also run
// after the upstream target finishes.
type TargetTree struct {
	Id      string
	Label   string
	Handler []Handler
}

// Target defines upstream-target and/or filesystem triggers, and the handlers
// to run as a result.
//
// Upstream Target.Id values will be stored in an app-level map instead of this type.
type Target struct {
	// Debounce is a time.Duration compatible string from the config file that defines
	// how long to wait after file activity settles before executing handlers.
	Debounce string

	// Downstream holds all direct descendants.
	//
	// It is generated at startup.
	Downstream []*Target

	// Exclude defines the path patterns of files/directories which should invalidate an Include match.
	Exclude []cage_filepath.Glob

	// Handler defines the commands to run if the target is triggered by write-activity.
	Handler []Handler

	// Id is user-defined, ideally short, and must be unique in the config file.
	//
	// It is an optional field and supports features like upstream-target triggers.
	Id string

	// Include defines the path patterns of files/directories whose write-activity can trigger this target.
	Include []cage_filepath.Glob

	// Label is displayed to users in output for reference/debugging/etc. and also
	// provides a documentation in the config file on the intent.
	//
	// It is a required field.
	Label string

	// Root is the default path prefix value for Include.Root fields.
	Root string

	// Tree holds one item per Target which Dispatcher should execute when this Target is
	// triggered. It includes ths Target in the first item, followed by all downstream
	// targets found recursively.
	//
	// It only holds the minimum details of each target in order to avoid data races,
	// e.g. that might happen with a map of Target/*Target.
	//
	// It is generated at startup.
	Tree []TargetTree

	// Upstream holds Id values of targets that, when triggered, also trigger this target.
	Upstream []string

	// debounce is the parsed version of Debounce.
	debounce time.Duration
}

// GetDebounce returns the parsed version of Debounce.
func (t Target) GetDebounce() time.Duration {
	return t.debounce
}

// MatchPath checks if the input path matches one of the target's inclusion patterns and no
// exclusion pattern.
func (t *Target) MatchPath(name string) (cage_filepath.MatchAnyOutput, error) {
	var include, exclude []string
	for _, i := range t.Include {
		include = append(include, i.Pattern)
	}
	for _, e := range t.Exclude {
		exclude = append(exclude, e.Pattern)
	}
	res, err := cage_filepath.PathMatchAny(cage_filepath.MatchAnyInput{
		Name:    name,
		Include: include,
		Exclude: exclude,
	})
	if err != nil {
		return cage_filepath.MatchAnyOutput{}, errors.Wrapf(err, "failed to match target [%s] to path [%s]", t.Label, name)
	}
	return res, nil
}

// ExpandTemplateVars updates Target configuration string fields by expanding template variables
// with associated input values.
func (t *Target) ExpandTemplateVars(data map[string]string) error {
	targetStrings := []*string{
		&t.Debounce,
		&t.Root,
	}
	for h, handler := range t.Handler {
		for e := range handler.Exec {
			targetStrings = append(
				targetStrings,
				&t.Handler[h].Exec[e].Cmd,
				&t.Handler[h].Exec[e].Dir,
				&t.Handler[h].Exec[e].Timeout,
			)
		}
	}
	return cage_template.ExpandFromStringMap(data, targetStrings...)
}

// ContainsDownstream returns true if a target is found downstream recursively.
func (t *Target) ContainsDownstream(targetId string) (found bool) {
	_ = VisitDownstream(t, func(downstream *Target) error {
		if targetId == downstream.Id {
			found = true
		}
		return nil
	})
	return found
}

// VisitDownstream calls the visitor with all targets found downstream recursively.
func VisitDownstream(t *Target, visit func(t *Target) error) (err error) {
	for _, d := range t.Downstream {
		if err = visit(d); err != nil { // begin halt of entire walk
			return err
		}
		err = VisitDownstream(d, visit) // propagate above halt
		if err != nil {
			return err
		}
	}
	return nil
}
