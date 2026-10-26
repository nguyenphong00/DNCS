package authorization

import (
	"context"
	"log/slog"
	"net/http"

	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/problem"
)

type AssignableRole struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// AssignableStaffRoles mirrors the eligibility rules of staff account creation.
// SUPER_ADMIN is provisioned exclusively through the protected bootstrap path.
func (store *Store) AssignableStaffRoles(ctx context.Context) ([]AssignableRole, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,code,CASE code WHEN 'ADMIN' THEN 'Quản lý' WHEN 'DOCTOR' THEN 'Bác sĩ' WHEN 'CASHIER' THEN 'Thu ngân' ELSE name END
	 FROM roles WHERE account_scope='STAFF' AND status='ACTIVE' AND NOT is_deleted AND code<>'SUPER_ADMIN' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []AssignableRole{}
	for rows.Next() {
		var role AssignableRole
		if err := rows.Scan(&role.ID, &role.Code, &role.Name); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (store *Store) RolesHandler(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if scope := r.URL.Query().Get("account_scope"); scope != "" && scope != "STAFF" {
			httpx.WriteError(w, 422, "AUTHZ_ROLE_SCOPE_INVALID", "Chỉ hỗ trợ danh mục vai trò nhân viên.")
			return
		}
		roles, err := store.AssignableStaffRoles(r.Context())
		if err != nil {
			problem.Write(w, r, logger, "list assignable roles", err)
			return
		}
		httpx.WriteJSON(w, 200, map[string]any{"items": roles})
	}
}
