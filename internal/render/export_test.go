// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render

import "time"

// SetBrowserForTest points RenderHTMLToPDF at another browser binary and
// startup timeout until the returned func restores them.
func SetBrowserForTest(path string, timeout time.Duration) func() {
	oldPath, oldTimeout := execPath, wsURLReadTimeout
	execPath, wsURLReadTimeout = path, timeout
	return func() { execPath, wsURLReadTimeout = oldPath, oldTimeout }
}
