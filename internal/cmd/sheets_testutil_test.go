package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/sheets/v4"

	"github.com/steipete/gogcli/internal/app"
)

type sheetsBatchUpdateCapture struct {
	Body    map[string]any
	Request sheets.BatchUpdateSpreadsheetRequest
	Last    *sheets.Request
}

func (c *sheetsBatchUpdateCapture) reset() {
	c.Body = nil
	c.Request = sheets.BatchUpdateSpreadsheetRequest{}
	c.Last = nil
}

func (c *sheetsBatchUpdateCapture) firstRequest(t *testing.T) *sheets.Request {
	t.Helper()
	if len(c.Request.Requests) != 1 {
		t.Fatalf("expected one batchUpdate request, got %#v", c.Request.Requests)
	}
	return c.Request.Requests[0]
}

func newSheetsBatchUpdateTestService(
	t *testing.T,
	spreadsheet map[string]any,
	capture *sheetsBatchUpdateCapture,
) *sheets.Service {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/sheets/v4"), "/v4")
		switch {
		case strings.HasPrefix(path, "/spreadsheets/s1") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(spreadsheet)
		case strings.Contains(path, "/spreadsheets/s1:batchUpdate") && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&capture.Body); err != nil {
				t.Errorf("decode batchUpdate: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body, err := json.Marshal(capture.Body)
			if err != nil {
				t.Errorf("marshal batchUpdate: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := json.Unmarshal(body, &capture.Request); err != nil {
				t.Errorf("decode typed batchUpdate: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(capture.Request.Requests) == 1 {
				capture.Last = capture.Request.Requests[0]
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	})
	svc, closeServer := newGoogleTestService(t, handler, sheets.NewService)
	t.Cleanup(closeServer)
	return svc
}

func newSheetsServiceFromServer(t *testing.T, srv *httptest.Server) *sheets.Service {
	t.Helper()
	return newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", sheets.NewService)
}

func withSheetsTestService(ctx context.Context, svc *sheets.Service) context.Context {
	return withSheetsTestServiceFactory(ctx, func(context.Context, string) (*sheets.Service, error) {
		return svc, nil
	})
}

func withSheetsTestServiceFactory(ctx context.Context, factory app.SheetsServiceFactory) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime := &app.Runtime{}
	if existing, ok := app.FromContext(ctx); ok {
		*runtime = *existing
	}
	runtime.Services.Sheets = factory
	return app.WithRuntime(ctx, runtime)
}

func executeWithSheetsTestService(t *testing.T, args []string, svc *sheets.Service) executeTestResult {
	t.Helper()
	return executeWithTestRuntime(t, args, &app.Runtime{Services: app.Services{
		Sheets: func(context.Context, string) (*sheets.Service, error) { return svc, nil },
	}})
}

func executeWithSheetsAndDriveTestServices(t *testing.T, args []string, sheetsSvc *sheets.Service, driveSvc *drive.Service) executeTestResult {
	t.Helper()
	return executeWithTestRuntime(t, args, &app.Runtime{Services: app.Services{
		Drive:  stubDriveService(driveSvc),
		Sheets: func(context.Context, string) (*sheets.Service, error) { return sheetsSvc, nil },
	}})
}
