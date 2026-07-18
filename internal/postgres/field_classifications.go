package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func validateFieldClassification(c domain.FieldClassification) error {
	if c.EntityType != "profile" && c.EntityType != "event" {
		return errors.New("entity_type must be profile or event")
	}
	if c.FieldPath == "" {
		return errors.New("field_path is required")
	}
	if c.Classification != "public" && c.Classification != "internal" && c.Classification != "confidential" && c.Classification != "restricted" {
		return fmt.Errorf("invalid classification: %s", c.Classification)
	}
	if c.SendToModel == "" {
		return errors.New("send_to_model is required")
	}
	if c.SendToModel != "allow" && c.SendToModel != "redact" && c.SendToModel != "tokenize" && c.SendToModel != "deny" {
		return fmt.Errorf("invalid send_to_model policy: %s", c.SendToModel)
	}
	return nil
}

const fieldClassificationColumns = `id, tenant_id, workspace_id, entity_type, field_path, classification, send_to_model, created_at`

func scanFieldClassification(row pgx.Row, c *domain.FieldClassification) error {
	return row.Scan(&c.ID, &c.TenantID, &c.WorkspaceID, &c.EntityType, &c.FieldPath, &c.Classification, &c.SendToModel, &c.CreatedAt)
}

func (s *Store) CreateFieldClassification(ctx context.Context, p domain.Principal, c domain.FieldClassification) (domain.FieldClassification, error) {
	if err := validateFieldClassification(c); err != nil {
		return domain.FieldClassification{}, err
	}
	var out domain.FieldClassification
	err := scanFieldClassification(s.pool.QueryRow(ctx, `INSERT INTO field_classifications
		(tenant_id, workspace_id, entity_type, field_path, classification, send_to_model)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+fieldClassificationColumns,
		p.TenantID, p.WorkspaceID, c.EntityType, c.FieldPath, c.Classification, c.SendToModel), &out)
	if err != nil {
		return domain.FieldClassification{}, err
	}
	_ = s.audit(ctx, p, "field_classification.create", "field_classification", out.ID, map[string]any{"field_path": out.FieldPath})
	return out, nil
}

func (s *Store) GetFieldClassification(ctx context.Context, p domain.Principal, id string) (domain.FieldClassification, error) {
	var out domain.FieldClassification
	err := scanFieldClassification(s.pool.QueryRow(ctx, `SELECT `+fieldClassificationColumns+`
		FROM field_classifications WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FieldClassification{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListFieldClassifications(ctx context.Context, p domain.Principal, entityType string) ([]domain.FieldClassification, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+fieldClassificationColumns+`
		FROM field_classifications WHERE tenant_id=$1 AND workspace_id=$2 AND ($3='' OR entity_type=$3)
		ORDER BY entity_type, field_path`, p.TenantID, p.WorkspaceID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FieldClassification
	for rows.Next() {
		var c domain.FieldClassification
		if err := rows.Scan(&c.ID, &c.TenantID, &c.WorkspaceID, &c.EntityType, &c.FieldPath, &c.Classification, &c.SendToModel, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateFieldClassification(ctx context.Context, p domain.Principal, c domain.FieldClassification) (domain.FieldClassification, error) {
	if err := validateFieldClassification(c); err != nil {
		return domain.FieldClassification{}, err
	}
	var out domain.FieldClassification
	err := scanFieldClassification(s.pool.QueryRow(ctx, `UPDATE field_classifications SET
		entity_type=$1, field_path=$2, classification=$3, send_to_model=$4
		WHERE tenant_id=$5 AND workspace_id=$6 AND id=$7 RETURNING `+fieldClassificationColumns,
		c.EntityType, c.FieldPath, c.Classification, c.SendToModel, p.TenantID, p.WorkspaceID, c.ID), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FieldClassification{}, ErrNotFound
	}
	if err != nil {
		return domain.FieldClassification{}, err
	}
	_ = s.audit(ctx, p, "field_classification.update", "field_classification", out.ID, map[string]any{"field_path": out.FieldPath})
	return out, nil
}

func (s *Store) DeleteFieldClassification(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM field_classifications WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	_ = s.audit(ctx, p, "field_classification.delete", "field_classification", id, nil)
	return nil
}
