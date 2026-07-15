package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ndelanhese/helio/internal/api"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/storage"
)

var safeBackupDisposition = regexp.MustCompile(`^attachment; filename="helio-backup-[0-9]{8}-[0-9]{6}\.db"$`)

func TestBackupRequiresAuthenticationAndReturnsSafeSQLiteAttachment(t *testing.T) {
	f := newFixture(t)
	unauthorized := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	cookie, _ := bootstrap(t, f)
	response := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("backup status=%d body=%s", response.Code, response.Body.String())
	}
	mediaType, _, err := mime.ParseMediaType(response.Header().Get("Content-Type"))
	if err != nil || mediaType != "application/vnd.sqlite3" {
		t.Fatalf("content type=%q err=%v", response.Header().Get("Content-Type"), err)
	}
	if disposition := response.Header().Get("Content-Disposition"); !safeBackupDisposition.MatchString(disposition) {
		t.Fatalf("unsafe content disposition %q", disposition)
	}
	if !strings.HasPrefix(response.Body.String(), "SQLite format 3\x00") {
		t.Fatalf("backup does not start with SQLite header: %q", response.Body.Bytes()[:min(16, response.Body.Len())])
	}
}

func TestBackupDoesNotStageTheDatabaseInProcessTemp(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	response := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Body.String(), "SQLite format 3\x00") {
		t.Fatalf("backup depended on process temp: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestBackupAuditContainsMetadataButNotArchiveContents(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	first := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first backup: %d %s", first.Code, first.Body.String())
	}
	second := request(t, f.handler, http.MethodGet, "/api/v1/data/backup", "", cookie, "")
	if second.Code != http.StatusOK {
		t.Fatalf("second backup: %d %s", second.Code, second.Body.String())
	}

	path := filepath.Join(t.TempDir(), "audit.db")
	if err := os.WriteFile(path, second.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var action, detail string
	if err := db.QueryRow(`SELECT action, detail_json FROM action_audit WHERE action='data.backup' ORDER BY id DESC LIMIT 1`).Scan(&action, &detail); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(detail), &metadata); err != nil {
		t.Fatal(err)
	}
	filename, ok := metadata["filename"].(string)
	if action != "data.backup" || !ok || !strings.HasPrefix(filename, "helio-backup-") {
		t.Fatalf("audit action=%q detail=%s", action, detail)
	}
	if len(metadata) != 1 || strings.Contains(detail, "SQLite format 3") || strings.Contains(detail, first.Body.String()) {
		t.Fatalf("audit contains archive contents: %s", detail)
	}
}

type disconnectWriter struct {
	header http.Header
	status int
}

func (w *disconnectWriter) Header() http.Header       { return w.header }
func (w *disconnectWriter) WriteHeader(status int)    { w.status = status }
func (w *disconnectWriter) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

type backupTrackingStore struct {
	*storage.DB
	prepared chan struct{}
}

func (s backupTrackingStore) PrepareBackup(ctx context.Context) (io.ReadCloser, error) {
	snapshot, err := s.DB.PrepareBackup(ctx)
	if err == nil {
		close(s.prepared)
	}
	return snapshot, err
}

func TestBackupClientDisconnectClosesPreparedSnapshot(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	store := backupTrackingStore{DB: f.db, prepared: make(chan struct{})}
	handler := api.New(api.Dependencies{Auth: auth.NewManager(f.db), Store: store, History: f.repo, Hub: f.hub})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/data/backup", nil)
	req.AddCookie(cookie)
	w := &disconnectWriter{header: make(http.Header)}
	handler.ServeHTTP(w, req)
	select {
	case <-store.prepared:
	default:
		t.Fatal("backup snapshot was not prepared")
	}
	if leftovers, err := filepath.Glob(filepath.Join(f.dbDir, ".helio-backup-*.db")); err != nil || len(leftovers) != 0 {
		t.Fatalf("prepared backup leaked after disconnect: %v err=%v", leftovers, err)
	}
}
