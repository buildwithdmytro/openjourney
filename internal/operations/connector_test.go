package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/extension"
)

type connectorInvoker struct {
	principal   domain.Principal
	extensionID string
	invocation  string
	input       json.RawMessage
	err         error
}

type connectorJobStore struct {
	Store
	job       domain.OperationJob
	claimed   bool
	completed string
	failed    string
	failErr   error
}

func (s *connectorJobStore) ClaimOperationJob(context.Context) (domain.OperationJob, bool, error) {
	if s.claimed {
		return domain.OperationJob{}, false, nil
	}
	s.claimed = true
	return s.job, true, nil
}
func (s *connectorJobStore) CompleteOperationJob(_ context.Context, id string) error {
	s.completed = id
	return nil
}
func (s *connectorJobStore) FailOperationJob(_ context.Context, id string, err error) error {
	s.failed, s.failErr = id, err
	return nil
}

func TestConnectorJobIsLeasedAndCompletedAfterBoundedInvocation(t *testing.T) {
	store := &connectorJobStore{job: domain.OperationJob{
		ID: "job-1", Type: "connector.run", Payload: json.RawMessage(`{
			"tenant_id":"tenant-1","workspace_id":"workspace-1","extension_id":"connector-1",
			"event_id":"event-1","event_type":"profile.updated",
			"event":{"event_id":"event-1","event_type":"profile.updated","payload":{}}
		}`),
	}}
	invoker := &connectorInvoker{}
	processed, err := DrainWithGatewayAndExtensions(context.Background(), store, nil, nil, invoker, 1, false)
	if err != nil || processed != 1 || store.completed != "job-1" {
		t.Fatalf("processed=%d completed=%q err=%v", processed, store.completed, err)
	}
	if invoker.invocation != "deliver" {
		t.Fatalf("expected connector delivery invocation, got %q", invoker.invocation)
	}
}

func (i *connectorInvoker) Invoke(_ context.Context, p domain.Principal, extensionID, invocation string, input json.RawMessage) (json.RawMessage, string, error) {
	i.principal, i.extensionID, i.invocation, i.input = p, extensionID, invocation, input
	return json.RawMessage(`{"accepted":true}`), "activity-1", nil
}

func (i *connectorInvoker) InvokeWithScope(ctx context.Context, p domain.Principal, extensionID, invocation, _ string, input json.RawMessage) (json.RawMessage, string, error) {
	if i.err != nil {
		return nil, "", i.err
	}
	return i.Invoke(ctx, p, extensionID, invocation, input)
}

func TestConnectorRunUsesStablePerEventIdempotencyInput(t *testing.T) {
	invoker := &connectorInvoker{}
	payload := json.RawMessage(`{
		"tenant_id":"tenant-1",
		"workspace_id":"workspace-1",
		"extension_id":"connector-1",
		"event_id":"event-1",
		"event_type":"events.accepted.v1",
		"event":{"event_id":"event-1","event_type":"profile.updated","payload":{"plan":"pro"}}
	}`)

	if err := execute(context.Background(), nil, nil, nil, invoker, "connector.run", payload); err != nil {
		t.Fatal(err)
	}
	if invoker.principal.TenantID != "tenant-1" || invoker.principal.WorkspaceID != "workspace-1" || invoker.principal.ActorType != "system" {
		t.Fatalf("unexpected principal: %+v", invoker.principal)
	}
	if invoker.extensionID != "connector-1" || invoker.invocation != "deliver" {
		t.Fatalf("unexpected invocation: %s/%s", invoker.extensionID, invoker.invocation)
	}
	var input struct {
		EventID        string `json:"event_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.Unmarshal(invoker.input, &input); err != nil {
		t.Fatal(err)
	}
	if input.EventID != "event-1" || input.IdempotencyKey != "connector:connector-1:event-1" {
		t.Fatalf("connector input lost idempotency key: %+v", input)
	}
}

func TestConnectorRunRequiresHost(t *testing.T) {
	err := execute(context.Background(), nil, nil, nil, nil, "connector.run", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected connector job without host to fail")
	}
}

func TestDisabledConnectorFailureIsTerminalAndNotRequeued(t *testing.T) {
	store := &connectorJobStore{job: domain.OperationJob{ID: "job-disabled", Type: "connector.run", Payload: json.RawMessage(`{
		"tenant_id":"tenant-1","workspace_id":"workspace-1","extension_id":"connector-1",
		"event_id":"event-1","event_type":"events.accepted.v1","event":{}
	}`)}}
	invoker := &connectorInvoker{err: extension.ErrExtensionDisabled}
	processed, err := DrainWithGatewayAndExtensions(context.Background(), store, nil, nil, invoker, 1, false)
	if err != nil || processed != 0 || store.failed != "job-disabled" || store.completed != "" {
		t.Fatalf("processed=%d err=%v failed=%q completed=%q", processed, err, store.failed, store.completed)
	}
	var terminal domain.TerminalOperationError
	if !errors.As(store.failErr, &terminal) || !terminal.TerminalOperation() {
		t.Fatalf("disabled connector error was not terminal: %v", store.failErr)
	}
}
