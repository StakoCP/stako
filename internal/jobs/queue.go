// SPDX-License-Identifier: MIT
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/StakoCP/stako/internal/agentclient"
	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

type Queue struct {
	store *store.Store
	agent *agentclient.Client
	ch    chan string
	wg    sync.WaitGroup
}

func NewQueue(s *store.Store, a *agentclient.Client) *Queue {
	return &Queue{
		store: s,
		agent: a,
		ch:    make(chan string, 128),
	}
}

func (q *Queue) Start(ctx context.Context) {
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case id := <-q.ch:
				q.runOne(ctx, id)
			}
		}
	}()
}

func (q *Queue) Stop() {
	q.wg.Wait()
}

func (q *Queue) Enqueue(ctx context.Context, jobType, createdBy string, payload any) (*store.Job, error) {
	id, err := crypto.RandomID("j")
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(payload)
	j := &store.Job{
		ID:        id,
		Type:      jobType,
		Status:    "queued",
		CreatedBy: createdBy,
		Payload:   raw,
	}
	if err := q.store.CreateJob(ctx, j); err != nil {
		return nil, err
	}
	select {
	case q.ch <- id:
	default:
		// If channel is full, run in background goroutine so caller isn't blocked.
		go q.runOne(context.Background(), id)
	}
	return j, nil
}

func (q *Queue) runOne(ctx context.Context, id string) {
	j, err := q.store.GetJob(ctx, id)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	j.Status = "running"
	j.StartedAt = &now
	_ = q.store.UpdateJob(ctx, j)

	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf("[%s] "+format, append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)...))
	}

	var payload map[string]any
	_ = json.Unmarshal(j.Payload, &payload)

	var cmd string
	switch j.Type {
	case "user.provision":
		cmd = agentproto.CmdUserCreate
	case "user.delete":
		cmd = agentproto.CmdUserDelete
	case "user.suspend":
		cmd = agentproto.CmdUserSuspend
	case "user.unsuspend":
		cmd = agentproto.CmdUserUnsuspend
	case "user.set_shell":
		cmd = agentproto.CmdUserSetShell
	case "domain.apply":
		cmd = agentproto.CmdDomainApply
	case "domain.delete":
		cmd = agentproto.CmdDomainDelete
	case "ssl.issue":
		cmd = agentproto.CmdSSLIssue
	case "app.deploy":
		cmd = agentproto.CmdAppDeploy
	case "app.start", "app.stop", "app.restart", "app.delete":
		cmd = agentproto.CmdAppAction
	case "db.create":
		cmd = agentproto.CmdDBCreate
	case "db.delete":
		cmd = agentproto.CmdDBDelete
	case "db.backup":
		cmd = agentproto.CmdDBBackup
	case "ftp.create":
		cmd = agentproto.CmdFTPCreate
	case "ftp.delete":
		cmd = agentproto.CmdFTPDelete
	case "firewall.apply":
		cmd = agentproto.CmdFirewallApply
	case "firewall.unban":
		cmd = agentproto.CmdFirewallUnban
	case "updates.apply":
		cmd = agentproto.CmdUpdatesApply
	case "docker.run":
		cmd = agentproto.CmdDockerRun
	case "docker.start", "docker.stop", "docker.restart", "docker.delete":
		cmd = agentproto.CmdDockerAction
	case "docker.compose":
		cmd = agentproto.CmdDockerCompose
	case "catalog.install":
		cmd = agentproto.CmdCatalogInstall
	case "redis.create":
		cmd = agentproto.CmdRedisCreate
	case "redis.delete":
		cmd = agentproto.CmdRedisDelete
	case "rabbitmq.create":
		cmd = agentproto.CmdRabbitCreate
	case "rabbitmq.delete":
		cmd = agentproto.CmdRabbitDelete
	case "mail.domain_add":
		cmd = agentproto.CmdMailDomainAdd
	case "mail.domain_del":
		cmd = agentproto.CmdMailDomainDel
	case "backup.run":
		cmd = agentproto.CmdBackupRun
	case "backup.restore":
		cmd = agentproto.CmdBackupRestore
	case "waf.apply":
		cmd = agentproto.CmdWAFApply
	case "git.deploy":
		cmd = agentproto.CmdGitDeploy
	case "panel.import":
		cmd = agentproto.CmdPanelImport
	default:
		errMsg := "unknown job type: " + j.Type
		logf("error: %s", errMsg)
		fin := time.Now().UTC()
		j.Status = "failed"
		j.Error = &errMsg
		j.Log = strings.Join(logs, "\n")
		j.FinishedAt = &fin
		_ = q.store.UpdateJob(ctx, j)
		return
	}

	logf("dispatching %s to stako-agent", cmd)
	resp, callErr := q.agent.Call(ctx, cmd, payload)
	fin := time.Now().UTC()
	j.FinishedAt = &fin
	if callErr != nil {
		errMsg := callErr.Error()
		logf("agent call failed: %s", errMsg)
		j.Status = "failed"
		j.Error = &errMsg
	} else if !resp.OK {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "agent returned error"
		}
		logf("agent error: %s", errMsg)
		if resp.Output != "" {
			logf("output: %s", resp.Output)
		}
		j.Status = "failed"
		j.Error = &errMsg
	} else {
		if resp.Output != "" {
			logf("%s", resp.Output)
		}
		logf("completed successfully")
		j.Status = "succeeded"
	}
	j.Log = strings.Join(logs, "\n")
	_ = q.store.UpdateJob(ctx, j)
}
