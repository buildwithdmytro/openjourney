package httpapi

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) bulkUploadCatalogItems(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	catalogID := r.PathValue("id")

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required", "file is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 20<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_file", err.Error())
		return
	}

	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "file_required", "file is empty")
		return
	}

	// Parse the file based on format
	var items []domain.CatalogItem
	var failedRows []map[string]any

	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("[")) || bytes.HasPrefix(trimmed, []byte("{")) {
		// Try JSON format first (newline-delimited JSON or array)
		items, failedRows = parseJSON(data)
		if len(items) == 0 && len(failedRows) == 0 {
			// JSON parsing failed, try CSV
			items, failedRows = parseCSV(data)
		}
	} else {
		// Try CSV format
		items, failedRows = parseCSV(data)
	}

	if len(items) == 0 && len(failedRows) > 0 {
		writeError(w, http.StatusBadRequest, "invalid_data", "no valid rows found in file")
		return
	}

	// Get the catalog to verify it exists
	cat, err := s.store.GetCatalog(r.Context(), principal, catalogID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "catalog not found")
		return
	}
	if err != nil {
		internalError(w, err, "get catalog", principal)
		return
	}

	// Set tenant/app ID for items
	for i := range items {
		items[i].CatalogID = cat.ID
		items[i].TenantID = principal.TenantID
		items[i].AppID = principal.AppID
	}

	// Bulk upsert items
	result, err := s.store.BulkUpsertCatalogItems(r.Context(), principal, items)
	if err != nil {
		internalError(w, err, "bulk upsert catalog items", principal)
		return
	}

	response := map[string]any{
		"inserted":    result.InsertedCount,
		"updated":     result.UpdatedCount,
		"total":       len(items),
		"failed_rows": failedRows,
	}

	writeJSON(w, http.StatusAccepted, response)
}

func parseJSON(data []byte) ([]domain.CatalogItem, []map[string]any) {
	var items []domain.CatalogItem
	var failedRows []map[string]any

	trimmed := bytes.TrimSpace(data)
	// Try parsing as array
	if bytes.HasPrefix(trimmed, []byte("[")) {
		var arr []map[string]any
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, nil
		}
		for i, obj := range arr {
			payload, _ := json.Marshal(obj)
			key, _ := obj["item_key"].(string)
			if key == "" {
				failedRows = append(failedRows, map[string]any{"row": i + 1, "error": "item_key is required"})
				continue
			}
			items = append(items, domain.CatalogItem{
				ItemKey: key,
				Payload: payload,
			})
		}
		return items, failedRows
	}

	// Try parsing as newline-delimited JSON
	scanner := bufio.NewScanner(bytes.NewReader(data))
	rowNum := 1
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			rowNum++
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			failedRows = append(failedRows, map[string]any{"row": rowNum, "error": err.Error()})
			rowNum++
			continue
		}
		payload, _ := json.Marshal(obj)
		key, _ := obj["item_key"].(string)
		if key == "" {
			failedRows = append(failedRows, map[string]any{"row": rowNum, "error": "item_key is required"})
			rowNum++
			continue
		}
		items = append(items, domain.CatalogItem{
			ItemKey: key,
			Payload: payload,
		})
		rowNum++
	}

	return items, failedRows
}

func parseCSV(data []byte) ([]domain.CatalogItem, []map[string]any) {
	var items []domain.CatalogItem
	var failedRows []map[string]any

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // Allow variable number of fields

	// Read header
	header, err := reader.Read()
	if err != nil {
		failedRows = append(failedRows, map[string]any{"error": "failed to read CSV header"})
		return items, failedRows
	}

	// Find item_key column
	itemKeyIdx := -1
	for i, col := range header {
		if strings.TrimSpace(col) == "item_key" {
			itemKeyIdx = i
			break
		}
	}

	if itemKeyIdx == -1 {
		failedRows = append(failedRows, map[string]any{"error": "item_key column not found"})
		return items, failedRows
	}

	// Read rows
	rowNum := 2
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			failedRows = append(failedRows, map[string]any{
				"row":   rowNum,
				"error": err.Error(),
			})
			rowNum++
			continue
		}

		// Build payload from all columns
		payload := make(map[string]any)
		for i, col := range header {
			if i < len(record) {
				payload[col] = record[i]
			}
		}

		itemKey := ""
		if itemKeyIdx < len(record) {
			itemKey = strings.TrimSpace(record[itemKeyIdx])
		}

		if itemKey == "" {
			failedRows = append(failedRows, map[string]any{
				"row":   rowNum,
				"error": "item_key is empty",
			})
			rowNum++
			continue
		}

		payloadJSON, _ := json.Marshal(payload)
		items = append(items, domain.CatalogItem{
			ItemKey: itemKey,
			Payload: payloadJSON,
		})

		rowNum++
	}

	return items, failedRows
}

func (s *Server) listCatalogItems(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	catalogID := r.PathValue("id")

	// Parse limit query parameter
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil {
			limit = parsed
		}
	}

	res, err := s.store.ListCatalogItems(r.Context(), principal, catalogID, limit)
	if err != nil {
		internalError(w, err, "list catalog items", principal)
		return
	}
	if res == nil {
		res = []domain.CatalogItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": res})
}
