package campaigns

import (
	"context"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type memoryBlobStore struct {
	objects map[string][]byte
}

func (m *memoryBlobStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	m.objects[key] = data
	return nil
}

func (m *memoryBlobStore) Get(ctx context.Context, key string) ([]byte, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func TestDispatchNext_SMSCampaignResolvesPhonesOnly(t *testing.T) {
	store := newMockStore()
	blobs := &memoryBlobStore{objects: make(map[string][]byte)}

	campID := "camp-sms"
	tmplID := "tmpl-sms"
	segID := "seg-sms"

	// 1. Setup template (channel = sms)
	store.templates[tmplID] = domain.Template{
		ID:      tmplID,
		Channel: "sms",
		Version: 1,
	}

	// 2. Setup segment
	store.segments[segID] = domain.Segment{
		ID:      segID,
		Version: 1,
		DSL:     []byte(`{}`),
	}

	// 3. Setup profiles
	// profile 1: has phone only
	// profile 2: has email only
	// profile 3: has both phone and email
	// profile 4: has neither
	store.resolvedSegment[segID] = []string{"prof-1", "prof-2", "prof-3", "prof-4"}
	store.profilePhones["prof-1"] = "+15555550001"
	store.profileEmails["prof-2"] = "prof2@example.com"
	store.profilePhones["prof-3"] = "+15555550003"
	store.profileEmails["prof-3"] = "prof3@example.com"

	// 4. Setup campaign
	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		SegmentID:   segID,
		TemplateID:  tmplID,
		Status:      "scheduled",
	}

	// 5. Run DispatchNext
	dispatched, err := DispatchNext(context.Background(), store, blobs)
	if err != nil {
		t.Fatalf("DispatchNext failed: %v", err)
	}
	if !dispatched {
		t.Fatalf("expected dispatched=true")
	}

	// 6. Verify jobs and recipients
	jobs := store.manifestJobs[campID]
	if len(jobs) != 1 {
		t.Fatalf("expected 1 delivery job, got %d", len(jobs))
	}

	job := jobs[0]
	if len(job.Recipients) != 2 {
		t.Fatalf("expected 2 recipients in job, got %d: %v", len(job.Recipients), job.Recipients)
	}

	expectedRecipients := map[string]string{
		"prof-1": "+15555550001",
		"prof-3": "+15555550003",
	}

	for _, r := range job.Recipients {
		expEndpoint, ok := expectedRecipients[r.ProfileID]
		if !ok {
			t.Errorf("unexpected recipient: %s", r.ProfileID)
			continue
		}
		if r.Endpoint != expEndpoint {
			t.Errorf("expected endpoint %s for profile %s, got %s", expEndpoint, r.ProfileID, r.Endpoint)
		}
	}
}

func TestDispatchNext_EmailCampaignResolvesEmailsOnly(t *testing.T) {
	store := newMockStore()
	blobs := &memoryBlobStore{objects: make(map[string][]byte)}

	campID := "camp-email"
	tmplID := "tmpl-email"
	segID := "seg-email"

	// 1. Setup template (channel = email)
	store.templates[tmplID] = domain.Template{
		ID:      tmplID,
		Channel: "email",
		Version: 1,
	}

	// 2. Setup segment
	store.segments[segID] = domain.Segment{
		ID:      segID,
		Version: 1,
		DSL:     []byte(`{}`),
	}

	// 3. Setup profiles
	store.resolvedSegment[segID] = []string{"prof-1", "prof-2", "prof-3", "prof-4"}
	store.profilePhones["prof-1"] = "+15555550001"
	store.profileEmails["prof-2"] = "prof2@example.com"
	store.profilePhones["prof-3"] = "+15555550003"
	store.profileEmails["prof-3"] = "prof3@example.com"

	// 4. Setup campaign
	store.campaigns[campID] = domain.Campaign{
		ID:          campID,
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		SegmentID:   segID,
		TemplateID:  tmplID,
		Status:      "scheduled",
	}

	// 5. Run DispatchNext
	dispatched, err := DispatchNext(context.Background(), store, blobs)
	if err != nil {
		t.Fatalf("DispatchNext failed: %v", err)
	}
	if !dispatched {
		t.Fatalf("expected dispatched=true")
	}

	// 6. Verify jobs and recipients
	jobs := store.manifestJobs[campID]
	if len(jobs) != 1 {
		t.Fatalf("expected 1 delivery job, got %d", len(jobs))
	}

	job := jobs[0]
	if len(job.Recipients) != 2 {
		t.Fatalf("expected 2 recipients in job, got %d: %v", len(job.Recipients), job.Recipients)
	}

	expectedRecipients := map[string]string{
		"prof-2": "prof2@example.com",
		"prof-3": "prof3@example.com",
	}

	for _, r := range job.Recipients {
		expEndpoint, ok := expectedRecipients[r.ProfileID]
		if !ok {
			t.Errorf("unexpected recipient: %s", r.ProfileID)
			continue
		}
		if r.Endpoint != expEndpoint {
			t.Errorf("expected endpoint %s for profile %s, got %s", expEndpoint, r.ProfileID, r.Endpoint)
		}
	}
}
