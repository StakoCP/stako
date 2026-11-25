// SPDX-License-Identifier: MIT
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/catalog"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

// --- Docker Rootless Manager (7.4, SEC-S13, SEC-S14) ---

func validateContainerSecurity(image string, volumes []string, composeYAML string) error {
	if strings.ContainsAny(image, " \n\r;&|") || strings.TrimSpace(image) == "" {
		return fmt.Errorf("invalid image reference")
	}
	for _, v := range volumes {
		low := strings.ToLower(v)
		if strings.Contains(low, "docker.sock") || strings.HasPrefix(low, "/etc:") || strings.HasPrefix(low, "/var/run:") || strings.HasPrefix(low, "/:") {
			return fmt.Errorf("forbidden host bind mount (SEC-S14): %s", v)
		}
	}
	if composeYAML != "" {
		low := strings.ToLower(composeYAML)
		for _, forbidden := range []string{"privileged: true", "docker.sock", "network_mode: host", "pid: host"} {
			if strings.Contains(low, forbidden) {
				return fmt.Errorf("forbidden directive in compose (%s, SEC-S14)", forbidden)
			}
		}
	}
	return nil
}

func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	limit, offset := parsePagination(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, total, err := s.store.ListContainers(r.Context(), userFilter, resellerFilter, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleCreateContainer(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Name     string            `json:"name"`
		Image    string            `json:"image"`
		UserID   *string           `json:"user_id"`
		DomainID *string           `json:"domain_id"`
		Ports    []string          `json:"ports"`
		Volumes  []string          `json:"volumes"`
		Env      map[string]string `json:"env"`
		CPULimit string            `json:"cpu_limit"`
		MemoryMB int               `json:"memory_mb"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || strings.ContainsAny(req.Name, " \n\r/") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid container name", "")
		return
	}
	if err := validateContainerSecurity(req.Image, req.Volumes, ""); err != nil {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Container security policy violation", err.Error())
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	if req.Ports == nil {
		req.Ports = []string{}
	}
	if req.Volumes == nil {
		req.Volumes = []string{}
	}
	if req.CPULimit == "" {
		req.CPULimit = "1.0"
	}
	if req.MemoryMB == 0 {
		req.MemoryMB = 512
	}
	_, envEnc := s.encryptEnvMap(req.Env)
	id, _ := crypto.RandomID("c")
	c := &store.Container{
		ID: id, UserID: owner.ID, DomainID: req.DomainID, Name: req.Name, Image: req.Image,
		Ports: req.Ports, Volumes: req.Volumes, EnvEnc: envEnc, CPULimit: req.CPULimit,
		MemoryMB: req.MemoryMB, Status: "running",
	}
	if err := s.store.CreateContainer(r.Context(), c); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "docker.run", ac.User.ID, map[string]any{
		"username": owner.Username, "name": c.Name, "image": c.Image,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "docker.create", "container", c.ID, "success", map[string]any{"name": c.Name, "image": c.Image})
	WriteJSON(w, http.StatusCreated, map[string]any{"container": c, "job_id": jobID})
}

func (s *Server) handleContainerAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ac := GetAuth(r)
		if ac.User.Role == "readonly" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
			return
		}
		c, err := s.store.GetContainer(r.Context(), r.PathValue("id"))
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		if !s.canAccessOwned(r, ac, c.UserID) {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
			return
		}
		st := "running"
		if action == "stop" {
			st = "stopped"
		}
		_ = s.store.UpdateContainerStatus(r.Context(), c.ID, st)
		owner, _ := s.store.GetUserByID(r.Context(), c.UserID)
		var jobID *string
		if j, err := s.jobs.Enqueue(r.Context(), "docker."+action, ac.User.ID, map[string]any{
			"username": owner.Username, "name": c.Name, "action": action,
		}); err == nil {
			jobID = &j.ID
		}
		s.audit(r, ac.User, "docker."+action, "container", c.ID, "success", nil)
		WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
	}
}

func (s *Server) handleDeleteContainer(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	c, err := s.store.GetContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, c.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteContainer(r.Context(), c.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), c.UserID)
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "docker.delete", ac.User.ID, map[string]any{
		"username": owner.Username, "name": c.Name, "action": "delete",
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "docker.delete", "container", c.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	c, err := s.store.GetContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, c.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), c.UserID)
	resp, err := s.agent.Call(r.Context(), agentproto.CmdDockerLogs, map[string]any{
		"username": owner.Username, "name": c.Name,
	})
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"logs": err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"logs": resp.Output})
}

func (s *Server) handleListComposeStacks(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListComposeStacks(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateComposeStack(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Name        string  `json:"name"`
		ComposeYAML string  `json:"compose_yaml"`
		UserID      *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.ComposeYAML) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Name and compose_yaml are required", "")
		return
	}
	if err := validateContainerSecurity("compose", nil, req.ComposeYAML); err != nil {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Compose security policy violation", err.Error())
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	id, _ := crypto.RandomID("cs")
	st := &store.ComposeStack{
		ID: id, UserID: owner.ID, Name: strings.TrimSpace(req.Name), ComposeYAML: req.ComposeYAML, Status: "running",
	}
	if err := s.store.CreateComposeStack(r.Context(), st); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "docker.compose", ac.User.ID, map[string]any{
		"username": owner.Username, "name": st.Name, "action": "up",
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "docker.compose_up", "compose_stack", st.ID, "success", map[string]any{"name": st.Name})
	WriteJSON(w, http.StatusCreated, map[string]any{"stack": st, "job_id": jobID})
}

func (s *Server) handleDeleteComposeStack(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	st, err := s.store.GetComposeStack(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, st.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteComposeStack(r.Context(), st.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), st.UserID)
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "docker.compose", ac.User.ID, map[string]any{
		"username": owner.Username, "name": st.Name, "action": "down",
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "docker.compose_down", "compose_stack", st.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

// --- App Installer Catalog (7.11) ---

func (s *Server) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"items": catalog.Apps})
}

func (s *Server) handleListAppInstalls(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListAppInstalls(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleInstallCatalogApp(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		AppSlug    string  `json:"app_slug"`
		DomainID   string  `json:"domain_id"`
		UserID     *string `json:"user_id"`
		AdminEmail string  `json:"admin_email"`
		SiteTitle  string  `json:"site_title"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	appDef, ok := catalog.Find(req.AppSlug)
	if !ok {
		WriteProblem(w, http.StatusNotFound, "not-found", "App not found in catalog", "")
		return
	}
	dom, err := s.store.GetDomain(r.Context(), req.DomainID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden domain", "")
		return
	}
	id, _ := crypto.RandomID("ai")
	ai := &store.AppInstall{
		ID: id, UserID: dom.UserID, DomainID: dom.ID, AppSlug: appDef.Slug, Status: "installed",
	}
	if err := s.store.CreateAppInstall(r.Context(), ai); err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), dom.UserID)
	username := ac.User.Username
	if owner != nil {
		username = owner.Username
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "catalog.install", ac.User.ID, map[string]any{
		"app_slug": appDef.Slug, "domain": dom.Domain, "username": username, "docroot": dom.Docroot,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "catalog.install", "app_install", ai.ID, "success", map[string]any{"app": appDef.Slug, "domain": dom.Domain})
	WriteJSON(w, http.StatusCreated, map[string]any{"install": ai, "job_id": jobID})
}

// --- Redis & RabbitMQ (7.9) ---

func (s *Server) handleListRedis(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListRedis(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateRedis(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Name       string  `json:"name"`
		UserID     *string `json:"user_id"`
		MemoryMB   int     `json:"memory_mb"`
		Persistent bool    `json:"persistent"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Name required", "")
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	port, err := s.store.NextFreeRedisPort(r.Context())
	if err != nil {
		WriteProblem(w, http.StatusConflict, "conflict", "No free Redis port", "")
		return
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = 256
	}
	pass, _ := crypto.RandomHex(12)
	id, _ := crypto.RandomID("rd")
	inst := &store.RedisInstance{
		ID: id, UserID: owner.ID, Name: strings.TrimSpace(req.Name), Port: port,
		MemoryMB: req.MemoryMB, Persistent: req.Persistent, Status: "running",
	}
	if err := s.store.CreateRedis(r.Context(), inst); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "redis.create", ac.User.ID, map[string]any{"name": inst.Name, "port": inst.Port}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "redis.create", "redis", inst.ID, "success", map[string]any{"name": inst.Name, "port": inst.Port})
	WriteJSON(w, http.StatusCreated, map[string]any{
		"instance":          inst,
		"connection_string": fmt.Sprintf("redis://:%s@127.0.0.1:%d", pass, inst.Port),
		"job_id":            jobID,
	})
}

func (s *Server) handleDeleteRedis(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	inst, err := s.store.GetRedis(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, inst.UserID) || ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteRedis(r.Context(), inst.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "redis.delete", ac.User.ID, map[string]any{"name": inst.Name}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "redis.delete", "redis", inst.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleListRabbit(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListRabbitVhosts(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateRabbit(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Vhost    string  `json:"vhost"`
		Username string  `json:"username"`
		Password string  `json:"password"`
		UserID   *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if strings.TrimSpace(req.Vhost) == "" || strings.TrimSpace(req.Username) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Vhost and username required", "")
		return
	}
	if err := crypto.ValidatePasswordStrength(req.Password); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error())
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	id, _ := crypto.RandomID("rmq")
	rv := &store.RabbitVhost{
		ID: id, UserID: owner.ID, Vhost: strings.TrimSpace(req.Vhost), Username: strings.TrimSpace(req.Username), Status: "active",
	}
	if err := s.store.CreateRabbitVhost(r.Context(), rv); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "rabbitmq.create", ac.User.ID, map[string]any{"vhost": rv.Vhost, "username": rv.Username}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "rabbitmq.create", "rabbitmq", rv.ID, "success", map[string]any{"vhost": rv.Vhost})
	WriteJSON(w, http.StatusCreated, map[string]any{
		"vhost":             rv,
		"connection_string": fmt.Sprintf("amqp://%s:%s@127.0.0.1:5672/%s", rv.Username, req.Password, strings.TrimPrefix(rv.Vhost, "/")),
		"job_id":            jobID,
	})
}

func (s *Server) handleDeleteRabbit(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	rv, err := s.store.GetRabbitVhost(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, rv.UserID) || ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteRabbitVhost(r.Context(), rv.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "rabbitmq.delete", ac.User.ID, map[string]any{"vhost": rv.Vhost}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "rabbitmq.delete", "rabbitmq", rv.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}
