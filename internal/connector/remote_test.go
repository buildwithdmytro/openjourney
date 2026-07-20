package connector_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type remoteInvoker struct {
	invocations []string
	err         error
}

func (i *remoteInvoker) InvokeWithScope(_ context.Context, _ domain.Principal, extensionID, invocation, scope string, input json.RawMessage) (json.RawMessage, string, error) {
	i.invocations = append(i.invocations, extensionID+":"+invocation+":"+scope+":"+string(input))
	if i.err != nil {
		return nil, "activity-1", i.err
	}
	if invocation == "read" {
		return json.RawMessage(`{"rows":[{"external_id":"p1"}],"next_cursor":"object:1"}`), "activity-1", nil
	}
	return json.RawMessage(`{"written":1}`), "activity-2", nil
}

func TestRemoteDriverUsesHostForReadAndWrite(t *testing.T) {
	host := &remoteInvoker{}
	driver := connector.NewRemoteDriver(host, domain.Principal{TenantID: "tenant", WorkspaceID: "workspace"}, "ext-1")
	rows, cursor, err := driver.Read(context.Background(), nil, "object:0")
	if err != nil || cursor != "object:1" || len(rows) != 1 {
		t.Fatalf("read rows=%v cursor=%q err=%v", rows, cursor, err)
	}
	written, err := driver.Write(context.Background(), nil, rows)
	if err != nil || written != 1 {
		t.Fatalf("written=%d err=%v", written, err)
	}
	if len(host.invocations) != 2 || host.invocations[0] != `ext-1:read:connectors:read:{"cursor":"object:0"}` || host.invocations[1] != `ext-1:write:connectors:write:{"rows":[{"external_id":"p1"}]}` {
		t.Fatalf("unexpected host invocations: %#v", host.invocations)
	}
}

func TestRemoteDriverPropagatesHostKillSwitch(t *testing.T) {
	host := &remoteInvoker{err: errors.New("extension is disabled")}
	driver := connector.NewRemoteDriver(host, domain.Principal{}, "ext-1")
	if _, _, err := driver.Read(context.Background(), nil, ""); err == nil || err.Error() != "extension is disabled" {
		t.Fatalf("expected host kill-switch error, got %v", err)
	}
}
