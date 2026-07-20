// Package connector contains the in-process connector driver ports.
package connector

import (
	"context"
	"errors"
	"sync"
)

type Row map[string]any

// ConnectorDriver is the backend-neutral, bounded I/O port for sources and sinks.
type ConnectorDriver interface {
	Read(ctx context.Context, cfg map[string]any, cursor string) ([]Row, string, error)
	Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error)
}

type Registry struct {
	mu       sync.RWMutex
	drivers  map[string]ConnectorDriver
	fallback ConnectorDriver
}

func NewRegistry(drivers map[string]ConnectorDriver, fallback ConnectorDriver) *Registry {
	if drivers == nil {
		drivers = map[string]ConnectorDriver{}
	}
	return &Registry{drivers: drivers, fallback: fallback}
}

// Native drivers are registered here once so every process uses the same
// governed connector port. S3 constructs its MinIO client from *_ref config
// at read time, keeping credentials out of connector definitions.
func DefaultRegistry() *Registry {
	fake := NewFakeDriver()
	stub := &unimplementedDriver{}
	return NewRegistry(map[string]ConnectorDriver{
		"fake": fake, "s3": NewS3Driver(), "clickhouse": stub, "kafka": stub, "webhook": stub,
	}, fake)
}

func (r *Registry) For(name string) ConnectorDriver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if driver, ok := r.drivers[name]; ok {
		return driver
	}
	return r.fallback
}

func (r *Registry) Register(name string, driver ConnectorDriver) error {
	if name == "" || driver == nil {
		return errors.New("connector driver name and implementation are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drivers[name] = driver
	return nil
}

func RegisterNativeConnectors(reg *Registry, drivers map[string]ConnectorDriver) error {
	if reg == nil {
		return errors.New("connector registry is required")
	}
	for name, driver := range drivers {
		if err := reg.Register(name, driver); err != nil {
			return err
		}
	}
	return nil
}

// RegisterRemoteBridge keeps remote connectors on the same registry while the
// implementation remains responsible for using the M9 extension host.
func RegisterRemoteBridge(reg *Registry, name string, driver ConnectorDriver) error {
	return reg.Register(name, driver)
}

type FakeDriver struct {
	Rows     []Row
	Writes   []Row
	ReadErr  error
	WriteErr error
}

type unimplementedDriver struct{}

func (d *unimplementedDriver) Read(ctx context.Context, cfg map[string]any, cursor string) ([]Row, string, error) {
	return nil, cursor, errors.New("connector driver is not installed")
}

func (d *unimplementedDriver) Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error) {
	return 0, errors.New("connector driver is not installed")
}

func NewFakeDriver() *FakeDriver { return &FakeDriver{} }
func (f *FakeDriver) Read(ctx context.Context, cfg map[string]any, cursor string) ([]Row, string, error) {
	if f.ReadErr != nil {
		return nil, cursor, f.ReadErr
	}
	rows := make([]Row, len(f.Rows))
	copy(rows, f.Rows)
	return rows, cursor, nil
}
func (f *FakeDriver) Write(ctx context.Context, cfg map[string]any, rows []Row) (int, error) {
	if f.WriteErr != nil {
		return 0, f.WriteErr
	}
	f.Writes = append(f.Writes, rows...)
	return len(rows), nil
}
