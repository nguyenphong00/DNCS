# ERD — DNCS MVP

## 1. Phạm vi và ký hiệu

ERD này mô tả 42 bảng của baseline `MVP-BL-01`. Quan hệ polymorphic được vẽ nét logic trong phần ghi chú vì PostgreSQL không thể tạo một foreign key trỏ tới nhiều bảng.

- `||`: đúng một.
- `o|`: không hoặc một.
- `o{`: không hoặc nhiều.
- `|{`: một hoặc nhiều.

## 2. Bản đồ phụ thuộc tổng thể

```mermaid
flowchart TD
    IAM[Identity & Authorization] --> PROFILE[Staff / Patient Profiles]
    IAM --> PLATFORM[Settings / Audit / Idempotency]
    PROFILE --> MASTER[Clinic Master Data]
    MASTER --> SCHED[Doctor Scheduling]
    SCHED --> APPT[Appointment]
    PROFILE --> APPT
    MASTER --> SERVICE[Service Catalog]
    APPT --> CLINICAL[Medical Encounter]
    SERVICE --> CLINICAL
    MASTER --> MED[Medicine & Inventory]
    CLINICAL --> RX[Prescription]
    MED --> RX
    CLINICAL --> BILL[Billing & Payment]
    SERVICE --> BILL
    RX --> BILL
    MED --> BILL
    PLATFORM --> SCHED
    PLATFORM --> APPT
    PLATFORM --> BILL
```

## 3. Identity, authorization và profile

```mermaid
erDiagram
    GENDER_CODES ||--o{ STAFF_PROFILES : classifies
    GENDER_CODES ||--o{ PATIENTS : classifies
    ROLES ||--o{ USERS : assigned_to
    ROLES ||--o{ ROLE_PERMISSIONS : grants
    PERMISSIONS ||--o{ ROLE_PERMISSIONS : included_in
    USERS ||--o{ USER_PERMISSION_OVERRIDES : receives
    PERMISSIONS ||--o{ USER_PERMISSION_OVERRIDES : overrides
    USERS ||--o{ USER_ROLE_HISTORIES : changes
    ROLES ||--o{ USER_ROLE_HISTORIES : from_or_to
    USERS ||--o{ AUTH_SESSIONS : owns
    USERS ||--o| STAFF_PROFILES : staff_account
    USERS ||--o| PATIENTS : patient_account
    STAFF_PROFILES ||--o| DOCTOR_PROFILES : may_be
```

Ràng buộc “user STAFF phải có staff profile, user PATIENT phải có patient” là invariant aggregate và được bảo vệ trong transaction tạo/archive/restore; FK đơn lẻ chỉ bảo vệ chiều profile → user.

## 4. Clinic master, scheduling và appointment

```mermaid
erDiagram
    DOCTOR_PROFILES ||--o{ DOCTOR_SPECIALTIES : assigned
    SPECIALTIES ||--o{ DOCTOR_SPECIALTIES : contains
    DOCTOR_PROFILES ||--o{ DOCTOR_SCHEDULES : works
    ROOMS ||--o{ DOCTOR_SCHEDULES : allocated
    DOCTOR_SCHEDULES ||--o{ APPOINTMENTS : contains
    PATIENTS ||--o{ APPOINTMENTS : books
    DOCTOR_PROFILES ||--o{ APPOINTMENTS : receives
    SPECIALTIES ||--o{ APPOINTMENTS : selected
    ROOMS ||--o{ APPOINTMENTS : occurs_in
    APPOINTMENTS ||--|{ APPOINTMENT_STATUS_HISTORIES : transitions
    APPOINTMENTS ||--o| MEDICAL_ENCOUNTERS : starts
    MEDICAL_ENCOUNTERS ||--o{ APPOINTMENTS : follow_up_source
```

Hai vòng tham chiếu appointment–encounter có semantics khác nhau:

- `medical_encounters.appointment_id` là quan hệ nguồn `1 : 0..1` và có unique.
- `appointments.follow_up_from_encounter_id` chỉ dùng khi `source = FOLLOW_UP`; lịch terminal được giữ lịch sử nên một encounter có thể có nhiều follow-up lịch sử nhưng tối đa một lịch đang giữ chỗ.

## 5. Clinical, service và prescription

```mermaid
erDiagram
    SPECIALTIES ||--o{ SERVICES : consultation_for
    SERVICES ||--o{ SERVICE_PRICE_VERSIONS : priced_by
    APPOINTMENTS ||--o| MEDICAL_ENCOUNTERS : produces
    PATIENTS ||--o{ MEDICAL_ENCOUNTERS : has
    DOCTOR_PROFILES ||--o{ MEDICAL_ENCOUNTERS : performs
    SPECIALTIES ||--o{ MEDICAL_ENCOUNTERS : classifies
    MEDICAL_ENCOUNTERS ||--o{ ENCOUNTER_VITAL_SIGNS : records
    MEDICAL_ENCOUNTERS ||--|{ MEDICAL_ENCOUNTER_STATUS_HISTORIES : transitions
    MEDICAL_ENCOUNTERS ||--o{ MEDICAL_ENCOUNTER_AMENDMENTS : appends
    MEDICAL_ENCOUNTERS ||--o{ ENCOUNTER_SERVICES : orders
    SERVICES ||--o{ ENCOUNTER_SERVICES : selected
    SERVICE_PRICE_VERSIONS ||--o{ ENCOUNTER_SERVICES : price_snapshot_source
    MEDICAL_ENCOUNTERS ||--o{ PRESCRIPTION_SERIES : produces
    PRESCRIPTION_SERIES ||--|{ PRESCRIPTIONS : versions
    PRESCRIPTIONS ||--o{ PRESCRIPTION_ITEMS : contains
    PRESCRIPTIONS o|--o{ PRESCRIPTIONS : replacement_source
    MEDICINES ||--o{ PRESCRIPTION_ITEMS : prescribed
```

`prescription_series.current_issued_prescription_id` tạo vòng tham chiếu với `prescriptions.series_id`. Composite FK `(current_issued_prescription_id, id) → prescriptions(id, series_id)` bảo đảm current version thuộc đúng series.

## 6. Medicine và inventory

```mermaid
erDiagram
    MEDICINES ||--o{ MEDICINE_PRICE_VERSIONS : priced_by
    MEDICINES ||--o{ MEDICINE_BATCHES : stocked_as
    STOCK_RECEIPTS ||--|{ STOCK_RECEIPT_ITEMS : contains
    MEDICINES ||--o{ STOCK_RECEIPT_ITEMS : received
    STOCK_RECEIPT_ITEMS ||--|| MEDICINE_BATCHES : creates
    INVENTORY_ADJUSTMENTS ||--|{ INVENTORY_ADJUSTMENT_ITEMS : contains
    MEDICINE_BATCHES ||--o{ INVENTORY_ADJUSTMENT_ITEMS : adjusts
    MEDICINE_BATCHES ||--o{ STOCK_MOVEMENTS : ledger
    MEDICINES ||--o{ STOCK_MOVEMENTS : groups
```

`stock_movements.reference_type/reference_id` trỏ logic tới một trong `stock_receipt_items`, `inventory_adjustment_items` hoặc `invoice_items`. Không tạo polymorphic FK; command tương ứng là writer duy nhất.

## 7. Billing, payment và platform

```mermaid
erDiagram
    MEDICAL_ENCOUNTERS ||--o{ INVOICES : billed_from
    PATIENTS ||--o{ INVOICES : billed_to
    INVOICES ||--|{ INVOICE_ITEMS : contains
    ENCOUNTER_SERVICES ||--o| INVOICE_ITEMS : service_source
    PRESCRIPTION_ITEMS ||--o| INVOICE_ITEMS : medicine_source
    SERVICE_PRICE_VERSIONS ||--o{ INVOICE_ITEMS : service_price_source
    MEDICINE_PRICE_VERSIONS ||--o{ INVOICE_ITEMS : medicine_price_source
    INVOICES ||--|| PAYMENTS : paid_by
    BANK_ACCOUNTS ||--o{ PAYMENTS : snapshotted_from
    INVOICE_ITEMS ||--o{ STOCK_MOVEMENTS : allocated_to
    USERS ||--o{ AUDIT_LOGS : acts
    USERS ||--o{ IDEMPOTENCY_RECORDS : owns
    USERS ||--o{ SYSTEM_SETTINGS : updates
    USERS ||--o{ BANK_ACCOUNTS : manages
```

`audit_logs.target_type/target_id`, `idempotency_records.resource_type/resource_id` và stock movement source là logical reference. Những cặp này dùng allowlist, command guard và audit/test thay cho generic FK.

## 8. Cardinality/invariant quan trọng

| Quan hệ | Cardinality/Invariant |
| --- | --- |
| User → role | Mỗi user đúng một role hiện hành |
| User → staff/patient | Tối đa một profile; loại profile phải khớp `account_type` |
| Staff → doctor | Tối đa một doctor identity trong toàn vòng đời |
| Doctor ↔ specialty | N:M; tối đa một relation active cho mỗi cặp |
| Doctor/room → active schedule | Không có hai range chồng lấn |
| Doctor/patient/room → holding appointment | Không có hai range chồng lấn |
| Appointment → encounter | Tối đa một encounter |
| Encounter → vital signs | Draft có 0..N; finalized có ít nhất một |
| Specialty → active consultation service | Tối đa một |
| Encounter → active service | Tối đa một row cho mỗi service |
| Series → prescription version | Tối đa một DRAFT và một ISSUED |
| Prescription → active medicine item | Tối đa một row cho mỗi medicine |
| Medicine → batch | Lot number duy nhất trong medicine |
| Encounter → unpaid invoice | Tối đa một; PAID history có thể nhiều |
| Source service/prescription item → invoice item | Tối đa một trên toàn lịch sử |
| Invoice → payment | Đúng một, được tạo cùng transaction |
| Invoice medicine item → stock movement | 0..N allocation batch; sau confirm phải đủ tổng quantity |

