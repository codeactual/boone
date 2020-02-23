// Copyright (C) 2019 The CodeActual Go Environment Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package gob

import (
	std_gob "encoding/gob"
	"os"

	"github.com/pkg/errors"
)

func EncodeToFile(name string, value interface{}) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to open file [%s] for encoding", name)
	}
	enc := std_gob.NewEncoder(f)
	err = enc.Encode(value)
	if err != nil {
		return errors.Wrapf(err, "failed to encode value to file [%s]", name)
	}
	return nil
}

// DecodeFromFile returns a Decoder, instead of an interface{} whose type
// can be asserted, because Decode relies on the destination type information.
func DecodeFromFile(name string) (dec *std_gob.Decoder, err error) {
	f, err := os.Open(name) // #nosec G304
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open file [%s] for decoding", name)
	}
	return std_gob.NewDecoder(f), nil
}
