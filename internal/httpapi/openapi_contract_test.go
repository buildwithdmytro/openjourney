package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPIDocumentsRuntimeErrorResponses(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(content)
	assertOperationDocumentsStatuses(t, document, "/v1/events/batch:", "/v1/profiles/{externalID}:",
		[]string{`"400":`, `"401":`, `"403":`, `"409":`, `"422":`, `"429":`, `"500":`})
	assertOperationDocumentsStatuses(t, document, "/v1/profiles/{externalID}:", "/v1/schemas:",
		[]string{`"401":`, `"403":`, `"404":`, `"500":`})
	assertOperationDocumentsStatuses(t, document, "/v1/schemas:", "/v1/api-keys:",
		[]string{`"401":`, `"403":`})
	assertOperationDocumentsStatuses(t, document, "/v1/operations/replay/verify:", "/v1/roles:",
		[]string{`"401":`, `"403":`, `"409":`})
}

func assertOperationDocumentsStatuses(t *testing.T, document, operationStart, operationEnd string, statuses []string) {
	t.Helper()
	start := strings.Index(document, operationStart)
	if start < 0 {
		t.Fatalf("OpenAPI operation %s not found", operationStart)
	}
	end := strings.Index(document[start+len(operationStart):], operationEnd)
	if end < 0 {
		t.Fatalf("OpenAPI operation end %s not found after %s", operationEnd, operationStart)
	}
	operation := document[start : start+len(operationStart)+end]
	for _, status := range statuses {
		if !strings.Contains(operation, status) {
			t.Fatalf("OpenAPI operation %s does not document response %s", operationStart, status)
		}
	}
}
