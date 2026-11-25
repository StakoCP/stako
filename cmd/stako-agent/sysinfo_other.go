//go:build !linux

// SPDX-License-Identifier: MIT
package main

import "runtime"

func collectSystemInfo() map[string]any {
	return map[string]any{
		"os": runtime.GOOS + "/" + runtime.GOARCH,
	}
}
