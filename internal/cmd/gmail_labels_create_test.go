package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGmailLabelsCreateCmd_NestedNameDistinctFromFlatName(t *testing.T) {
	createCalled := false

	newLabelsDeleteService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && isLabelsListPath(r.URL.Path):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]any{
				{"id": "Label_flat", "name": "gog-pr-review", "type": "user"},
			}})
			return
		case r.Method == http.MethodPost && isLabelsListPath(r.URL.Path):
			createCalled = true

			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if body.Name != "gog/pr-review" {
				http.Error(w, "wrong label name", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "Label_nested",
				"name": body.Name,
				"type": "user",
			})
			return
		default:
			http.NotFound(w, r)
		}
	})

	flags := &RootFlags{Account: "a@b.com"}
	ctx := newLabelsDeleteContext(t, true)

	out := captureStdout(t, func() {
		if err := runKong(t, &GmailLabelsCreateCmd{}, []string{"gog/pr-review"}, ctx, flags); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	if !createCalled {
		t.Fatal("expected label create call")
	}

	var parsed struct {
		Label struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"label"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse: %v\nout=%q", err, out)
	}
	if parsed.Label.ID != "Label_nested" || parsed.Label.Name != "gog/pr-review" {
		t.Fatalf("unexpected label: %#v", parsed.Label)
	}
}
