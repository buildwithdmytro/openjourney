package connector

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// ExtensionInvoker is the M9 host seam. Remote connectors deliberately use
// this port rather than making HTTP calls themselves, so signing, scopes,
// allowlists, budgets, circuit breaking, and extension_activity auditing stay
// in one governance path.
type ExtensionInvoker interface {
	InvokeWithScope(context.Context, domain.Principal, string, string, string, json.RawMessage) (json.RawMessage, string, error)
}

// RemoteDriver adapts the connector driver port to a signed remote extension.
// The host owns the timeout and transport; this adapter only translates rows.
type RemoteDriver struct {
	host        ExtensionInvoker
	principal   domain.Principal
	extensionID string
}

func NewRemoteDriver(host ExtensionInvoker, principal domain.Principal, extensionID string) *RemoteDriver {
	return &RemoteDriver{host: host, principal: principal, extensionID: extensionID}
}

func (d *RemoteDriver) Read(ctx context.Context, cfg map[string]any, cursor string) ([]Row, string, error) {
	if d.host == nil || d.extensionID == "" {
		return nil, cursor, errors.New("remote connector host and extension are required")
	}
	input, err := json.Marshal(map[string]any{"cursor": cursor})
	if err != nil {
		return nil, cursor, err
	}
	output, _, err := d.host.InvokeWithScope(ctx, d.principal, d.extensionID, "read", "connectors:read", input)
	if err != nil {
		return nil, cursor, err
	}
	var response struct {
		Rows       []Row  `json:"rows"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, cursor, errors.New("remote connector read returned invalid rows")
	}
	return response.Rows, response.NextCursor, nil
}

func (d *RemoteDriver) Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error) {
	if d.host == nil || d.extensionID == "" {
		return 0, errors.New("remote connector host and extension are required")
	}
	input, err := json.Marshal(map[string]any{"rows": rows})
	if err != nil {
		return 0, err
	}
	output, _, err := d.host.InvokeWithScope(ctx, d.principal, d.extensionID, "write", "connectors:write", input)
	if err != nil {
		return 0, err
	}
	var response struct {
		Written int `json:"written"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return 0, errors.New("remote connector write returned invalid count")
	}
	return response.Written, nil
}
