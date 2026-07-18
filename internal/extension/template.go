package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/osteele/liquid"
	liquidrender "github.com/osteele/liquid/render"
)

// TemplateRegistration describes one manifest-declared Liquid entry point.
// A filter receives {"value": ..., "args": [...]} and a tag receives
// {"args": "...", "bindings": {...}}. The Wasm result is returned as the
// Liquid value (or rendered as a tag string).
type TemplateRegistration struct {
	Name        string
	ExtensionID string
	Invocation  string
	Tag         bool
}

type templateManifest struct {
	Filter     string `json:"filter"`
	FilterName string `json:"filter_name"`
	Tag        string `json:"tag"`
	TagName    string `json:"tag_name"`
	Export     string `json:"export"`
	WasmExport string `json:"wasm_export"`
}

// RegisterTemplateFunction installs one bounded, audited extension function.
// The host supplies the hard timeout, Wasm sandbox, scope intersection, and
// append-only activity record; Liquid only sees the resulting value.
func RegisterTemplateFunction(engine *liquid.Engine, host *Host, principal domain.Principal, registration TemplateRegistration) error {
	if engine == nil || host == nil {
		return fmt.Errorf("template engine and extension host are required")
	}
	if strings.TrimSpace(registration.Name) == "" || strings.TrimSpace(registration.ExtensionID) == "" {
		return fmt.Errorf("template function name and extension id are required")
	}
	if registration.Invocation == "" {
		registration.Invocation = "run"
	}
	if registration.Tag {
		engine.RegisterTag(registration.Name, func(ctx liquidrender.Context) (string, error) {
			input, err := json.Marshal(map[string]any{
				"args": ctx.TagArgs(), "bindings": ctx.Bindings(),
			})
			if err != nil {
				return "", err
			}
			output, _, err := host.Invoke(context.Background(), principal, registration.ExtensionID, registration.Invocation, input)
			if err != nil {
				return "", fmt.Errorf("template extension %s: %w", registration.Name, err)
			}
			return templateString(output)
		})
		return nil
	}

	engine.RegisterFilter(registration.Name, func(value interface{}, args ...interface{}) (interface{}, error) {
		input, err := json.Marshal(map[string]any{"value": value, "args": args})
		if err != nil {
			return nil, err
		}
		output, _, err := host.Invoke(context.Background(), principal, registration.ExtensionID, registration.Invocation, input)
		if err != nil {
			return nil, fmt.Errorf("template extension %s: %w", registration.Name, err)
		}
		var result interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			return nil, fmt.Errorf("template extension %s returned invalid JSON: %w", registration.Name, err)
		}
		return result, nil
	})
	return nil
}

func templateString(output json.RawMessage) (string, error) {
	var value interface{}
	if err := json.Unmarshal(output, &value); err != nil {
		return "", fmt.Errorf("template extension returned invalid JSON: %w", err)
	}
	if s, ok := value.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

// RegisterTemplateFunctions discovers enabled template_function extensions
// and installs their manifest-declared filter/tag entry points.
func RegisterTemplateFunctions(ctx context.Context, engine *liquid.Engine, store ports.Store, host *Host, principal domain.Principal) error {
	extensions, err := store.ListExtensions(ctx, principal)
	if err != nil {
		return fmt.Errorf("list template extensions: %w", err)
	}
	for _, ext := range extensions {
		if ext.Status != "enabled" || ext.CurrentVersionID == nil || *ext.CurrentVersionID == "" {
			continue
		}
		version, err := store.GetExtensionVersion(ctx, principal, *ext.CurrentVersionID)
		if err != nil {
			return fmt.Errorf("get template extension %s: %w", ext.ID, err)
		}
		if version.Status != "active" || version.Kind != "template_function" || version.Transport != "wasm" {
			continue
		}
		var manifest templateManifest
		if err := json.Unmarshal(version.Manifest, &manifest); err != nil {
			return fmt.Errorf("decode template extension %s manifest: %w", ext.ID, err)
		}
		name := manifest.Filter
		tag := false
		if name == "" {
			name = manifest.FilterName
		}
		if name == "" {
			name = manifest.Tag
			tag = name != ""
		}
		if name == "" {
			name = manifest.TagName
			tag = name != ""
		}
		if name == "" {
			return fmt.Errorf("template extension %s manifest has no filter or tag name", ext.ID)
		}
		invocation := manifest.Export
		if invocation == "" {
			invocation = manifest.WasmExport
		}
		if err := RegisterTemplateFunction(engine, host, principal, TemplateRegistration{
			Name: name, ExtensionID: ext.ID, Invocation: invocation, Tag: tag,
		}); err != nil {
			return err
		}
	}
	return nil
}
