package postgres

import (
	"testing"
	"time"
)

func TestAuditHashChainDetectsTamperedRow_NonGated(t *testing.T) {
	events := []auditRow{
		{id: "event-1", tenantID: "tenant-1", workspaceID: "workspace-1", actType: "user", actID: "user-1", act: "user.create", resType: "user", resID: "user-1", metadata: []byte(`{"role":"member"}`), occurredAt: time.Unix(100, 0).UTC(), seq: 1},
		{id: "event-2", tenantID: "tenant-1", workspaceID: "workspace-1", actType: "user", actID: "user-1", act: "user.update", resType: "user", resID: "user-1", metadata: []byte(`{"role":"admin"}`), occurredAt: time.Unix(101, 0).UTC(), seq: 2},
	}
	for i := range events {
		if i > 0 {
			events[i].prevHash = events[i-1].rowHash
		}
		events[i].rowHash = ComputeAuditRowHash(events[i].prevHash, events[i].id, events[i].tenantID, events[i].workspaceID, events[i].appID, events[i].actType, events[i].actID, events[i].act, events[i].resType, events[i].resID, events[i].metadata, events[i].occurredAt, events[i].seq)
	}

	intact := verifyAuditRows(events)
	if !intact.Intact || intact.Status != "ok" {
		t.Fatalf("expected intact chain, got %+v", intact)
	}
	events[1].act = "user.delete"
	tampered := verifyAuditRows(events)
	if tampered.Intact || tampered.Status != "tampered" || tampered.FirstBrokenSeq == nil || *tampered.FirstBrokenSeq != 2 {
		t.Fatalf("expected tampered event 2 to be detected, got %+v", tampered)
	}
}
