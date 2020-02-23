// Copyright (c) 2015-2017 Marin Atanasov Nikolov <dnaeon@gmail.com>
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
//
//  1. Redistributions of source code must retain the above copyright
//     notice, this list of conditions and the following disclaimer
//     in this position and unchanged.
//  2. Redistributions in binary form must reproduce the above copyright
//     notice, this list of conditions and the following disclaimer in the
//     documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR(S) ``AS IS'' AND ANY EXPRESS OR
// IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
// OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
// IN NO EVENT SHALL THE AUTHOR(S) BE LIABLE FOR ANY DIRECT, INDIRECT,
// INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
// NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
// THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Origin:
//   https://github.com/dnaeon/gru/blob/f27c5b12cb30bd02491c4b689f1df7273c66ae49/utils/slice.go
//   BSD: https://github.com/dnaeon/gru/blob/f27c5b12cb30bd02491c4b689f1df7273c66ae49/LICENSE
//
// Changes:
//   - Renamed to "Slice"

package sync

import "sync"

// Slice type that can be safely shared between goroutines
type Slice struct {
	sync.RWMutex
	items []interface{}
}

// SliceItem contains the index/value pair of an item in a
// concurrent slice
type SliceItem struct {
	Index int
	Value interface{}
}

// NewSlice creates a new concurrent slice
func NewSlice() *Slice {
	s := &Slice{
		items: make([]interface{}, 0),
	}

	return s
}

// Append adds an item to the concurrent slice
func (s *Slice) Append(item interface{}) {
	s.Lock()
	defer s.Unlock()

	s.items = append(s.items, item)
}

// Delete removes an item from the concurrent slice
func (s *Slice) Delete(idx int) {
	s.Lock()
	defer s.Unlock()

	if idx > -1 && idx < len(s.items) {
		s.items = append(s.items[:idx], s.items[idx+1:]...)
	}
}

// PopFirst removes the first element and returns it.
func (s *Slice) PopFirst() interface{} {
	s.Lock()
	defer s.Unlock()

	if len(s.items) == 0 {
		return nil
	}

	first := s.items[0]
	s.items = append(s.items[:0], s.items[1:]...)

	return first
}

// Iter iterates over the items in the concurrent slice
// Each item is sent over a channel, so that
// we can iterate over the slice using the builin range keyword
func (s *Slice) Iter() <-chan SliceItem {
	c := make(chan SliceItem)

	f := func() {
		s.Lock()
		defer s.Unlock()
		for index, value := range s.items {
			c <- SliceItem{index, value}
		}
		close(c)
	}
	go f()

	return c
}
