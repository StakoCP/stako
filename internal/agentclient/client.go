// SPDX-License-Identifier: MIT
package agentclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/version"
)

type Client struct {
	sockPath string
	dryRun   bool
}

func New(sockPath string, dryRun bool) *Client {
	return &Client{sockPath: sockPath, dryRun: dryRun}
}

func (c *Client) Call(ctx context.Context, cmd string, args map[string]any) (*agentproto.Response, error) {
	id, _ := crypto.RandomID("req")
	req := agentproto.Request{ID: id, Command: cmd, Args: args}

	if c.dryRun || c.sockPath == "" {
		return &agentproto.Response{
			ID:     id,
			OK:     true,
			DryRun: true,
			Output: fmt.Sprintf("[dry-run] %s %v", cmd, args),
			Data:   map[string]any{"version": version.Version, "dry_run": true},
		}, nil
	}

	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", c.sockPath)
	if err != nil {
		return nil, fmt.Errorf("connect agent socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("agent closed connection")
	}
	var resp agentproto.Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
