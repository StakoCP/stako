//go:build linux

// SPDX-License-Identifier: MIT
package main

import (
	"fmt"
	"net"
	"syscall"
)

func verifyPeerUID(conn net.Conn, wantUID int) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if int(cred.Uid) != wantUID {
		return fmt.Errorf("peer uid %d != allowed %d", cred.Uid, wantUID)
	}
	return nil
}
