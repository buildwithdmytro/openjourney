CREATE TABLE IF NOT EXISTS sending_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    channel text NOT NULL CHECK (channel IN ('email','webhook')),
    from_address text,
    from_name text,
    reply_to text,
    provider text NOT NULL DEFAULT 'ses' CHECK (provider IN ('ses','webhook')),
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    max_send_rate integer NOT NULL DEFAULT 14,
    verified boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, channel, from_address)
);

CREATE TABLE IF NOT EXISTS templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL,
    channel text NOT NULL CHECK (channel IN ('email','webhook')),
    subject_template text,
    html_template text,
    text_template text,
    body_template text,
    sending_identity_id uuid REFERENCES sending_identities(id),
    version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ( (channel='email' AND html_template IS NOT NULL)
         OR (channel='webhook' AND body_template IS NOT NULL) )
);

CREATE TABLE IF NOT EXISTS tracked_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    template_id uuid NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    original_url text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (template_id, original_url)
);
