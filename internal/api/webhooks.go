// SPDX-License-Identifier: MIT
package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

type webhookDelivery struct {
	ID         string    `json:"id"`
	WebhookID  string    `json:"webhook_id"`
	Event      string    `json:"event"`
	URL        string    `json:"url"`
	StatusCode int       `json:"status_code"`
	Success    bool      `json:"success"`
	Attempts   int       `json:"attempts"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) recordWebhookDelivery(d webhookDelivery) {
	s.whMu.Lock()
	defer s.whMu.Unlock()
	s.whDeliveries = append([]webhookDelivery{d}, s.whDeliveries...)
	if len(s.whDeliveries) > 200 {
		s.whDeliveries = s.whDeliveries[:200]
	}
}

func (s *Server) dispatchWebhookEvent(event string, payload map[string]any) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		hooks, err := s.store.ListWebhooks(ctx, nil)
		if err != nil || len(hooks) == 0 {
			return
		}
		for _, wh := range hooks {
			if !slices.Contains(wh.Events, event) && !slices.Contains(wh.Events, "*") {
				continue
			}
			s.deliverWebhook(wh, event, payload)
		}
	}()
}

func (s *Server) deliverWebhook(wh store.Webhook, event string, payload map[string]any) {
	deliveryID, _ := crypto.RandomID("whd")
	envelope := map[string]any{
		"id":         deliveryID,
		"event":      event,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"data":       payload,
	}
	body, _ := json.Marshal(envelope)
	secret, _ := s.enc.Decrypt(wh.SecretEnc)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	client := &http.Client{Timeout: 6 * time.Second}
	backoffs := []time.Duration{0, 1 * time.Second, 3 * time.Second}
	var lastCode int
	var lastErr string
	attempts := 0

	for i, wait := range backoffs {
		if wait > 0 {
			time.Sleep(wait)
		}
		attempts = i + 1
		req, err := http.NewRequest(http.MethodPost, wh.URL, bytes.NewReader(body))
		if err != nil {
			lastErr = err.Error()
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Stako-Webhook/1.0")
		req.Header.Set("X-Stako-Event", event)
		req.Header.Set("X-Stako-Delivery", deliveryID)
		req.Header.Set("X-Stako-Signature", sig)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		lastCode = resp.StatusCode
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			s.recordWebhookDelivery(webhookDelivery{
				ID: deliveryID, WebhookID: wh.ID, Event: event, URL: wh.URL,
				StatusCode: lastCode, Success: true, Attempts: attempts, CreatedAt: time.Now().UTC(),
			})
			return
		}
		lastErr = http.StatusText(resp.StatusCode)
		if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			break
		}
	}

	s.recordWebhookDelivery(webhookDelivery{
		ID: deliveryID, WebhookID: wh.ID, Event: event, URL: wh.URL,
		StatusCode: lastCode, Success: false, Attempts: attempts, Error: lastErr, CreatedAt: time.Now().UTC(),
	})
}

func (s *Server) handleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	s.whMu.Lock()
	items := make([]webhookDelivery, len(s.whDeliveries))
	copy(items, s.whDeliveries)
	s.whMu.Unlock()
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	hooks, err := s.store.ListWebhooks(r.Context(), nil)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	id := r.PathValue("id")
	for _, wh := range hooks {
		if wh.ID == id && (ac.User.Role == "admin" || wh.UserID == ac.User.ID) {
			go s.deliverWebhook(wh, "webhook.test", map[string]any{"webhook_id": wh.ID, "triggered_by": ac.User.Username})
			WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "webhook_id": wh.ID})
			return
		}
	}
	WriteProblem(w, http.StatusNotFound, "not-found", "Webhook not found", "")
}
