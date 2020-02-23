// Copyright (C) 2020 The boone Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package boone

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	std_viper "github.com/spf13/viper"

	cage_viper "github.com/codeactual/boone/internal/cage/config/viper"
	cage_io "github.com/codeactual/boone/internal/cage/io"
	cage_file "github.com/codeactual/boone/internal/cage/os/file"
	cage_filepath "github.com/codeactual/boone/internal/cage/path/filepath"
	cage_structs "github.com/codeactual/boone/internal/cage/structs"
	cage_template "github.com/codeactual/boone/internal/cage/text/template"
)

const (
	// DefaultDebounce is the default Target.Debounce value.
	DefaultDebounce = "15s"

	// DefaultTimeout is the default Target.Handler.Exec.Timeout value.
	DefaultCmdTimeout = "15m"

	// DefaultCooldown is the default Global value.
	DefaultCooldown = "5s"

	// dataDirPerm is the default permissions granted for new directories.
	dataDirPerm = 0700

	// dataFilePerm is the default permissions granted for new files.
	dataFilePerm = 0600
)

// SessionConfig defines how to store sessions.
//
// Its config section is Data.Session.
type SessionConfig struct {
	File string
}

// DataConfig defines how to store program state.
//
// Its config section is Data.
type DataConfig struct {
	// Session defines how to store sessions.
	Session SessionConfig
}

// Config defines the structure of a config file.
type Config struct {
	// Data defines how to store program state.
	Data DataConfig

	// Global defines properties which should be applied to all targets, e.g. as default values.
	Global GlobalConfig

	// Target defines file/directory paths to watch and commands to run when they receive writes.
	Target []Target

	// Template holds key/value pairs which can be used in some string fields via {{.key_name}} syntax.
	//
	// Key names must use lowercase due to viper(/mapstructure?) limitation. Convention: "some_key_name".
	// https://github.com/spf13/viper/issues/411
	// https://github.com/spf13/viper/pull/635
	Template map[string]string

	// AutoStartTarget holds an Id for each Target that should run when the main process starts.
	AutoStartTarget []string

	// startTarget is computed and populated by AutoStartTarget selections.
	startTarget []Target
}

// GetStartTarget returns all targets selected in the AutoStartTarget config.
func (c *Config) GetStartTarget() (t []Target) {
	t = make([]Target, len(c.startTarget))
	copy(t, c.startTarget)
	return t
}

// GlobalConfig defines properties which should be applied to all targets, e.g. as default values.
type GlobalConfig struct {
	// Cooldown is a time.Duration compatible string which selects how long to wait after one command
	// finishes before starting another.
	Cooldown string

	// Exclude are appended to every Target.Exclude list.
	Exclude []cage_filepath.Glob

	// cooldown is converted from Cooldown.
	cooldown time.Duration
}

// GetCooldown returns the converted value of Cooldown.
func (c GlobalConfig) GetCooldown() time.Duration {
	return c.cooldown
}

// ReadConfigFile converts a file to a Config value.
func ReadConfigFile(name string) (c Config, err error) {
	file := std_viper.New()
	if err = cage_viper.ReadInConfig(file, name); err != nil {
		return Config{}, errors.Wrapf(err, "failed to read config file [%s]", name)
	}

	// https://github.com/mitchellh/mapstructure struct tags, e.g. `mapstructure:"path_map"` from
	// the viper README.md, are currently unnecessary becaue the struct field naming matches
	// the YAML naming 1:1.
	err = file.Unmarshal(&c)
	if err != nil {
		return Config{}, errors.Wrapf(err, "failed to unmarshal config from file [%s]", name)
	}

	allTargets := []*Target{}
	for k := range c.Target {
		allTargets = append(allTargets, &c.Target[k])
	}

	err = FinalizeConfig(allTargets, &c)
	if err != nil {
		return Config{}, errors.WithStack(err)
	}

	return c, err
}

// FinalizeConfig validates and finalizes Config fields.
func FinalizeConfig(all []*Target, c *Config) error {
	uniqueTargetId := map[string]*Target{} // map Target.Id to Target.Label
	uniqueLabel := map[string]bool{}

	// Validate the session file path early (vs. the first timer-based write) by ensuring the path is
	// writable and intermediate directories exist.
	if c.Data.Session.File != "" {
		f, err := cage_file.CreateFileAll(c.Data.Session.File, 0, dataFilePerm, dataDirPerm)
		if err != nil {
			return errors.Wrapf(err, "failed to init log file [%s]", c.Data.Session.File)
		}
		defer cage_io.CloseOrStderr(f, c.Data.Session.File)
	}

	if c.Global.Cooldown == "" {
		c.Global.Cooldown = DefaultCooldown
	}
	var cooldownErr error
	c.Global.cooldown, cooldownErr = time.ParseDuration(c.Global.Cooldown)
	if cooldownErr != nil {
		return errors.Wrapf(cooldownErr, "failed to parse Cooldown [%s]", c.Global.Cooldown)
	}

	var expectedTemplateKeys []string
	for k := range c.Template {
		expectedTemplateKeys = append(expectedTemplateKeys, k)
	}

	for n := range all {
		var absErr error

		t := all[n]

		// Include all possible keys to avoid text/template expanding missing keys with "<no value>".
		dataBuilder := cage_template.NewStringMapBuilder().
			SetExpectedKey(expectedTemplateKeys...).
			Merge(cage_structs.MergeModeCombine, c.Template, CmdTemplateData{})

		expandErr := t.ExpandTemplateVars(dataBuilder.Map())
		if expandErr != nil {
			return errors.WithStack(expandErr)
		}

		// Require Root paths except for any target with watched file patterns.
		//
		// This allows targets, such as those in AutoStartTargets, to define a command sequence but not file watches.
		// But if a target is configured to auto-start but also watch files, then a Root is required to resolve
		// the relative file patterns.
		if t.Root == "" && len(t.Include) > 0 {
			return errors.Errorf("target [%s] is missing a [Root] field", t.Label)
		}

		if t.Label == "" {
			return errors.New("target is missing a [Label] field")
		}
		t.Root, absErr = filepath.Abs(t.Root)
		if absErr != nil {
			return errors.Wrapf(absErr, "failed to get absolute path of target [%s] [Root] field [%s]", t.Label, t.Root)
		}
		exists, fi, expandErr := cage_file.Exists(t.Root)
		if expandErr != nil {
			return errors.Wrapf(expandErr, "failed to verify target [%s] root [%s] exists", t.Label, t.Root)
		}
		if !exists {
			return errors.Errorf("target [%s] root [%s] does not exist", t.Label, t.Root)
		}
		if !fi.IsDir() {
			return errors.Errorf("target [%s] root [%s] is not a directory", t.Label, t.Root)
		}

		// ensure UI layer has unique value for Target identity checks (== not allowed due to struct fields)
		if t.Id == "" {
			t.Id = fmt.Sprintf("auto-generated Id: [%s][%s]", t.Label, t.Root)
		}

		if _, dupe := uniqueLabel[t.Label]; dupe {
			return errors.Errorf("target label [%s] was used more than once", t.Label)
		}
		uniqueLabel[t.Label] = true

		if t.Debounce == "" {
			t.Debounce = DefaultDebounce
		}
		var debounceErr error
		t.debounce, debounceErr = time.ParseDuration(t.Debounce)
		if debounceErr != nil {
			return errors.Wrapf(debounceErr, "[target: %s]: failed to parse Debounce [%s]", t.Label, t.Debounce)
		}

		// Default all per-include roots to the target root.
		// Resolve all per-include roots as relative to the target root.
		// Resolve all globs as relative to the per-include root.
		for k := range t.Include {
			var appendErr error

			i := &t.Include[k]

			if i.Root == "" { // simple fallback on target root
				i.Root = t.Root
			} else {
				if filepath.IsAbs(i.Root) {
					return errors.Errorf("target [%s] include root [%s] must be relative to target [Root] field", t.Label, i.Root)
				}
				i.Root, appendErr = cage_filepath.Append(t.Root, i.Root) // include root is relative to target root
				if appendErr != nil {
					return errors.Wrapf(appendErr, "failed to append target [%s] Include.Root [%s] to Target.Root [%s]", t.Label, i.Root, t.Root)
				}

				exists, fi, existsErr := cage_file.Exists(i.Root)
				if existsErr != nil {
					return errors.Wrapf(existsErr, "failed to check if target [%s] include root [%s] exists", t.Label, i.Root)
				}
				if !exists {
					return errors.Errorf("target [%s] include root [%s] does not exist", t.Label, i.Root)
				}
				if !fi.IsDir() {
					return errors.Errorf("target [%s] include root [%s] is not a directory", t.Label, i.Root)
				}
			}

			if i.Pattern == "" { // glob is relative to the include root
				return errors.Errorf("target [%s] contains an [Include] with an empty [Glob] field", t.Label)
			}
			if filepath.IsAbs(i.Pattern) {
				return errors.Errorf("target [%s] include glob [%s] must be relative", t.Label, i.Pattern)
			}

			i.Pattern, appendErr = cage_filepath.Append(i.Root, i.Pattern)
			if appendErr != nil {
				return errors.Wrapf(appendErr, "failed to append target [%s] Include.Pattern [%s] to Include.Root [%s]", t.Label, i.Pattern, i.Root)
			}
		}

		// Global excludes, while globally applied to all targets, are resolved relative to
		// each target's root. So simply adding the globs to the target's exclude list
		// is sufficient for them to get resolved just like per-target excludes.
		for _, e := range c.Global.Exclude {
			if filepath.IsAbs(e.Pattern) {
				return errors.Errorf("global exclude [%s] must be relative", e.Pattern)
			}
			t.Exclude = append(t.Exclude, cage_filepath.Glob{Pattern: e.Pattern})
		}

		// Default all per-exclude roots to the target root.
		// Resolve all per-exclude roots as relative to the target root.
		// Resolve all globs as relative to the per-exclude root.
		for k := range t.Exclude {
			var appendErr error

			e := &t.Exclude[k]

			if e.Root == "" { // simple fallback on target root
				e.Root = t.Root
			} else {
				if filepath.IsAbs(e.Root) {
					return errors.Errorf("target [%s] exclude root [%s] must be relative to target [Root] field", t.Label, e.Root)
				}
				e.Root, appendErr = cage_filepath.Append(t.Root, e.Root) // exclude root is relative to target root
				if appendErr != nil {
					return errors.Wrapf(appendErr, "failed to append target [%s] Exclude.Root [%s] to Target.Root [%s]", t.Label, e.Root, t.Root)
				}

				exists, fi, existsErr := cage_file.Exists(e.Root)
				if existsErr != nil {
					return errors.Wrapf(existsErr, "failed to check if target [%s] exclude root [%s] exists", t.Label, e.Root)
				}
				if !exists {
					return errors.Errorf("target [%s] exclude root [%s] does not exist", t.Label, e.Root)
				}
				if !fi.IsDir() {
					return errors.Errorf("target [%s] exclude root [%s] is not a directory", t.Label, e.Root)
				}
			}

			if e.Pattern == "" { // glob is relative to the exclude root
				return errors.Errorf("target [%s] contains an [Exclude] with an empty [Glob] field", t.Label)
			}
			if filepath.IsAbs(e.Pattern) {
				return errors.Errorf("target [%s] exclude glob [%s] must be relative", t.Label, e.Pattern)
			}

			e.Pattern, appendErr = cage_filepath.Append(e.Root, e.Pattern)
			if appendErr != nil {
				return errors.Wrapf(appendErr, "failed to append target [%s] Exclude.Pattern [%s] to Exclude.Root [%s]", t.Label, e.Pattern, e.Root)
			}
		}

		// Ensure all pattern root paths are within the target's root.
		for _, glob := range t.Include {
			if !strings.HasPrefix(glob.Root, t.Root) {
				return errors.Errorf("target [%s] has an Include.Root [%s] outside the target-level root [%s]", t.Label, glob.Root, t.Root)
			}
		}
		for _, glob := range t.Exclude {
			if !strings.HasPrefix(glob.Root, t.Root) {
				return errors.Errorf("target [%s] has an Exclude.Root [%s] outside the target-level root [%s]", t.Label, glob.Root, t.Root)
			}
		}
	}

	for n := range all { // perform in 2nd pass so Id/Label/etc are already finalized
		t := all[n]

		// Exec.Dir values must be relative to Target.Root and default to Target.Root
		for h, handler := range t.Handler {
			for e, exe := range handler.Exec {
				var appendErr error

				if exe.Dir == "" {
					t.Handler[h].Exec[e].Dir = t.Root
				} else {
					t.Handler[h].Exec[e].Dir, appendErr = cage_filepath.Append(t.Root, t.Handler[h].Exec[e].Dir)
					if appendErr != nil {
						return errors.Wrapf(appendErr, "target [%s] has a Handler.Exec.Dir [%s] outside the target-level root [%s]", t.Label, t.Handler[h].Exec[e].Dir, t.Root)
					}
					exists, fi, existsErr := cage_file.Exists(t.Handler[h].Exec[e].Dir)
					if existsErr != nil {
						return errors.Wrapf(existsErr, "failed to verify target [%s] handler [%s] exec dir [%s] exists", t.Label, handler.Label, t.Handler[h].Exec[e].Dir)
					}
					if !exists {
						return errors.Errorf("target [%s] handler [%s] exec dir [%s] does not exist", t.Label, handler.Label, t.Handler[h].Exec[e].Dir)
					}
					if !fi.IsDir() {
						return errors.Errorf("target [%s] handler [%s] exec dir [%s] is not a directory", t.Label, handler.Label, t.Handler[h].Exec[e].Dir)
					}
				}

				if t.Handler[h].Exec[e].Timeout == "" {
					t.Handler[h].Exec[e].Timeout = DefaultCmdTimeout
				}

				var timeoutErr error
				t.Handler[h].Exec[e].timeout, timeoutErr = time.ParseDuration(t.Handler[h].Exec[e].Timeout)
				if timeoutErr != nil {
					return errors.Wrapf(timeoutErr, "[target: %s]: failed to parse handler [%s] command [%s] Timeout [%s]", t.Label, t.Handler[h].Label, t.Handler[h].Exec[e].Cmd, t.Handler[h].Exec[e].Timeout)
				}
			}
		}
	}

	for n := range all {
		t := all[n]

		t.Downstream = []*Target{}
		for o, other := range all {
			if other.Id == t.Id {
				continue
			}
			for _, id := range other.Upstream {
				if id == t.Id {
					t.Downstream = append(t.Downstream, all[o])
				}
			}
		}

		// Allow targets with 0 handlers in case they're effectively a shared "glob only" target with
		// various downstream targets that want to "reuse" the former's glob triggers and define their own
		// handlers. For example, a shared target might cover all source code in a project, but only its
		// downstreams define handlers.
		if len(t.Handler) == 0 && len(t.Downstream) == 0 {
			return errors.Errorf("target [%s] cannot have 0 handlers unless it has at least 1 downstream", t.Label)
		}
	}

	for n := range all {
		t := all[n]

		if preexisting, dupe := uniqueTargetId[t.Id]; dupe {
			return errors.Errorf("target [%s] has an Id [%s] that is already used by target [%s]", t.Label, t.Id, preexisting.Label)
		}
		uniqueTargetId[t.Id] = t
	}

	for n := range all {
		t := all[n]

		t.Tree = []TargetTree{
			{
				Id:      t.Id,
				Label:   t.Label,
				Handler: append([]Handler{}, t.Handler...),
			},
		}
		visitErr := VisitDownstream(t, func(target *Target) error {
			t.Tree = append(
				t.Tree,
				TargetTree{
					Id:      target.Id,
					Label:   target.Label,
					Handler: append([]Handler{}, target.Handler...),
				},
			)
			return nil
		})
		if visitErr != nil {
			return errors.Wrapf(visitErr, "failed to collect downstream Target.Id values [%s]", t.Label)
		}
	}

	c.startTarget = []Target{}
	for _, id := range c.AutoStartTarget {
		match, valid := uniqueTargetId[id]
		if !valid {
			return errors.Errorf("cannot auto-start target [%s]: Id not found", id)
		}
		c.startTarget = append(c.startTarget, *match)
	}

	return nil
}
