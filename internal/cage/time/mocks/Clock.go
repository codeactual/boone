// Code generated by mockery v1.0.0. DO NOT EDIT.

package mocks

import cagetime "github.com/codeactual/boone/internal/cage/time"
import mock "github.com/stretchr/testify/mock"
import time "time"

// Clock is an autogenerated mock type for the Clock type
type Clock struct {
	mock.Mock
}

// NewTimer provides a mock function with given fields: _a0
func (_m *Clock) NewTimer(_a0 time.Duration) cagetime.Timer {
	ret := _m.Called(_a0)

	var r0 cagetime.Timer
	if rf, ok := ret.Get(0).(func(time.Duration) cagetime.Timer); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(cagetime.Timer)
		}
	}

	return r0
}

// Now provides a mock function with given fields:
func (_m *Clock) Now() time.Time {
	ret := _m.Called()

	var r0 time.Time
	if rf, ok := ret.Get(0).(func() time.Time); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(time.Time)
	}

	return r0
}
