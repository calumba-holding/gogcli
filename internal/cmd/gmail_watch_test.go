package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestGmailWatchStartCmd_JSON(t *testing.T) {
	origNew := newGmailService
	t.Cleanup(func() { newGmailService = origNew })

	home := t.TempDir()
	t.Setenv("HOME", home)

	var watchReq struct {
		TopicName string   `json:"topicName"`
		LabelIds  []string `json:"labelIds"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/gmail/v1/users/me/labels"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"labels": []map[string]any{
					{"id": "INBOX", "name": "INBOX"},
					{"id": "Label_1", "name": "Custom"},
				},
			})
			return
		case strings.Contains(r.URL.Path, "/gmail/v1/users/me/watch"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &watchReq)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"historyId":  "123",
				"expiration": "1730000000000",
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	svc, err := gmail.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	newGmailService = func(context.Context, string) (*gmail.Service, error) { return svc, nil }

	flags := &rootFlags{Account: "a@b.com"}
	out := captureStdout(t, func() {
		u, uiErr := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
		if uiErr != nil {
			t.Fatalf("ui.New: %v", uiErr)
		}
		ctx := ui.WithUI(context.Background(), u)
		ctx = outfmt.WithMode(ctx, outfmt.ModeJSON)

		cmd := newGmailWatchStartCmd(flags)
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{
			"--topic", "projects/p/topics/t",
			"--label", "INBOX",
			"--label", "Custom",
			"--hook-url", "http://127.0.0.1:1/hooks",
			"--hook-token", "tok",
			"--include-body",
			"--max-bytes", "5",
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	if watchReq.TopicName != "projects/p/topics/t" {
		t.Fatalf("unexpected topic: %#v", watchReq)
	}
	if len(watchReq.LabelIds) != 2 || watchReq.LabelIds[0] != "INBOX" || watchReq.LabelIds[1] != "Label_1" {
		t.Fatalf("unexpected labels: %#v", watchReq.LabelIds)
	}

	var parsed struct {
		Watch gmailWatchState `json:"watch"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if parsed.Watch.HistoryID != "123" {
		t.Fatalf("unexpected history: %#v", parsed.Watch)
	}
	if parsed.Watch.Hook == nil || parsed.Watch.Hook.URL == "" || !parsed.Watch.Hook.IncludeBody {
		t.Fatalf("missing hook: %#v", parsed.Watch.Hook)
	}
	if parsed.Watch.Hook.MaxBytes != 5 {
		t.Fatalf("unexpected max bytes: %#v", parsed.Watch.Hook)
	}

	store, err := loadGmailWatchStore("a@b.com")
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if store.Get().HistoryID != "123" {
		t.Fatalf("store missing history: %#v", store.Get())
	}
}

func TestGmailWatchServerServeHTTP_TruncateBody(t *testing.T) {
	origNew := newGmailService
	t.Cleanup(func() { newGmailService = origNew })

	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := newGmailWatchStore("me@example.com")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := store.Update(func(s *gmailWatchState) error {
		*s = gmailWatchState{Account: "me@example.com", HistoryID: "100"}
		return nil
	}); err != nil {
		t.Fatalf("store update: %v", err)
	}

	bodyEncoded := base64.RawURLEncoding.EncodeToString([]byte("hello world"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/gmail/v1/users/me/history"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"history": []map[string]any{
					{
						"id":            "1",
						"messagesAdded": []map[string]any{{"message": map[string]any{"id": "m1"}}},
					},
				},
				"historyId": "250",
			})
			return
		case strings.Contains(r.URL.Path, "/gmail/v1/users/me/messages/m1"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "m1",
				"threadId": "t1",
				"labelIds": []string{"INBOX"},
				"snippet":  "snippet",
				"payload": map[string]any{
					"mimeType": "text/plain",
					"body":     map[string]any{"data": bodyEncoded},
					"headers": []map[string]any{
						{"name": "From", "value": "From <from@example.com>"},
						{"name": "To", "value": "To <to@example.com>"},
						{"name": "Subject", "value": "Hi"},
						{"name": "Date", "value": "Wed, 17 Dec 2025 14:00:00 -0800"},
					},
				},
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	svc, err := gmail.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	newGmailService = func(context.Context, string) (*gmail.Service, error) { return svc, nil }

	hookServer := &gmailWatchServer{
		cfg: gmailWatchServeConfig{
			Account:      "me@example.com",
			Path:         "/gmail-pubsub",
			SharedToken:  "token",
			IncludeBody:  true,
			MaxBodyBytes: 5,
			HistoryMax:   defaultHistoryMaxResults,
			ResyncMax:    defaultHistoryResyncMax,
			AllowNoHook:  true,
		},
		store:      store,
		newService: newGmailService,
		hookClient: &http.Client{Timeout: time.Second},
		logf:       func(string, ...any) {},
		warnf:      func(string, ...any) {},
	}

	payload, _ := json.Marshal(gmailPushPayload{EmailAddress: "me@example.com", HistoryID: "200"})
	env := pubsubPushEnvelope{}
	env.Message.Data = base64.StdEncoding.EncodeToString(payload)

	data, _ := json.Marshal(env)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/gmail-pubsub", bytes.NewReader(data))
	req.Header.Set("x-gog-token", "token")
	rec := httptest.NewRecorder()

	hookServer.ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rec.Result().Body)
		t.Fatalf("unexpected status: %d body=%q", rec.Result().StatusCode, string(body))
	}

	var parsed gmailHookPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("unexpected messages: %#v", parsed.Messages)
	}
	msg := parsed.Messages[0]
	if msg.Body != "hello" || !msg.BodyTruncated {
		t.Fatalf("unexpected body: %#v", msg)
	}
}
