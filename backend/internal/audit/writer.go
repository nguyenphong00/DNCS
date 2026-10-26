package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dncs/backend/internal/platform/requestmeta"
	"github.com/jackc/pgx/v5"
)

type Event struct {
	ActorType    string
	ActorUserID  *int64
	ActorService *string
	Module       string
	ActionCode   string
	Outcome      string
	TargetType   string
	TargetID     int64
	TargetCode   string
	Metadata     any
	Before       any
	After        any
}

func UserEvent(actorUserID int64, module, actionCode, targetType string, targetID int64) Event {
	return Event{
		ActorType: "USER", ActorUserID: &actorUserID, Module: module,
		ActionCode: actionCode, Outcome: "SUCCESS", TargetType: targetType, TargetID: targetID,
	}
}

func SystemEvent(module, actionCode, targetType string, targetID int64) Event {
	return Event{
		ActorType: "SYSTEM", Module: module, ActionCode: actionCode,
		Outcome: "SUCCESS", TargetType: targetType, TargetID: targetID,
	}
}

func Insert(ctx context.Context, tx pgx.Tx, event Event) error {
	requestMetadata, _ := requestmeta.FromContext(ctx)
	metadata, err := marshalJSON(event.Metadata, false)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	before, err := marshalJSON(event.Before, true)
	if err != nil {
		return fmt.Errorf("marshal audit before snapshot: %w", err)
	}
	after, err := marshalJSON(event.After, true)
	if err != nil {
		return fmt.Errorf("marshal audit after snapshot: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs(
			occurred_at, actor_type, actor_user_id, actor_service, module, action_code, outcome,
			target_type, target_id, target_code, correlation_id, request_id, ip_hash,
			metadata, before_snapshot, after_snapshot,
			retention_until
		) VALUES(
			now(), $1, $2, $3, $4, $5, $6, NULLIF($7,''), NULLIF($8,0), NULLIF($9,''),
			NULLIF($10,'')::uuid, NULLIF($11,'')::uuid, NULLIF($12,''),
			$13::jsonb, $14::jsonb, $15::jsonb, now() + interval '7 years'
		)`,
		event.ActorType, event.ActorUserID, event.ActorService, event.Module, event.ActionCode,
		event.Outcome, event.TargetType, event.TargetID, event.TargetCode,
		requestMetadata.CorrelationID, requestMetadata.RequestID, requestMetadata.IPHash,
		metadata, before, after,
	)
	if err != nil {
		return fmt.Errorf("insert audit event %s: %w", event.ActionCode, err)
	}
	return nil
}

func marshalJSON(value any, nullable bool) ([]byte, error) {
	if value == nil {
		if nullable {
			return nil, nil
		}
		return []byte(`{}`), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 16*1024 {
		return nil, fmt.Errorf("audit JSON exceeds 16 KiB")
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	if field, found := prohibitedField(decoded); found {
		return nil, fmt.Errorf("audit JSON contains prohibited field %q", field)
	}
	if field, found := unknownField(decoded); found {
		return nil, fmt.Errorf("audit JSON contains non-allowlisted field %q", field)
	}
	return encoded, nil
}

var prohibitedFields = map[string]struct{}{
	"access_token": {}, "refresh_token": {}, "token": {}, "secret": {}, "otp": {},
	"password": {}, "password_hash": {}, "temporary_password": {},
	"clinical_notes": {}, "symptoms": {}, "diagnosis": {}, "medical_history": {},
	"allergies": {}, "prescription_instruction": {}, "prescription_notes": {},
	"account_number": {}, "bank_account_number": {}, "bank_reference": {}, "qr_payload": {},
	"address": {}, "cccd": {}, "phone": {}, "phone_number": {}, "email": {},
	"health_insurance_number": {},
}

var allowedFields = map[string]struct{}{
	"action": {}, "allocation_count": {}, "amendment_type": {}, "amount_vnd": {},
	"appointment_auto_approval_enabled": {}, "appointment_booking_max_advance_days": {},
	"appointment_booking_min_lead_minutes": {}, "appointment_check_in_early_minutes": {},
	"appointment_check_in_late_minutes": {}, "appointment_max_holding_future_per_patient": {},
	"appointment_no_show_grace_minutes": {}, "bank_account_id": {}, "bank_bin": {},
	"bank_name": {}, "changed_keys": {}, "confirmed_existing_overrides": {},
	"default_slot_duration_minutes": {}, "doctor_profile_created": {}, "doctor_profile_id": {}, "effective_from": {},
	"encounter_id": {}, "expected_version": {}, "fields": {}, "from": {},
	"from_status": {}, "from_version": {}, "initial_status": {}, "inventory_expiry_alert_days": {},
	"invoice_code": {}, "invoice_id": {}, "item_count": {}, "key": {}, "medicine_count": {},
	"movement_ids": {}, "new_value": {}, "old_value": {}, "patient_id": {},
	"payment_method": {}, "qr_sha256": {}, "quantity": {}, "reason": {},
	"revoked_all_sessions": {}, "role_id": {}, "schedule_id": {}, "sequence_number": {},
	"service_count": {}, "service_id": {}, "service_type": {}, "source": {},
	"specialty_id": {}, "staff_profile_id": {}, "status": {}, "to": {}, "to_status": {},
	"to_version": {}, "total_vnd": {}, "unit_price_vnd": {}, "user_id": {},
}

func prohibitedField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if _, denied := prohibitedFields[normalized]; denied {
				return normalized, true
			}
			if field, denied := prohibitedField(child); denied {
				return field, true
			}
		}
	case []any:
		for _, child := range typed {
			if field, denied := prohibitedField(child); denied {
				return field, true
			}
		}
	}
	return "", false
}

func unknownField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if _, allowed := allowedFields[normalized]; !allowed {
				return normalized, true
			}
			if field, unknown := unknownField(child); unknown {
				return field, true
			}
		}
	case []any:
		for _, child := range typed {
			if field, unknown := unknownField(child); unknown {
				return field, true
			}
		}
	}
	return "", false
}
