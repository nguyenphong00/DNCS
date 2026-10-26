package settings

import (
	"context"
	"dncs/backend/internal/platform/problem"
	"strings"
)

type AuditOption struct {
	ID         int64  `json:"id"`
	Label      string `json:"label"`
	Code       string `json:"code,omitempty"`
	TargetType string `json:"target_type,omitempty"`
}

func (s *Service) AuditActors(ctx context.Context, search string) ([]AuditOption, error) {
	if len(search) > 150 {
		return nil, problem.New(422, "AUDIT_FILTER_INVALID", "Từ khóa quá dài.")
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT a.actor_user_id,`+actorNameSQL+` AS label,COALESCE(sp.staff_code,pa.patient_code,u.username,'') AS code FROM audit_logs a `+actorJoinsSQL+` WHERE a.actor_user_id IS NOT NULL AND ($1='' OR `+actorNameSQL+` ILIKE '%'||$1||'%' OR u.username ILIKE '%'||$1||'%' OR sp.staff_code ILIKE '%'||$1||'%' OR pa.patient_code ILIKE '%'||$1||'%') ORDER BY label,a.actor_user_id LIMIT 30`, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditOption{}
	for rows.Next() {
		var x AuditOption
		if err = rows.Scan(&x.ID, &x.Label, &x.Code); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) AuditTargets(ctx context.Context, targetType, search string) ([]AuditOption, []string, error) {
	targetType = strings.ToUpper(strings.TrimSpace(targetType))
	if len(targetType) > 80 || len(search) > 150 {
		return nil, nil, problem.New(422, "AUDIT_FILTER_INVALID", "Bộ lọc không hợp lệ.")
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT target_type FROM audit_logs WHERE target_type IS NOT NULL AND target_id IS NOT NULL ORDER BY target_type`)
	if err != nil {
		return nil, nil, err
	}
	types := []string{}
	for rows.Next() {
		var x string
		if err = rows.Scan(&x); err != nil {
			rows.Close()
			return nil, nil, err
		}
		types = append(types, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	out := []AuditOption{}
	if targetType == "" {
		return out, types, nil
	}
	rows, err = s.pool.Query(ctx, `WITH targets AS (SELECT DISTINCT ON (target_id) * FROM audit_logs WHERE target_type=$1 AND target_id IS NOT NULL ORDER BY target_id,occurred_at DESC,id DESC), labeled AS (SELECT a.target_id,`+targetLabelSQL+` AS label FROM targets a) SELECT target_id,label FROM labeled WHERE ($2='' OR label ILIKE '%'||$2||'%') ORDER BY label,target_id LIMIT 30`, targetType, strings.TrimSpace(search))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		x := AuditOption{TargetType: targetType}
		if err = rows.Scan(&x.ID, &x.Label); err != nil {
			return nil, nil, err
		}
		out = append(out, x)
	}
	return out, types, rows.Err()
}
