// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render

import "time"

// SetBrowserForTest points the renderer at another browser binary and, when
// timeout isn't zero, start-up timeout, until the returned func restores
// them.
func SetBrowserForTest(path string, timeout time.Duration) func() {
	oldPath, oldTimeout := execPath, testStartTimeout
	execPath, testStartTimeout = path, timeout
	return func() { execPath, testStartTimeout = oldPath, oldTimeout }
}
