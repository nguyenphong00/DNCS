package httpapi

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestOpenAPIHasExactRouterOperationCoverage(t *testing.T) {
	type operation struct {
		OperationID string                     `json:"operationId"`
		Responses   map[string]json.RawMessage `json:"responses"`
	}
	type document struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Hardening  map[string]json.RawMessage            `json:"x-hardening"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Headers map[string]json.RawMessage `json:"headers"`
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	content, err := os.ReadFile("../../openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec document
	if err := json.Unmarshal(content, &spec); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1.") {
		t.Fatalf("OpenAPI version must be 3.1.x, got %q", spec.OpenAPI)
	}
	if spec.Info.Version != "0.7.0" || len(spec.Hardening) == 0 {
		t.Fatalf("P6 hardening contract missing: version=%q hardening=%v", spec.Info.Version, spec.Hardening)
	}
	for _, header := range []string{"RequestId", "CorrelationId", "RetryAfter"} {
		if _, exists := spec.Components.Headers[header]; !exists {
			t.Errorf("P6 response header component %q is missing", header)
		}
	}
	var errorEnvelope struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(spec.Components.Schemas["ErrorEnvelope"], &errorEnvelope); err != nil {
		t.Fatalf("parse error envelope: %v", err)
	}
	if _, exists := errorEnvelope.Properties["request_id"]; !exists {
		t.Error("error envelope must expose request_id")
	}

	routerSource, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) (/[^" ]+)"`)
	actual := make(map[string]bool)
	for _, match := range pattern.FindAllStringSubmatch(string(routerSource), -1) {
		actual[match[1]+" "+match[2]] = true
	}
	documented := make(map[string]bool)
	operationIDs := make(map[string]string)
	validMethods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	for path, pathItem := range spec.Paths {
		for method, raw := range pathItem {
			if !validMethods[method] {
				continue
			}
			key := strings.ToUpper(method) + " " + path
			documented[key] = true
			var item operation
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatalf("parse operation %s: %v", key, err)
			}
			if item.OperationID == "" || len(item.Responses) == 0 {
				t.Errorf("%s must define operationId and responses", key)
			}
			if previous, exists := operationIDs[item.OperationID]; exists {
				t.Errorf("duplicate operationId %q at %s and %s", item.OperationID, previous, key)
			}
			operationIDs[item.OperationID] = key
		}
	}

	missing := difference(actual, documented)
	extra := difference(documented, actual)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("OpenAPI/router mismatch\nmissing: %v\nextra: %v", missing, extra)
	}
}

func difference(left, right map[string]bool) []string {
	result := make([]string, 0)
	for item := range left {
		if !right[item] {
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
