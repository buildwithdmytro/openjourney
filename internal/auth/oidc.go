package auth

import (
	"context"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/coreos/go-oidc/v3/oidc"
)

type OIDCVerifier struct {
	issuer   string
	verifier *oidc.IDTokenVerifier
}

func NewOIDCVerifier(ctx context.Context, issuer, clientID string) (*OIDCVerifier, error) {
	if issuer == "" || clientID == "" {
		return nil, errors.New("OIDC issuer and client ID are required")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCVerifier{issuer: issuer, verifier: provider.Verifier(&oidc.Config{ClientID: clientID})}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (domain.OIDCClaims, error) {
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return domain.OIDCClaims{}, err
	}
	var claims struct {
		Email       string `json:"email"`
		Name        string `json:"name"`
		TenantID    string `json:"tenant_id"`
		WorkspaceID string `json:"workspace_id"`
		AppID       string `json:"app_id"`
	}
	if err := token.Claims(&claims); err != nil {
		return domain.OIDCClaims{}, err
	}
	if claims.TenantID == "" || claims.WorkspaceID == "" || claims.AppID == "" {
		return domain.OIDCClaims{}, errors.New("OIDC token requires tenant_id, workspace_id, and app_id claims")
	}
	return domain.OIDCClaims{
		Issuer: v.issuer, Subject: token.Subject, Email: claims.Email, Name: claims.Name,
		TenantID: claims.TenantID, WorkspaceID: claims.WorkspaceID, AppID: claims.AppID,
	}, nil
}
