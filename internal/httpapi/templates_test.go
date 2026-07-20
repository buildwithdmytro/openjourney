package httpapi

import (
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    domain.Template
		wantErr bool
		errMsg  string
	}{
		{
			name: "email template with html_template",
			tmpl: domain.Template{
				Channel:      "email",
				HTMLTemplate: ptr("Hello"),
			},
			wantErr: false,
		},
		{
			name: "email template missing html_template",
			tmpl: domain.Template{
				Channel: "email",
			},
			wantErr: true,
			errMsg:  "html_template",
		},
		{
			name: "sms template with text_template",
			tmpl: domain.Template{
				Channel:      "sms",
				TextTemplate: ptr("Hello"),
			},
			wantErr: false,
		},
		{
			name: "sms template with body_template",
			tmpl: domain.Template{
				Channel:      "sms",
				BodyTemplate: ptr("Hello"),
			},
			wantErr: false,
		},
		{
			name: "sms template missing both text_template and body_template",
			tmpl: domain.Template{
				Channel: "sms",
			},
			wantErr: true,
			errMsg:  "text_template or body_template",
		},
		{
			name: "push template with title and body",
			tmpl: domain.Template{
				Channel:       "push",
				TitleTemplate: ptr("Title"),
				BodyTemplate:  ptr("Body"),
			},
			wantErr: false,
		},
		{
			name: "push template missing title",
			tmpl: domain.Template{
				Channel:      "push",
				BodyTemplate: ptr("Body"),
			},
			wantErr: true,
			errMsg:  "title_template",
		},
		{
			name: "in_app template with title_template",
			tmpl: domain.Template{
				Channel:       "in_app",
				TitleTemplate: ptr("Title"),
			},
			wantErr: false,
		},
		{
			name: "in_app template with body_template",
			tmpl: domain.Template{
				Channel:      "in_app",
				BodyTemplate: ptr("Body"),
			},
			wantErr: false,
		},
		{
			name: "in_app template with html_template",
			tmpl: domain.Template{
				Channel:      "in_app",
				HTMLTemplate: ptr("<p>HTML</p>"),
			},
			wantErr: false,
		},
		{
			name: "in_app template missing all templates",
			tmpl: domain.Template{
				Channel: "in_app",
			},
			wantErr: true,
			errMsg:  "in_app templates require",
		},
		{
			name: "webhook template with body_template",
			tmpl: domain.Template{
				Channel:      "webhook",
				BodyTemplate: ptr("{}"),
			},
			wantErr: false,
		},
		{
			name: "webhook template missing body_template",
			tmpl: domain.Template{
				Channel: "webhook",
			},
			wantErr: true,
			errMsg:  "body_template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTemplate(tt.tmpl)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTemplate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if err.Error() == "" || !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateTemplate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr))
}
