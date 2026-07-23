package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestDSRVerificationSLAAndRejectLifecycle(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	p, _ := setupTestTenant(t, ctx, store)
	p.Scopes = []string{"privacy:write", "privacy:read", "privacy:approve"}

	// 1. Create a privacy request. It should start unverified, have SLA due date set, and NOT enqueue job.
	req, err := store.CreatePrivacyRequest(ctx, p, "user-dsr-101", "export")
	if err != nil {
		t.Fatalf("CreatePrivacyRequest failed: %v", err)
	}

	if req.VerificationStatus != "unverified" {
		t.Fatalf("expected verification_status 'unverified', got %q", req.VerificationStatus)
	}
	if req.SLADueAt == nil || req.SLADueAt.Before(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("expected SLA due date around 30 days in future, got %v", req.SLADueAt)
	}
	if req.VerificationToken == "" {
		t.Fatal("expected non-empty VerificationToken")
	}

	// Assert NO operation_jobs row was created for unverified request
	var jobCount int
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM operation_jobs WHERE payload->>'request_id' = $1`, req.ID).Scan(&jobCount)
	if err != nil {
		t.Fatalf("querying operation_jobs: %v", err)
	}
	if jobCount != 0 {
		t.Fatalf("expected 0 operation_jobs for unverified request, got %d", jobCount)
	}

	// 2. Verify with wrong token fails
	_, err = store.VerifyPrivacyRequest(ctx, p, req.ID, "wrong-token")
	if err == nil {
		t.Fatal("expected error verifying with wrong token, got nil")
	}

	// 3. Verify with correct token succeeds and enqueues job
	verifiedReq, err := store.VerifyPrivacyRequest(ctx, p, req.ID, req.VerificationToken)
	if err != nil {
		t.Fatalf("VerifyPrivacyRequest failed: %v", err)
	}
	if verifiedReq.VerificationStatus != "verified" {
		t.Fatalf("expected verification_status 'verified', got %q", verifiedReq.VerificationStatus)
	}

	// Assert operation_jobs row WAS created now
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM operation_jobs WHERE payload->>'request_id' = $1`, req.ID).Scan(&jobCount)
	if err != nil {
		t.Fatalf("querying operation_jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("expected 1 operation_job after verification, got %d", jobCount)
	}

	// 4. Create a second request and test reject workflow (terminal state)
	req2, err := store.CreatePrivacyRequest(ctx, p, "user-dsr-102", "delete")
	if err != nil {
		t.Fatalf("CreatePrivacyRequest req2 failed: %v", err)
	}

	rejectedReq, err := store.RejectPrivacyRequest(ctx, p, req2.ID, "identity proof failed")
	if err != nil {
		t.Fatalf("RejectPrivacyRequest failed: %v", err)
	}
	if rejectedReq.VerificationStatus != "rejected" || rejectedReq.Status != "rejected" {
		t.Fatalf("expected status & verification_status 'rejected', got status=%q vstatus=%q", rejectedReq.Status, rejectedReq.VerificationStatus)
	}
	if rejectedReq.Error != "identity proof failed" {
		t.Fatalf("expected error message 'identity proof failed', got %q", rejectedReq.Error)
	}

	// Assert verifying a rejected request is blocked (terminal state)
	_, err = store.VerifyPrivacyRequest(ctx, p, req2.ID, req2.VerificationToken)
	if err == nil {
		t.Fatal("expected error attempting to verify a rejected privacy request, got nil")
	}

	// Assert audit events were recorded for create, verify, reject
	auditEvents, err := store.ListAuditEvents(ctx, p, 50)
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}

	hasVerify, hasReject := false, false
	for _, ev := range auditEvents {
		if ev.ResourceID == req.ID && ev.Action == "privacy.verify" {
			hasVerify = true
		}
		if ev.ResourceID == req2.ID && ev.Action == "privacy.reject" {
			var meta map[string]any
			_ = json.Unmarshal(ev.Metadata, &meta)
			if meta["reason"] == "identity proof failed" {
				hasReject = true
			}
		}
	}
	if !hasVerify {
		t.Error("expected audit event for privacy.verify")
	}
	if !hasReject {
		t.Error("expected audit event for privacy.reject")
	}
}
