package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestOptimizationProposalUsesReportGateAndDoesNotReassign_12_8_2(t *testing.T) {
	databaseURL := os.Getenv("OPENJOURNEY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("OPENJOURNEY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("optimization-%d", time.Now().UnixNano())
	if err := store.EnsureDevelopmentTenant(ctx, key); err != nil {
		t.Fatal(err)
	}
	p, err := store.Authenticate(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	var templateID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO sending_identities
		(tenant_id,workspace_id,channel,from_address,provider)
		VALUES ($1,$2,'email',$3,'fake') RETURNING id`, p.TenantID, p.WorkspaceID,
		fmt.Sprintf("optimization-%d@example.com", time.Now().UnixNano())).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO templates
		(tenant_id,workspace_id,name,channel,html_template,sending_identity_id)
		VALUES ($1,$2,'Optimization template','email','hello',$3) RETURNING id`,
		p.TenantID, p.WorkspaceID, templateID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	journeyID, versionID := seedOptimizationJourney(t, ctx, store, p)
	proposalExperiment := createOptimizationExperiment(t, ctx, store, p, "proposal", `[{"name":"complaint"}]`)
	seedOptimizationFacts(t, ctx, store, p, proposalExperiment.ID, journeyID, versionID, templateID, false)
	proposal, err := store.ProposeExperimentOptimization(ctx, p, proposalExperiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "proposed" || proposal.Kind != "winner" || proposal.WinnerVariant == nil || *proposal.WinnerVariant != "treatment" {
		t.Fatalf("proposal = %+v, want proposed treatment winner", proposal)
	}
	if len(proposal.ReportSnapshot) == 0 {
		t.Fatal("proposal did not snapshot the report")
	}
	var assignments int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM experiment_assignments WHERE experiment_id=$1`, proposalExperiment.ID).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if assignments != 0 {
		t.Fatalf("proposal created %d assignments", assignments)
	}
	repeated, err := store.ProposeExperimentOptimization(ctx, p, proposalExperiment.ID)
	if err != nil || repeated.ID != proposal.ID {
		t.Fatalf("repeat proposal = %+v, err=%v; want same pending proposal", repeated, err)
	}

	blockedExperiment := createOptimizationExperiment(t, ctx, store, p, "blocked", `[{"name":"complaint"}]`)
	seedOptimizationFacts(t, ctx, store, p, blockedExperiment.ID, journeyID, versionID, templateID, true)
	if _, err := store.ProposeExperimentOptimization(ctx, p, blockedExperiment.ID); err != ErrOptimizationUnavailable {
		t.Fatalf("guardrail regression proposal error = %v, want ErrOptimizationUnavailable", err)
	}
}

func createOptimizationExperiment(t *testing.T, ctx context.Context, store *Store, p domain.Principal, name, guardrails string) domain.Experiment {
	t.Helper()
	experiment, err := store.CreateExperiment(ctx, p, domain.Experiment{
		Name: name, SubjectType: "journey", Status: "running", Method: "frequentist",
		Seed: name + "-immutable-seed", HoldoutPct: 10,
		PrimaryGoal: []byte(`{"name":"purchase"}`), GuardrailGoals: []byte(guardrails),
		Variants: []domain.ExperimentVariant{{Label: "control", Weight: 50, IsControl: true}, {Label: "treatment", Weight: 50}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return experiment
}

func seedOptimizationJourney(t *testing.T, ctx context.Context, store *Store, p domain.Principal) (string, string) {
	t.Helper()
	var journeyID, versionID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO journeys
		(tenant_id,workspace_id,name,status,graph) VALUES ($1,$2,$3,'published','{}') RETURNING id`,
		p.TenantID, p.WorkspaceID, fmt.Sprintf("Optimization journey %d", time.Now().UnixNano())).Scan(&journeyID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `INSERT INTO journey_versions
		(journey_id,tenant_id,workspace_id,version,graph,entry_kind,status)
		VALUES ($1,$2,$3,1,'{}','event','active') RETURNING id`, journeyID, p.TenantID, p.WorkspaceID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	return journeyID, versionID
}

func seedOptimizationFacts(t *testing.T, ctx context.Context, store *Store, p domain.Principal, experimentID, journeyID, versionID, templateID string, guardrailRegression bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		variant := "control"
		if i >= 50 {
			variant = "treatment"
		}
		var profileID, runID string
		if err := store.pool.QueryRow(ctx, `INSERT INTO profiles
			(tenant_id,workspace_id,app_id,external_id) VALUES ($1,$2,$3,$4) RETURNING id`,
			p.TenantID, p.WorkspaceID, p.AppID, fmt.Sprintf("optimization-profile-%s-%d", experimentID, i)).Scan(&profileID); err != nil {
			t.Fatal(err)
		}
		if err := store.pool.QueryRow(ctx, `INSERT INTO journey_runs
			(tenant_id,workspace_id,journey_id,journey_version_id,profile_id,entry_key,current_node_id)
			VALUES ($1,$2,$3,$4,$5,$6,'send') RETURNING id`, p.TenantID, p.WorkspaceID, journeyID, versionID,
			profileID, fmt.Sprintf("optimization-entry-%s-%d", experimentID, i)).Scan(&runID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO journey_message_intents
			(run_id,tenant_id,workspace_id,journey_id,journey_version_id,node_id,profile_id,template_id,channel,endpoint,status,decision,experiment_id,variant)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'email','x@example.com','completed','sent',$9,$10)`,
			runID, p.TenantID, p.WorkspaceID, journeyID, versionID, "send", profileID, templateID, experimentID, variant); err != nil {
			t.Fatal(err)
		}
		purchase := (variant == "control" && i < 5) || (variant == "treatment" && i < 65)
		if purchase {
			if _, err := store.pool.Exec(ctx, `INSERT INTO conversion_facts
				(tenant_id,workspace_id,source_type,source_id,experiment_id,variant,profile_id,goal_name,occurred_at,attributed_send_at,source_event_id)
				VALUES ($1,$2,'journey',$3,$4,$5,$6,'purchase',now(),now(),gen_random_uuid())`,
				p.TenantID, p.WorkspaceID, journeyID, experimentID, variant, profileID); err != nil {
				t.Fatal(err)
			}
		}
		if guardrailRegression && variant == "treatment" && i < 75 {
			if _, err := store.pool.Exec(ctx, `INSERT INTO conversion_facts
				(tenant_id,workspace_id,source_type,source_id,experiment_id,variant,profile_id,goal_name,occurred_at,attributed_send_at,source_event_id)
				VALUES ($1,$2,'journey',$3,$4,$5,$6,'complaint',now(),now(),gen_random_uuid())`,
				p.TenantID, p.WorkspaceID, journeyID, experimentID, variant, profileID); err != nil {
				t.Fatal(err)
			}
		}
	}
}
