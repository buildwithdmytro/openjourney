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

func (s *Server) createConnectedContentSource(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var source domain.ConnectedContentSource
	if err := decodeJSON(w, r, &source); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if source.AllowedHost == "" {
		writeError(w, http.StatusBadRequest, "invalid_source", "allowed_host is required")
		return
	}
	if source.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_source", "name is required")
		return
	}

	if source.AuthSecretRef == "" && source.AuthHeaderName != "" {
		writeError(w, http.StatusBadRequest, "invalid_source", "auth_secret_ref is required when auth_header_name is set")
		return
	}

	source.CreatedByUserID = &principal.UserID
	res, err := s.store.CreateConnectedContentSource(r.Context(), principal, source)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			writeError(w, http.StatusConflict, "duplicate_source", "a source with this name already exists")
			return
		}
		internalError(w, err, "create connected content source", principal)
		return
	}

	redacted := redactConnectedContentSource(res)
	writeJSON(w, http.StatusCreated, redacted)
}

func (s *Server) listConnectedContentSources(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListConnectedContentSources(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list connected content sources", principal)
		return
	}
	if res == nil {
		res = []domain.ConnectedContentSource{}
	}
	var redacted []domain.ConnectedContentSource
	for _, src := range res {
		redacted = append(redacted, redactConnectedContentSource(src))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": redacted})
}

func (s *Server) getConnectedContentSource(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetConnectedContentSource(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "source not found")
		return
	}
	if err != nil {
		internalError(w, err, "get connected content source", principal)
		return
	}
	redacted := redactConnectedContentSource(res)
	writeJSON(w, http.StatusOK, redacted)
}

func (s *Server) updateConnectedContentSource(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")

	var input domain.ConnectedContentSource
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if input.AllowedHost == "" {
		writeError(w, http.StatusBadRequest, "invalid_source", "allowed_host is required")
		return
	}
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_source", "name is required")
		return
	}

	input.ID = id
	res, err := s.store.UpdateConnectedContentSource(r.Context(), principal, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "source not found")
		return
	}
	if err != nil {
		internalError(w, err, "update connected content source", principal)
		return
	}

	redacted := redactConnectedContentSource(res)
	writeJSON(w, http.StatusOK, redacted)
}

func (s *Server) enableConnectedContentSource(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	if !isHuman(principal) {
		writeError(w, http.StatusForbidden, "human_approval_required", "enabling a connected content source requires an authenticated user")
		return
	}

	id := r.PathValue("id")
	src, err := s.store.GetConnectedContentSource(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "source not found")
		return
	}
	if err != nil {
		internalError(w, err, "get connected content source", principal)
		return
	}

	src.Enabled = true
	src.Status = "active"
	res, err := s.store.UpdateConnectedContentSource(r.Context(), principal, src)
	if err != nil {
		internalError(w, err, "enable connected content source", principal)
		return
	}

	redacted := redactConnectedContentSource(res)
	writeJSON(w, http.StatusOK, redacted)
}

func (s *Server) deleteConnectedContentSource(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	err := s.store.DeleteConnectedContentSource(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "source not found")
		return
	}
	if err != nil {
		internalError(w, err, "delete connected content source", principal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func redactConnectedContentSource(src domain.ConnectedContentSource) domain.ConnectedContentSource {
	src.AuthSecretRef = ""
	return src
}
