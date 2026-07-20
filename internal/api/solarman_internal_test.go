package api

import (
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/solarmancloud"
)

func TestCloudHistoryLaggingUsesNewestFrame(t *testing.T) {
	now := time.Date(2026, 7, 20, 17, 0, 0, 0, time.UTC)
	if cloudHistoryLagging([]solarmancloud.Frame{{At: now.Add(-21 * time.Minute)}}, now) == false {
		t.Fatal("21-minute cloud delay must retry")
	}
	if cloudHistoryLagging([]solarmancloud.Frame{{At: now.Add(-time.Hour)}, {At: now.Add(-19 * time.Minute)}}, now) {
		t.Fatal("newest frame within watermark must stop retries")
	}
	if !cloudHistoryLagging(nil, now) {
		t.Fatal("missing cloud frames must retry")
	}
}
