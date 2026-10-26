-- Read-only billing queue projection. No clinical data or encounter permission.
INSERT INTO permissions(code,module,name,risk_level)
VALUES ('billing.pending_encounter.view','BILLING_PAYMENT','Xem bệnh án chờ thu tiền','NORMAL')
ON CONFLICT (code) DO NOTHING;
INSERT INTO role_permissions(role_id,permission_id)
SELECT r.id,p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('ADMIN','CASHIER') AND p.code='billing.pending_encounter.view'
ON CONFLICT DO NOTHING;
