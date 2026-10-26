package patient

import "testing"

func TestValidatePatientFieldsNormalizesIdentity(t *testing.T) {
	fields, err := validateFields(CreateInput{FullName: "  Nguyễn   Văn  An ", CCCD: "001 234 567 890", HealthInsuranceNumber: "ab1234567890123"})
	if err != nil {
		t.Fatalf("valid patient fields rejected: %v", err)
	}
	if fields.name != "Nguyễn Văn An" || fields.normalizedName != "nguyen van an" {
		t.Fatalf("unexpected normalized name: %q/%q", fields.name, fields.normalizedName)
	}
	if fields.cccd == nil || *fields.cccd != "001234567890" {
		t.Fatalf("unexpected CCCD: %v", fields.cccd)
	}
	if fields.insurance == nil || *fields.insurance != "AB1234567890123" {
		t.Fatalf("unexpected insurance: %v", fields.insurance)
	}
}

func TestValidatePatientFieldsRejectsFutureDOB(t *testing.T) {
	if _, err := validateFields(CreateInput{FullName: "Nguyễn Văn An", DateOfBirth: "2999-01-01"}); err == nil {
		t.Fatal("future date of birth must be rejected")
	}
}
