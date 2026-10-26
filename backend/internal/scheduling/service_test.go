package scheduling

import (
	"testing"
	"time"
)

func TestValidateScheduleWindowAndDivisibility(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	now := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	service := &Service{now: func() time.Time { return now }, location: location}
	start := time.Date(2026, 9, 2, 9, 0, 0, 0, location)
	end := start.Add(2 * time.Hour)
	_, _, _, status, err := service.validate(start.Format(time.RFC3339), end.Format(time.RFC3339), 30, "", "")
	if err != nil || status != "DRAFT" {
		t.Fatalf("valid schedule rejected: status=%s err=%v", status, err)
	}
	if _, _, _, _, err := service.validate(start.Format(time.RFC3339), end.Add(5*time.Minute).Format(time.RFC3339), 30, "ACTIVE", ""); err == nil {
		t.Fatal("non-divisible range must be rejected")
	}
	crossDay := time.Date(2026, 9, 3, 0, 30, 0, 0, location)
	if _, _, _, _, err := service.validate(start.Format(time.RFC3339), crossDay.Format(time.RFC3339), 30, "DRAFT", ""); err == nil {
		t.Fatal("cross-local-date schedule must be rejected")
	}
}
