//go:build !linux

// SPDX-License-Identifier: MIT
package main

import "net"

func verifyPeerUID(_ net.Conn, _ int) error {
	return nil
}
