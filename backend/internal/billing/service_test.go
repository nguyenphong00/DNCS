package billing

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"dncs/backend/internal/settings"
)

func TestVietQRPayloadHasStableCRCAndUTF8SafeFields(t *testing.T) {
	bank := settings.ActiveBankAccount{ID: 1, BankBIN: "970415", AccountNumber: "123456789", AccountName: "PHÒNG KHÁM ĐA KHOA SỨC KHỎE"}
	payload := vietQRPayload(bank, 175000, "DNCS-INV-20260901-00000001")
	if len(payload) < 8 || !strings.Contains(payload, bank.BankBIN) || !strings.Contains(payload, bank.AccountNumber) {
		t.Fatalf("missing required VietQR snapshot fields: %q", payload)
	}
	want := fmt.Sprintf("%04X", crc16(payload[:len(payload)-4]))
	if payload[len(payload)-4:] != want {
		t.Fatalf("invalid VietQR CRC: got %s want %s", payload[len(payload)-4:], want)
	}
	name := qrSafe(bank.AccountName, 25)
	if !utf8.ValidString(name) || len([]byte(name)) > 25 {
		t.Fatalf("QR account name is not a valid UTF-8 field: %q (%d bytes)", name, len([]byte(name)))
	}
}

func TestBankReferenceUsesNFKCTrimAndUppercase(t *testing.T) {
	display, normalized, err := normalizeBankReference("  ｔｘ-p5-001  ")
	if err != nil {
		t.Fatal(err)
	}
	if display != "tx-p5-001" || normalized != "TX-P5-001" {
		t.Fatalf("unexpected reference normalization: display=%q normalized=%q", display, normalized)
	}
	if _, _, err = normalizeBankReference(strings.Repeat("x", 101)); err == nil {
		t.Fatal("accepted overlong bank reference")
	}
}

func TestTLVUsesByteLength(t *testing.T) {
	value := "ĐA KHOA"
	encoded := tlv("59", value)
	wantLength := fmt.Sprintf("%02d", len([]byte(value)))
	if encoded[:4] != "59"+wantLength {
		t.Fatalf("TLV byte length=%s want=%s", encoded[2:4], wantLength)
	}
	if parsed, err := strconv.Atoi(encoded[2:4]); err != nil || parsed != len([]byte(value)) {
		t.Fatalf("invalid TLV length: %q", encoded)
	}
}

func TestVietQRASCIIFieldsAndUniqueShortContent(t *testing.T) {
	bank := settings.ActiveBankAccount{BankBIN: "970415", AccountNumber: "123456789", AccountName: "Phòng khám Đặng Đức"}
	content := transferContent(9223372036854775807)
	if len(content) > 25 || content == transferContent(9223372036854775806) {
		t.Fatal("content length/uniqueness")
	}
	payload := vietQRPayload(bank, 2000, content)
	fields := map[string]string{}
	for offset := 0; offset < len(payload); {
		if offset+4 > len(payload) {
			t.Fatal("truncated TLV")
		}
		n, e := strconv.Atoi(payload[offset+2 : offset+4])
		if e != nil || offset+4+n > len(payload) {
			t.Fatal("invalid TLV length")
		}
		fields[payload[offset:offset+2]] = payload[offset+4 : offset+4+n]
		offset += 4 + n
	}
	if fields["59"] != "PHONG KHAM DANG DUC" {
		t.Fatalf("ASCII name: %q", fields["59"])
	}
	if fields["54"] != "2000" || fields["53"] != "704" || fields["58"] != "VN" || fields["62"] != tlv("08", content) {
		t.Fatal("amount/currency/content mismatch")
	}
	if fields["60"] != "" {
		t.Fatal("hardcoded city")
	}
	if crc16("123456789") != 0x29B1 {
		t.Fatal("CRC reference vector")
	}
	if fields["63"] != fmt.Sprintf("%04X", crc16(payload[:len(payload)-4])) {
		t.Fatal("CRC mismatch")
	}
}
