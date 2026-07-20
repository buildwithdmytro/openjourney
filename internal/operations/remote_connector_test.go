package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type remoteJobInvoker struct {
	activities []string
	err        error
}

func (i *remoteJobInvoker) InvokeWithScope(_ context.Context, _ domain.Principal, extensionID, invocation, scope string, input json.RawMessage) (json.RawMessage, string, error) {
	i.activities = append(i.activities, extensionID+":"+invocation+":"+scope+":"+string(input))
	if i.err != nil {
		return nil, "activity-denied", i.err
	}
	if invocation == "read" {
		return json.RawMessage(`{"rows":[{"email":"remote@example.com"}],"next_cursor":"remote:1"}`), "activity-read", nil
	}
	return json.RawMessage(`{"written":1}`), "activity-write", nil
}

func TestRemoteConnectorSourceAndSinkUseHostBridge(t *testing.T) {
	invoker := &remoteJobInvoker{}
	sourceConfig, _ := json.Marshal(map[string]any{"transport": "remote_http"})
	sourceMapping, _ := json.Marshal(map[string]any{"event_type": "profile.updated", "external_id": "email"})
	store := &sourceStore{
		pipeline: domain.ConnectorPipeline{ID: "pipeline", TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", ConnectorExtensionID: "remote-ext", CurrentVersionID: stringPtr("version")},
		version:  domain.ConnectorPipelineVersion{ID: "version", PipelineID: "pipeline", Version: 1, Mapping: sourceMapping},
		config:   domain.ExtensionConfig{Config: sourceConfig},
	}
	if err := executeWarehouseSyncWithInvoker(context.Background(), store, &sourceBlobStore{values: map[string][]byte{}}, warehouseSyncInput{TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", PipelineID: "pipeline"}, invoker); err != nil {
		t.Fatal(err)
	}

	sinkConfig, _ := json.Marshal(map[string]any{"driver": "remote_http"})
	store.config.Config = sinkConfig
	sinkMapping, _ := json.Marshal(map[string]any{"audience_dsl": map[string]any{"type": "all"}, "fields": []string{"external_id"}})
	store.version.Mapping = sinkMapping
	if err := executeReverseETLWithInvoker(context.Background(), store, nil, reverseETLInput{TenantID: "tenant", WorkspaceID: "workspace", AppID: "app", PipelineID: "pipeline"}, invoker); err != nil {
		t.Fatal(err)
	}
	if len(invoker.activities) != 2 || invoker.activities[0] != `remote-ext:read:connectors:read:{"cursor":""}` || invoker.activities[1] != `remote-ext:write:connectors:write:{"rows":[{"external_id":"profile-1","plan":"pro"}]}` {
		t.Fatalf("remote host calls=%#v", invoker.activities)
	}
}

func TestRemoteConnectorKillSwitchIsReturnedByBridge(t *testing.T) {
	invoker := &remoteJobInvoker{err: errors.New("extension is disabled")}
	config, _ := json.Marshal(map[string]any{"transport": "remote_http"})
	mapping, _ := json.Marshal(map[string]any{"external_id": "email"})
	store := &sourceStore{
		pipeline: domain.ConnectorPipeline{ID: "pipeline", ConnectorExtensionID: "remote-ext", CurrentVersionID: stringPtr("version")},
		version:  domain.ConnectorPipelineVersion{ID: "version", Version: 1, Mapping: mapping},
		config:   domain.ExtensionConfig{Config: config},
	}
	if err := executeWarehouseSyncWithInvoker(context.Background(), store, nil, warehouseSyncInput{PipelineID: "pipeline"}, invoker); err == nil || err.Error() != "extension is disabled" {
		t.Fatalf("expected kill-switch error, got %v", err)
	}
}
