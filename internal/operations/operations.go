package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type Store interface {
	ClaimOperationJob(context.Context) (domain.OperationJob, bool, error)
	CompleteOperationJob(context.Context, string) error
	FailOperationJob(context.Context, string, error) error
	ExportPrivacyData(context.Context, string) (domain.PrivacyData, error)
	CompletePrivacyExport(context.Context, string, string) error
	DeletePrivacyData(context.Context, string) ([]string, error)
	EnforceRetention(context.Context, string) (domain.RetentionReport, error)
}

func Drain(ctx context.Context, store Store, blobs ports.BlobStore, maxItems int, watch bool) (int, error) {
	processed := 0
	for processed < maxItems {
		job, found, err := store.ClaimOperationJob(ctx)
		if err != nil {
			return processed, err
		}
		if !found {
			if !watch {
				return processed, nil
			}
			select {
			case <-ctx.Done():
				return processed, nil
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		if err := execute(ctx, store, blobs, job.Type, job.Payload); err != nil {
			if failErr := store.FailOperationJob(ctx, job.ID, err); failErr != nil {
				return processed, failErr
			}
			continue
		}
		if err := store.CompleteOperationJob(ctx, job.ID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func execute(ctx context.Context, store Store, blobs ports.BlobStore, jobType string, payload json.RawMessage) error {
	var input struct {
		RequestID string `json:"request_id"`
		TenantID  string `json:"tenant_id"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return errors.New("operation payload must be a JSON object")
	}
	switch jobType {
	case "privacy.export":
		if input.RequestID == "" {
			return errors.New("privacy.export payload requires request_id")
		}
		data, err := store.ExportPrivacyData(ctx, input.RequestID)
		if err != nil {
			return err
		}
		content, err := json.Marshal(data)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("privacy/%s/%s/export.json", data.TenantID, input.RequestID)
		if err := blobs.Put(ctx, key, content, "application/json"); err != nil {
			return err
		}
		return store.CompletePrivacyExport(ctx, input.RequestID, key)
	case "privacy.delete":
		if input.RequestID == "" {
			return errors.New("privacy.delete payload requires request_id")
		}
		objectKeys, err := store.DeletePrivacyData(ctx, input.RequestID)
		if err != nil {
			return err
		}
		for _, key := range objectKeys {
			if err := blobs.Delete(ctx, key); err != nil {
				return err
			}
		}
		return nil
	case "retention.enforce":
		tenantID := input.TenantID
		if tenantID == "" {
			return errors.New("retention.enforce payload requires tenant_id")
		}
		_, err := store.EnforceRetention(ctx, tenantID)
		return err
	default:
		return fmt.Errorf("unsupported operation type %q", jobType)
	}
}
