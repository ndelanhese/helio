package api

import (
	"encoding/json"
	"testing"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestUTCSnapshotEncodesMissingFaultCodesAsEmptyArray(t *testing.T) {
	snapshot := utcSnapshot(&domain.TelemetrySnapshot{})
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		FaultCodes []uint16 `json:"faultCodes"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if response.FaultCodes == nil || len(response.FaultCodes) != 0 {
		t.Fatalf("faultCodes = %#v, want non-nil empty slice", response.FaultCodes)
	}
}
