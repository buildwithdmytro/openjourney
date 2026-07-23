export type Profile = {
  id: string;
  external_id?: string;
  anonymous_id?: string;
  attributes: Record<string, unknown>;
  version: number;
  updated_at: string;
};

export type Consent = {
  profile_id: string;
  channel: string;
  topic: string;
  state: "subscribed" | "unsubscribed";
  occurred_at: string;
};

type ProfileResponse = { profile: Profile; consents: Consent[] };

export type EventSchema = {
  id: string; event_type: string; version: number; schema: Record<string, unknown>;
  status: string; compatibility: "none" | "backward"; created_at: string;
};
export type ConnectorPipeline = {
  id: string; tenant_id: string; workspace_id: string; app_id: string; connector_extension_id: string;
  name: string; direction: "source" | "sink" | "export"; status: "draft" | "enabled" | "disabled";
  current_version_id?: string; schedule_enabled: boolean; schedule_interval_seconds?: number;
  next_run_at?: string; last_run_at?: string; created_at: string; updated_at: string;
};
export type ConnectorRun = {
  id: string; pipeline_id: string; job_type: string; status: "running" | "succeeded" | "failed" | "dead";
  cursor?: string; rows_in: number; rows_out: number; rows_rejected: number; reject_blob_key?: string;
  error?: string; started_at: string; finished_at?: string;
};
export type APIKey = {
  id: string; name: string; scopes: string[]; expires_at?: string; revoked_at?: string; last_used_at?: string; created_at: string;
};
export type QueueStatus = { queue: string; pending: number; processing: number; dead: number };
export type ReplayReport = {
  match: boolean; live_checksum: string; replay_checksum: string; event_count: number; profile_count: number;
};
export type DeadLetterItem = {
  queue: string; id: string; subject_id?: string; kind: string; attempts: number;
  last_error?: string; payload?: Record<string, unknown>; created_at: string;
};
export type PrivacyRequest = {
  id: string; external_id: string; request_type: "export" | "delete"; status: string;
  artifact_key?: string; error?: string; created_at: string; completed_at?: string;
};
export type Role = {
  id: string; name: string; permissions: string[]; system: boolean; created_at: string;
};
export type User = {
  id: string; oidc_issuer: string; oidc_subject: string; email?: string; display_name?: string;
  password?: string; local?: boolean; role_ids: string[]; created_at: string;
};
export type UserInput = {
  oidc_issuer?: string; oidc_subject?: string; email?: string; display_name?: string;
  password?: string; role_ids: string[];
};
export type AuditEvent = {
  id: string; actor_type: string; actor_id: string; action: string; resource_type: string;
  resource_id?: string; metadata: Record<string, unknown>; occurred_at: string;
  seq?: number; prev_hash?: string; row_hash?: string;
};
export type AuditFilter = {
  actor_id?: string;
  resource_type?: string;
  action?: string;
  start_time?: string;
  end_time?: string;
  limit?: number;
};
export type AuditVerificationResult = {
  status: string;
  intact: boolean;
  total_events: number;
  first_broken_seq?: number;
  first_broken_id?: string;
  reason?: string;
};
export type AuthSession = { access_token: string; token_type: "Bearer"; expires_at: string };

export async function checkHealth(baseURL: string): Promise<boolean> {
  const response = await fetch(`${baseURL}/health/ready`);
  return response.ok;
}

export async function login(baseURL: string, email: string, password: string): Promise<AuthSession> {
  const response = await fetch(`${baseURL}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.error?.message ?? `Request failed (${response.status})`);
  }
  return response.json() as Promise<AuthSession>;
}

export async function logout(baseURL: string, accessToken: string): Promise<void> {
  await fetch(`${baseURL}/v1/auth/logout`, {
    method: "POST",
    headers: { Authorization: `Bearer ${accessToken}` },
  });
}

export async function getProfile(
  baseURL: string,
  apiKey: string,
  externalID: string,
): Promise<ProfileResponse> {
  const response = await fetch(
    `${baseURL}/v1/profiles/${encodeURIComponent(externalID)}`,
    { headers: { Authorization: `Bearer ${apiKey}` } },
  );
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.error?.message ?? `Request failed (${response.status})`);
  }
  return response.json() as Promise<ProfileResponse>;
}

async function requestJSON<T>(baseURL: string, apiKey: string, path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json", ...init?.headers },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.error?.message ?? `Request failed (${response.status})`);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export async function listSchemas(baseURL: string, apiKey: string): Promise<EventSchema[]> {
  return (await requestJSON<{ schemas: EventSchema[] | null }>(baseURL, apiKey, "/v1/schemas")).schemas ?? [];
}
export async function createSchema(baseURL: string, apiKey: string,
  input: Omit<EventSchema, "id" | "status" | "created_at">): Promise<EventSchema> {
  return requestJSON(baseURL, apiKey, "/v1/schemas", { method: "POST", body: JSON.stringify(input) });
}
export async function listConnectorPipelines(baseURL: string, apiKey: string): Promise<ConnectorPipeline[]> {
  return (await requestJSON<{ pipelines: ConnectorPipeline[] | null }>(baseURL, apiKey, "/v1/connectors/pipelines")).pipelines ?? [];
}
export async function createConnectorPipeline(baseURL: string, apiKey: string, input: Partial<ConnectorPipeline>): Promise<ConnectorPipeline> {
  return requestJSON(baseURL, apiKey, "/v1/connectors/pipelines", { method: "POST", body: JSON.stringify(input) });
}
export async function updateConnectorPipeline(baseURL: string, apiKey: string, id: string, input: Partial<ConnectorPipeline>): Promise<ConnectorPipeline> {
  return requestJSON(baseURL, apiKey, `/v1/connectors/pipelines/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}
export async function publishConnectorPipeline(baseURL: string, apiKey: string, id: string, mapping: Record<string, unknown>): Promise<unknown> {
  return requestJSON(baseURL, apiKey, `/v1/connectors/pipelines/${encodeURIComponent(id)}/publish`, { method: "POST", body: JSON.stringify({ mapping }) });
}
export async function listConnectorRuns(baseURL: string, apiKey: string, pipelineID: string): Promise<ConnectorRun[]> {
  return (await requestJSON<{ runs: ConnectorRun[] | null }>(baseURL, apiKey, `/v1/connectors/pipelines/${encodeURIComponent(pipelineID)}/runs`)).runs ?? [];
}
export async function identifyIdentity(baseURL: string, apiKey: string, input: Record<string, unknown>): Promise<unknown> {
  return requestJSON(baseURL, apiKey, "/v1/identity/identify", { method: "POST", body: JSON.stringify(input) });
}
export async function mergeIdentity(baseURL: string, apiKey: string, input: Record<string, unknown>): Promise<unknown> {
  return requestJSON(baseURL, apiKey, "/v1/identity/merge", { method: "POST", body: JSON.stringify(input) });
}
export async function unmergeIdentity(baseURL: string, apiKey: string, input: Record<string, unknown>): Promise<unknown> {
  return requestJSON(baseURL, apiKey, "/v1/identity/unmerge", { method: "POST", body: JSON.stringify(input) });
}
export async function listAPIKeys(baseURL: string, apiKey: string): Promise<APIKey[]> {
  return (await requestJSON<{ api_keys: APIKey[] | null }>(baseURL, apiKey, "/v1/api-keys")).api_keys ?? [];
}
export async function createAPIKey(baseURL: string, apiKey: string, name: string, scopes: string, expiresAt?: string):
Promise<{ api_key: APIKey; secret: string }>;
export async function createAPIKey(baseURL: string, apiKey: string, name: string, scopes: string[], expiresAt?: string):
Promise<{ api_key: APIKey; secret: string }>;
export async function createAPIKey(baseURL: string, apiKey: string, name: string, scopes: string | string[], expiresAt?: string) {
  const body: { name: string; scopes: string[]; expires_at?: string } = {
    name, scopes: Array.isArray(scopes) ? scopes : [scopes],
  };
  if (expiresAt) body.expires_at = expiresAt;
  return requestJSON<{ api_key: APIKey; secret: string }>(baseURL, apiKey, "/v1/api-keys", {
    method: "POST", body: JSON.stringify(body),
  });
}
export async function revokeAPIKey(baseURL: string, apiKey: string, id: string): Promise<void> {
  return requestJSON(baseURL, apiKey, `/v1/api-keys/${encodeURIComponent(id)}`, { method: "DELETE" });
}
export async function getQueueStatus(baseURL: string, apiKey: string): Promise<QueueStatus[]> {
  return (await requestJSON<{ queues: QueueStatus[] | null }>(baseURL, apiKey, "/v1/operations/queues")).queues ?? [];
}
export async function listDeadLetters(baseURL: string, apiKey: string, queue = ""): Promise<DeadLetterItem[]> {
  const query = queue ? `?queue=${encodeURIComponent(queue)}` : "";
  return (await requestJSON<{ dead_letters: DeadLetterItem[] | null }>(baseURL, apiKey, `/v1/operations/dlq${query}`)).dead_letters ?? [];
}
export async function retryDeadLetter(baseURL: string, apiKey: string, queue: string, id: string): Promise<void> {
  return requestJSON(baseURL, apiKey, `/v1/operations/dlq/${encodeURIComponent(queue)}/${encodeURIComponent(id)}/retry`, { method: "POST" });
}
export async function discardDeadLetter(baseURL: string, apiKey: string, queue: string, id: string): Promise<void> {
  return requestJSON(baseURL, apiKey, `/v1/operations/dlq/${encodeURIComponent(queue)}/${encodeURIComponent(id)}/discard`, { method: "POST" });
}
export async function replayVerify(baseURL: string, apiKey: string): Promise<ReplayReport> {
  return requestJSON(baseURL, apiKey, "/v1/operations/replay/verify", { method: "POST" });
}
export async function createPrivacyRequest(
  baseURL: string,
  apiKey: string,
  externalID: string,
  requestType: "export" | "delete",
): Promise<PrivacyRequest> {
  return requestJSON(baseURL, apiKey, "/v1/privacy/requests", {
    method: "POST", body: JSON.stringify({ external_id: externalID, request_type: requestType }),
  });
}
export async function getPrivacyRequest(baseURL: string, apiKey: string, id: string): Promise<PrivacyRequest> {
  return requestJSON(baseURL, apiKey, `/v1/privacy/requests/${encodeURIComponent(id)}`);
}
export async function listRoles(baseURL: string, apiKey: string): Promise<Role[]> {
  return (await requestJSON<{ roles: Role[] | null }>(baseURL, apiKey, "/v1/roles")).roles ?? [];
}
export async function createRole(baseURL: string, apiKey: string, name: string, permissions: string[]): Promise<Role> {
  return requestJSON(baseURL, apiKey, "/v1/roles", { method: "POST", body: JSON.stringify({ name, permissions }) });
}
export async function listUsers(baseURL: string, apiKey: string): Promise<User[]> {
  return (await requestJSON<{ users: User[] | null }>(baseURL, apiKey, "/v1/users")).users ?? [];
}
export async function createUser(baseURL: string, apiKey: string, user: UserInput): Promise<User> {
  return requestJSON(baseURL, apiKey, "/v1/users", { method: "POST", body: JSON.stringify(user) });
}
export async function listAuditEvents(baseURL: string, apiKey: string, filters?: AuditFilter): Promise<AuditEvent[]> {
  const params = new URLSearchParams();
  if (filters?.limit) params.set("limit", String(filters.limit));
  else params.set("limit", "100");
  if (filters?.actor_id) params.set("actor_id", filters.actor_id);
  if (filters?.resource_type) params.set("resource_type", filters.resource_type);
  if (filters?.action) params.set("action", filters.action);
  if (filters?.start_time) params.set("start_time", filters.start_time);
  if (filters?.end_time) params.set("end_time", filters.end_time);

  return (await requestJSON<{ audit_events: AuditEvent[] | null }>(baseURL, apiKey, `/v1/audit?${params.toString()}`)).audit_events ?? [];
}
export async function verifyAuditChain(baseURL: string, apiKey: string): Promise<AuditVerificationResult> {
  return requestJSON<AuditVerificationResult>(baseURL, apiKey, "/v1/audit/verify");
}

export type Segment = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  description?: string;
  type: "static" | "dynamic" | "snapshot";
  status: "draft" | "active" | "archived";
  dsl: Record<string, unknown>;
  version: number;
  created_at: string;
  updated_at: string;
};

export type SegmentMember = {
  segment_id: string;
  profile_id: string;
  tenant_id: string;
  membership: "include" | "exclude";
  created_at: string;
};

export type ScoringModel = {
  id: string; name: string; kind: "expression" | "llm"; latest_version: number;
  current_version_id?: string; created_at: string; updated_at: string;
};
export type ScoringModelVersion = {
  id: string; scoring_model_id: string; version: number; score_name: string;
  definition: Record<string, unknown>; output_min: number; output_max: number;
  manifest_key: string; status: string; eval_status: string; published_at?: string;
};
export type ProfileScore = {
  profile_id: string; scoring_model_id: string; score_name: string; value: number;
  model_version: number; computed_at: string;
};

export type FormField = {
  key: string; type: "text" | "email" | "number" | "integer" | "boolean";
  required?: boolean; validation?: Record<string, unknown>; consent?: boolean; maps_to?: string;
};
export type Form = {
  id: string; tenant_id: string; workspace_id: string; name: string; status: "draft" | "published" | "archived";
  draft: { fields: FormField[]; submit_actions?: Record<string, unknown> }; latest_version: number;
  current_version_id?: string; created_at: string; updated_at: string;
};
export type LandingPage = {
  id: string; tenant_id: string; workspace_id: string; slug: string; name: string;
  status: "draft" | "published" | "archived"; draft: { template: string; form_id?: string; form_version?: number; meta?: Record<string, unknown> };
  latest_version: number; current_version_id?: string; created_at: string; updated_at: string;
};
export type Asset = { id: string; filename: string; content_type: string; blob_key: string; size_bytes: number; created_at: string };

export async function listForms(baseURL: string, apiKey: string): Promise<Form[]> {
  return (await requestJSON<{ forms: Form[] | null }>(baseURL, apiKey, "/v1/forms")).forms ?? [];
}
export async function createForm(baseURL: string, apiKey: string, input: { name: string; draft: Form["draft"] }): Promise<Form> {
  return requestJSON(baseURL, apiKey, "/v1/forms", { method: "POST", body: JSON.stringify(input) });
}
export async function updateForm(baseURL: string, apiKey: string, id: string, input: { name: string; draft: Form["draft"] }): Promise<Form> {
  return requestJSON(baseURL, apiKey, `/v1/forms/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}
export async function publishForm(baseURL: string, apiKey: string, id: string): Promise<unknown> {
  return requestJSON(baseURL, apiKey, `/v1/forms/${encodeURIComponent(id)}/publish`, { method: "POST" });
}
export async function listLandingPages(baseURL: string, apiKey: string): Promise<LandingPage[]> {
  return (await requestJSON<{ pages: LandingPage[] | null }>(baseURL, apiKey, "/v1/pages")).pages ?? [];
}
export async function createLandingPage(baseURL: string, apiKey: string, input: { name: string; slug: string; draft: LandingPage["draft"] }): Promise<LandingPage> {
  return requestJSON(baseURL, apiKey, "/v1/pages", { method: "POST", body: JSON.stringify(input) });
}
export async function updateLandingPage(baseURL: string, apiKey: string, id: string, input: { name: string; slug: string; draft: LandingPage["draft"] }): Promise<LandingPage> {
  return requestJSON(baseURL, apiKey, `/v1/pages/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}
export async function publishLandingPage(baseURL: string, apiKey: string, id: string): Promise<unknown> {
  return requestJSON(baseURL, apiKey, `/v1/pages/${encodeURIComponent(id)}/publish`, { method: "POST" });
}
export async function listAssets(baseURL: string, apiKey: string): Promise<Asset[]> {
  return (await requestJSON<{ assets: Asset[] | null }>(baseURL, apiKey, "/v1/assets")).assets ?? [];
}

export type ShortLink = { id: string; slug: string; destination_url: string; utm?: Record<string, string>; created_at: string };
export async function listShortLinks(baseURL: string, apiKey: string): Promise<ShortLink[]> {
  return (await requestJSON<{ links: ShortLink[] | null }>(baseURL, apiKey, "/v1/links")).links ?? [];
}
export async function createShortLink(baseURL: string, apiKey: string, input: { slug: string; destination_url: string; utm: Record<string, string> }): Promise<ShortLink> {
  return requestJSON(baseURL, apiKey, "/v1/links", { method: "POST", body: JSON.stringify(input) });
}

export type Company = { id: string; name: string; external_id?: string; attributes: Record<string, unknown>; members?: { profile_id: string; role?: string }[] };
export async function listCompanies(baseURL: string, apiKey: string): Promise<Company[]> {
  return (await requestJSON<{ companies: Company[] | null }>(baseURL, apiKey, "/v1/companies")).companies ?? [];
}
export async function createCompany(baseURL: string, apiKey: string, input: { name: string; external_id?: string; attributes: Record<string, unknown>; members: { profile_id: string; role?: string }[] }): Promise<Company> {
  return requestJSON(baseURL, apiKey, "/v1/companies", { method: "POST", body: JSON.stringify(input) });
}

export type ImportRequest = { id: string; kind: string; status: string; total_rows: number; imported_rows: number; failed_rows: number; result_ref?: string; error?: string };
export async function uploadImport(baseURL: string, apiKey: string, file: File, kind: string, mapping: Record<string, string>): Promise<ImportRequest> {
  const body = new FormData(); body.append("file", file); body.append("kind", kind); body.append("mapping", JSON.stringify(mapping));
  const response = await fetch(`${baseURL}/v1/imports`, { method: "POST", headers: { Authorization: `Bearer ${apiKey}` }, body });
  if (!response.ok) throw new Error(`Request failed (${response.status})`);
  return response.json() as Promise<ImportRequest>;
}
export function getImport(baseURL: string, apiKey: string, id: string): Promise<ImportRequest> {
  return requestJSON(baseURL, apiKey, `/v1/imports/${encodeURIComponent(id)}`);
}
export type StageRule = { id: string; stage: string; segment_id: string; priority: number; enabled: boolean };
export async function listStageRules(baseURL: string, apiKey: string): Promise<StageRule[]> {
  return (await requestJSON<{ stages: StageRule[] | null }>(baseURL, apiKey, "/v1/stages")).stages ?? [];
}
export async function createStageRule(baseURL: string, apiKey: string, input: { stage: string; segment_id: string; priority: number }): Promise<StageRule> {
  return requestJSON(baseURL, apiKey, "/v1/stages", { method: "POST", body: JSON.stringify(input) });
}
export type LeadScoreInput = { name: string; score_name: string; expression: string; output_max: number };
export async function createLeadScore(baseURL: string, apiKey: string, input: LeadScoreInput): Promise<unknown> {
  return requestJSON(baseURL, apiKey, "/v1/scoring/lead-models", { method: "POST", body: JSON.stringify(input) });
}
export async function uploadAsset(baseURL: string, apiKey: string, file: File): Promise<Asset> {
  const body = new FormData(); body.append("file", file);
  const response = await fetch(`${baseURL}/v1/assets`, { method: "POST", headers: { Authorization: `Bearer ${apiKey}` }, body });
  if (!response.ok) throw new Error(`Request failed (${response.status})`);
  return response.json() as Promise<Asset>;
}

export async function listScoringModels(baseURL: string, apiKey: string): Promise<ScoringModel[]> {
  return (await requestJSON<{ models: ScoringModel[] | null }>(baseURL, apiKey, "/v1/scoring/models")).models ?? [];
}
export async function createScoringModel(baseURL: string, apiKey: string, input: { name: string; kind: ScoringModel["kind"] }): Promise<ScoringModel> {
  return requestJSON(baseURL, apiKey, "/v1/scoring/models", { method: "POST", body: JSON.stringify(input) });
}
export async function createScoringModelVersion(baseURL: string, apiKey: string, id: string, input: Partial<ScoringModelVersion>): Promise<ScoringModelVersion> {
  return requestJSON(baseURL, apiKey, `/v1/scoring/models/${encodeURIComponent(id)}/versions`, { method: "POST", body: JSON.stringify(input) });
}
export async function publishScoringModelVersion(baseURL: string, apiKey: string, id: string, version: number, manifestKey: string): Promise<ScoringModelVersion> {
  return requestJSON(baseURL, apiKey, `/v1/scoring/models/${encodeURIComponent(id)}/publish`, { method: "POST", body: JSON.stringify({ version, manifest_key: manifestKey }) });
}
export async function listProfileScores(baseURL: string, apiKey: string, profileID: string): Promise<ProfileScore[]> {
  return (await requestJSON<{ scores: ProfileScore[] | null }>(baseURL, apiKey, `/v1/scoring/profiles/${encodeURIComponent(profileID)}`)).scores ?? [];
}

export async function listSegments(baseURL: string, apiKey: string): Promise<Segment[]> {
  return (await requestJSON<{ segments: Segment[] | null }>(baseURL, apiKey, "/v1/segments")).segments ?? [];
}

export async function createSegment(baseURL: string, apiKey: string, input: Partial<Segment>): Promise<Segment> {
  return requestJSON(baseURL, apiKey, "/v1/segments", { method: "POST", body: JSON.stringify(input) });
}

export async function getSegment(baseURL: string, apiKey: string, id: string): Promise<Segment> {
  return requestJSON(baseURL, apiKey, `/v1/segments/${encodeURIComponent(id)}`);
}

export async function updateSegment(baseURL: string, apiKey: string, id: string, input: Partial<Segment>): Promise<Segment> {
  return requestJSON(baseURL, apiKey, `/v1/segments/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export async function setSegmentMembers(baseURL: string, apiKey: string, id: string, members: Partial<SegmentMember>[]): Promise<void> {
  return requestJSON(baseURL, apiKey, `/v1/segments/${encodeURIComponent(id)}/members`, { method: "PUT", body: JSON.stringify(members) });
}

export type SendingIdentity = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  channel: string;
  provider?: string;
  display_name: string;
  from_address: string;
  reply_to?: string;
  config?: Record<string, string>;
  created_at: string;
};

export type DeviceToken = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  app_id: string;
  profile_id: string;
  platform: string;
  provider: string;
  token: string;
  active: boolean;
  created_at: string;
};

export async function listDeviceTokens(baseURL: string, apiKey: string, profileId: string): Promise<DeviceToken[]> {
  const result = await requestJSON<DeviceToken[] | null>(baseURL, apiKey, `/v1/device-tokens?profile_id=${encodeURIComponent(profileId)}`);
  return Array.isArray(result) ? result : [];
}

export async function retireDeviceToken(baseURL: string, apiKey: string, id: string): Promise<void> {
  await requestJSON(baseURL, apiKey, `/v1/device-tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export type Template = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  channel: string;
  subject_template?: string;
  html_template?: string;
  text_template?: string;
  body_template?: string;
  sending_identity_id?: string;
  version: number;
  created_at: string;
  updated_at: string;
};

export type TemplatePreview = { subject: string; body: string };

export async function listTemplates(baseURL: string, apiKey: string): Promise<Template[]> {
  return (await requestJSON<{ templates: Template[] }>(baseURL, apiKey, "/v1/templates")).templates ?? [];
}

export async function getTemplate(baseURL: string, apiKey: string, id: string): Promise<Template> {
  return requestJSON(baseURL, apiKey, `/v1/templates/${encodeURIComponent(id)}`);
}

export async function createTemplate(baseURL: string, apiKey: string, input: Partial<Template>): Promise<Template> {
  return requestJSON(baseURL, apiKey, "/v1/templates", { method: "POST", body: JSON.stringify(input) });
}

export async function updateTemplate(baseURL: string, apiKey: string, id: string, input: Partial<Template>): Promise<Template> {
  return requestJSON(baseURL, apiKey, `/v1/templates/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export type CopilotResponse = {
  draft?: Record<string, unknown>;
  activity_id?: string;
  [key: string]: unknown;
};

export type AIProviderConfig = {
  id: string; provider: "fake" | "anthropic" | "openai"; is_default: boolean;
  config: Record<string, unknown>; endpoint_allowlist: string[]; fallback_provider?: string;
  monthly_budget_cents: number; status: "active" | "disabled"; created_at: string; updated_at: string;
};
export type AIBudget = { usage: { period: string; cost_cents: number; input_tokens: number; output_tokens: number }; monthly_budget_cents: number };
export type AIActivity = { id: string; action: string; provider: string; model: string; policy_decision: string; cost_cents: number; input_tokens: number; output_tokens: number; created_at: string };
export type Extension = { id: string; name: string; publisher: string; latest_version: number; status: "installed" | "enabled" | "disabled"; current_version_id?: string };
export type ExtensionConfig = { extension_id: string; config: Record<string, unknown>; endpoint_allowlist: string[]; timeout_ms: number; max_memory_mb: number; monthly_budget_cents: number; rate_per_min: number; status: "active" | "disabled" };
export type ExtensionGrant = { extension_id: string; scope: string; granted_by: string; granted_at: string };
export type ExtensionActivity = { id: string; extension_id: string; extension_version: number; kind: string; invocation: string; derived_scopes: string[]; policy_decision: string; latency_ms: number; created_at: string };
export async function listExtensions(baseURL: string, apiKey: string): Promise<Extension[]> { return (await requestJSON<{ extensions: Extension[] }>(baseURL, apiKey, "/v1/extensions")).extensions ?? []; }
export async function installExtension(baseURL: string, apiKey: string, input: Record<string, unknown>): Promise<Extension> { return (await requestJSON<{ extension: Extension }>(baseURL, apiKey, "/v1/extensions/install", { method: "POST", body: JSON.stringify(input) })).extension; }
export async function updateExtension(baseURL: string, apiKey: string, id: string, status: Extension["status"]): Promise<Extension> { return requestJSON(baseURL, apiKey, `/v1/extensions/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify({ status }) }); }
export async function getExtensionConfig(baseURL: string, apiKey: string, id: string): Promise<ExtensionConfig> { return requestJSON(baseURL, apiKey, `/v1/extensions/${encodeURIComponent(id)}/config`); }
export async function saveExtensionConfig(baseURL: string, apiKey: string, id: string, input: Partial<ExtensionConfig>): Promise<ExtensionConfig> { return requestJSON(baseURL, apiKey, `/v1/extensions/${encodeURIComponent(id)}/config`, { method: "PUT", body: JSON.stringify(input) }); }
export async function listExtensionGrants(baseURL: string, apiKey: string, id: string): Promise<ExtensionGrant[]> { return (await requestJSON<{ grants: ExtensionGrant[] }>(baseURL, apiKey, `/v1/extensions/${encodeURIComponent(id)}/grants`)).grants ?? []; }
export async function grantExtensionScope(baseURL: string, apiKey: string, id: string, scope: string): Promise<ExtensionGrant> { return requestJSON(baseURL, apiKey, `/v1/extensions/${encodeURIComponent(id)}/grants`, { method: "POST", body: JSON.stringify({ scope }) }); }
export async function listExtensionActivity(baseURL: string, apiKey: string, id: string): Promise<{ activities: ExtensionActivity[]; health: { state: string; consecutive_failures: number } }> { return requestJSON(baseURL, apiKey, `/v1/extensions/${encodeURIComponent(id)}/activity?limit=100`); }
export type FieldClassification = { id: string; entity_type: "profile" | "event"; field_path: string; classification: "public" | "internal" | "confidential" | "restricted"; send_to_model: "allow" | "redact" | "tokenize" | "deny"; created_at: string };

export async function listAIProviders(baseURL: string, apiKey: string): Promise<AIProviderConfig[]> {
  return (await requestJSON<{ providers: AIProviderConfig[] | null }>(baseURL, apiKey, "/v1/ai/providers")).providers ?? [];
}
export async function saveAIProvider(baseURL: string, apiKey: string, input: Partial<AIProviderConfig>): Promise<AIProviderConfig> {
  const path = input.id ? `/v1/ai/providers/${encodeURIComponent(input.id)}` : "/v1/ai/providers";
  return requestJSON(baseURL, apiKey, path, { method: input.id ? "PUT" : "POST", body: JSON.stringify(input) });
}
export async function getAIBudget(baseURL: string, apiKey: string): Promise<AIBudget> { return requestJSON(baseURL, apiKey, "/v1/ai/budget"); }
export async function listAIActivity(baseURL: string, apiKey: string): Promise<AIActivity[]> {
  return (await requestJSON<{ activities: AIActivity[] | null }>(baseURL, apiKey, "/v1/ai/activity?limit=100")).activities ?? [];
}
export async function listFieldClassifications(baseURL: string, apiKey: string): Promise<FieldClassification[]> {
  return (await requestJSON<{ classifications: FieldClassification[] | null }>(baseURL, apiKey, "/v1/ai/field-classifications")).classifications ?? [];
}
export async function saveFieldClassification(baseURL: string, apiKey: string, input: Partial<FieldClassification>): Promise<FieldClassification> {
  const path = input.id ? `/v1/ai/field-classifications/${encodeURIComponent(input.id)}` : "/v1/ai/field-classifications";
  return requestJSON(baseURL, apiKey, path, { method: input.id ? "PUT" : "POST", body: JSON.stringify(input) });
}

async function invokeCopilot(baseURL: string, apiKey: string, path: string, input?: Record<string, unknown>): Promise<CopilotResponse> {
  return requestJSON<CopilotResponse>(baseURL, apiKey, path, {
    method: "POST",
    body: JSON.stringify(input ?? {}),
  });
}

export function createContentCopilot(baseURL: string, apiKey: string, input: { brief: string; locale?: string }): Promise<CopilotResponse> {
  return invokeCopilot(baseURL, apiKey, "/v1/ai/copilots/content", input);
}

export function createAudienceCopilot(baseURL: string, apiKey: string, brief: string): Promise<CopilotResponse> {
  return invokeCopilot(baseURL, apiKey, "/v1/ai/copilots/audience", { brief });
}

export function createJourneyCopilot(baseURL: string, apiKey: string, input: { brief: string; name?: string }): Promise<CopilotResponse> {
  return invokeCopilot(baseURL, apiKey, "/v1/ai/copilots/journey", input);
}

export function createPerformanceCopilot(baseURL: string, apiKey: string, campaignID: string): Promise<CopilotResponse> {
  return invokeCopilot(baseURL, apiKey, `/v1/ai/copilots/performance/${encodeURIComponent(campaignID)}`);
}

export type InsightsCopilotResponse = {
  summary: string;
  insights: string[];
  key_metrics: Array<{
    name: string;
    value: any;
    source: string;
  }>;
  activity_id?: string;
  trace: Array<{
    step: number;
    action: string;
    tool?: string;
    args?: Record<string, unknown>;
    result?: string;
    activity_id?: string;
  }>;
  status: string;
};

export function createInsightsCopilot(baseURL: string, apiKey: string, question: string, query?: Record<string, unknown>): Promise<InsightsCopilotResponse> {
  return requestJSON<InsightsCopilotResponse>(baseURL, apiKey, "/v1/ai/copilots/insights", {
    method: "POST",
    body: JSON.stringify({ question, query }),
  });
}

export async function previewTemplate(baseURL: string, apiKey: string, id: string, externalId: string): Promise<TemplatePreview> {
  return requestJSON(baseURL, apiKey, `/v1/templates/${encodeURIComponent(id)}/preview`, {
    method: "POST",
    body: JSON.stringify({ external_id: externalId }),
  });
}

export type InAppMessage = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  app_id: string;
  profile_id: string;
  template_id?: string;
  campaign_id?: string;
  journey_run_id?: string;
  delivery_attempt_id?: string;
  message_type: "modal" | "banner" | "fullscreen" | "card";
  content: Record<string, unknown>;
  rank: number;
  categories: string[];
  start_at: string;
  expires_at?: string;
  idempotency_key?: string;
  status: "pending" | "delivered" | "displayed" | "clicked" | "dismissed" | "expired";
  delivered_at?: string;
  displayed_at?: string;
  clicked_at?: string;
  dismissed_at?: string;
  created_at: string;
  updated_at: string;
};

export async function listMessages(baseURL: string, apiKey: string): Promise<InAppMessage[]> {
  return (await requestJSON<{ messages: InAppMessage[] }>(baseURL, apiKey, "/v1/messages")).messages ?? [];
}

export async function getMessage(baseURL: string, apiKey: string, id: string): Promise<InAppMessage> {
  return requestJSON(baseURL, apiKey, `/v1/messages/${encodeURIComponent(id)}`);
}

export async function createMessage(baseURL: string, apiKey: string, input: Partial<InAppMessage>): Promise<InAppMessage> {
  return requestJSON(baseURL, apiKey, "/v1/messages", { method: "POST", body: JSON.stringify(input) });
}

export async function getProfileInbox(baseURL: string, apiKey: string, profileId: string): Promise<InAppMessage[]> {
  return (await requestJSON<{ messages: InAppMessage[] }>(baseURL, apiKey, `/v1/messages/profile/${encodeURIComponent(profileId)}`)).messages ?? [];
}

export async function listSendingIdentities(baseURL: string, apiKey: string): Promise<SendingIdentity[]> {
  return (await requestJSON<{ identities?: SendingIdentity[] }>(baseURL, apiKey, "/v1/sending-identities")).identities ?? [];
}

export async function createSendingIdentity(baseURL: string, apiKey: string, input: Partial<SendingIdentity>): Promise<SendingIdentity> {
  return requestJSON(baseURL, apiKey, "/v1/sending-identities", { method: "POST", body: JSON.stringify(input) });
}

export async function updateSendingIdentity(baseURL: string, apiKey: string, id: string, input: Partial<SendingIdentity>): Promise<SendingIdentity> {
  return requestJSON(baseURL, apiKey, `/v1/sending-identities/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export type Suppression = {
  id: string;
  tenant_id: string;
  channel: string;
  endpoint: string;
  reason: "bounce" | "complaint" | "unsubscribe" | "admin";
  source_event_id?: string;
  created_at: string;
};

export async function listSuppressions(baseURL: string, apiKey: string): Promise<Suppression[]> {
  const result = await requestJSON<Suppression[] | null>(baseURL, apiKey, "/v1/suppressions");
  return Array.isArray(result) ? result : [];
}

export async function createSuppression(baseURL: string, apiKey: string, input: Partial<Suppression>): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(baseURL, apiKey, "/v1/suppressions", { method: "POST", body: JSON.stringify(input) });
}

export async function deleteSuppression(baseURL: string, apiKey: string, channel: string, endpoint: string): Promise<void> {
  await requestJSON<void>(baseURL, apiKey, `/v1/suppressions?channel=${encodeURIComponent(channel)}&endpoint=${encodeURIComponent(endpoint)}`, { method: "DELETE" });
}

export type Campaign = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  description?: string;
  segment_id: string;
  template_id: string;
  experiment_id?: string | null;
  status: "draft" | "scheduled" | "building" | "sending" | "paused" | "completed" | "failed" | "archived";
  scheduled_at?: string;
  manifest_key?: string;
  segment_version: number;
  template_version: number;
  evaluated_at?: string;
  recipient_count: number;
  created_at: string;
  updated_at: string;
};

export async function listCampaigns(baseURL: string, apiKey: string): Promise<Campaign[]> {
  const result = await requestJSON<Campaign[] | null>(baseURL, apiKey, "/v1/campaigns");
  return Array.isArray(result) ? result : [];
}

export async function getCampaign(baseURL: string, apiKey: string, id: string): Promise<Campaign> {
  return requestJSON<Campaign>(baseURL, apiKey, `/v1/campaigns/${encodeURIComponent(id)}`);
}

export async function createCampaign(baseURL: string, apiKey: string, input: Partial<Campaign>): Promise<Campaign> {
  return requestJSON<Campaign>(baseURL, apiKey, "/v1/campaigns", { method: "POST", body: JSON.stringify(input) });
}

export async function updateCampaign(baseURL: string, apiKey: string, id: string, input: Partial<Campaign>): Promise<Campaign> {
  return requestJSON<Campaign>(baseURL, apiKey, `/v1/campaigns/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export type ExperimentVariant = {
  id?: string;
  experiment_id?: string;
  label: string;
  weight: number;
  is_control: boolean;
  template_id?: string;
};

export type Experiment = {
  id: string;
  name: string;
  description?: string;
  subject_type: "campaign" | "journey";
  status: "draft" | "running" | "completed" | "archived";
  method: "frequentist";
  seed: string;
  holdout_pct: number;
  variants?: ExperimentVariant[];
  created_at?: string;
  updated_at?: string;
};

export async function listExperiments(baseURL: string, apiKey: string): Promise<Experiment[]> {
  const result = await requestJSON<Experiment[] | null>(baseURL, apiKey, "/v1/experiments");
  return Array.isArray(result) ? result : [];
}

export async function createExperiment(baseURL: string, apiKey: string, input: Partial<Experiment>): Promise<Experiment> {
  return requestJSON<Experiment>(baseURL, apiKey, "/v1/experiments", { method: "POST", body: JSON.stringify(input) });
}

export async function getExperiment(baseURL: string, apiKey: string, id: string): Promise<Experiment> {
  return requestJSON<Experiment>(baseURL, apiKey, `/v1/experiments/${encodeURIComponent(id)}`);
}

export async function updateExperiment(baseURL: string, apiKey: string, id: string, input: Partial<Experiment>): Promise<Experiment> {
  return requestJSON<Experiment>(baseURL, apiKey, `/v1/experiments/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export type OptimizationProposal = {
  id: string; experiment_id: string; kind: "reallocate" | "winner";
  report_snapshot: Record<string, unknown>; proposed_weights?: Record<string, number>;
  winner_variant?: string; rationale: string; status: "proposed" | "approved" | "rejected" | "superseded";
  approved_by?: string; approved_at?: string; created_at: string;
};
export type ExperimentVersion = {
  id: string; experiment_id: string; version: number; seed: string; holdout_pct: number;
  variants: ExperimentVariant[]; approved_by: string; created_at: string;
};
export async function proposeExperimentOptimization(baseURL: string, apiKey: string, id: string): Promise<OptimizationProposal> {
  return requestJSON(baseURL, apiKey, `/v1/experiments/${encodeURIComponent(id)}/optimize`, { method: "POST" });
}
export async function approveExperimentOptimization(baseURL: string, apiKey: string, id: string, proposalID: string): Promise<ExperimentVersion> {
  return requestJSON(baseURL, apiKey, `/v1/experiments/${encodeURIComponent(id)}/optimize/${encodeURIComponent(proposalID)}/approve`, { method: "POST" });
}

export type ReportCount = { total: number; unique: number };

export type ReportFunnel = {
  targeted: ReportCount;
  sent: ReportCount;
  suppressed: ReportCount;
  no_consent: ReportCount;
  fatigued: ReportCount;
  render_failed: ReportCount;
  send_failed: ReportCount;
  failed: ReportCount;
  holdout: ReportCount;
  delivered: ReportCount;
  opened: ReportCount;
  clicked: ReportCount;
  converted: ReportCount;
};

export type ReportDeliverability = {
  bounced: ReportCount;
  complained: ReportCount;
  bounce_rate: number;
  complaint_rate: number;
};

export type CampaignReport = { campaign_id: string; funnel: ReportFunnel; deliverability: ReportDeliverability };
export type JourneyReport = { journey_id: string; funnel: ReportFunnel; deliverability: ReportDeliverability };

export type ExperimentVariantReport = {
  label: string;
  is_control: boolean;
  sent: number;
  conversions: number;
  rate: number;
  uplift: number;
  z_score: number;
  p_value: number;
  ci_low: number;
  ci_high: number;
  guardrails: Array<{ goal_name: string; conversions: number; rate: number }>;
};

export type ExperimentReport = {
  experiment_id: string;
  winner_variant?: string;
  variants: ExperimentVariantReport[];
};

export type Overview = {
  profiles: number;
  journeys: number;
  campaigns: number;
  delivery_attempts: number;
  inapp_messages: number;
  connector_runs: number;
};

export async function getOverview(baseURL: string, apiKey: string): Promise<Overview> {
  return requestJSON<Overview>(baseURL, apiKey, "/v1/overview");
}

export async function getCampaignReport(baseURL: string, apiKey: string, id: string): Promise<CampaignReport> {
  return requestJSON<CampaignReport>(baseURL, apiKey, `/v1/reports/campaigns/${encodeURIComponent(id)}`);
}

export async function getJourneyReport(baseURL: string, apiKey: string, id: string): Promise<JourneyReport> {
  return requestJSON<JourneyReport>(baseURL, apiKey, `/v1/reports/journeys/${encodeURIComponent(id)}`);
}

export async function getExperimentReport(baseURL: string, apiKey: string, id: string): Promise<ExperimentReport> {
  return requestJSON<ExperimentReport>(baseURL, apiKey, `/v1/reports/experiments/${encodeURIComponent(id)}`);
}

export type Journey = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  description?: string;
  status: "draft" | "published" | "archived";
  graph: Record<string, unknown>;
  latest_version: number;
  current_version_id?: string;
  created_at: string;
  updated_at: string;
};

export async function listJourneys(baseURL: string, apiKey: string): Promise<Journey[]> {
  return (await requestJSON<{ journeys: Journey[] }>(baseURL, apiKey, "/v1/journeys")).journeys ?? [];
}

export async function getJourney(baseURL: string, apiKey: string, id: string): Promise<Journey> {
  return requestJSON<Journey>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}`);
}

export async function createJourney(baseURL: string, apiKey: string, input: Partial<Journey>): Promise<Journey> {
  return requestJSON<Journey>(baseURL, apiKey, "/v1/journeys", { method: "POST", body: JSON.stringify(input) });
}

export async function updateJourney(baseURL: string, apiKey: string, id: string, input: Partial<Journey>): Promise<Journey> {
  return requestJSON<Journey>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export type JourneyVersion = {
  id: string;
  journey_id: string;
  tenant_id: string;
  workspace_id: string;
  version: number;
  graph: Record<string, unknown>;
  manifest_key?: string;
  entry_kind: "event" | "scheduled";
  entry_event_type?: string;
  entry_segment_id?: string;
  entry_schedule?: string;
  reentry_policy: "once" | "always" | "after_exit";
  max_reentries: number;
  late_policy: "run" | "skip" | "reschedule";
  status: "active" | "paused" | "archived";
  published_by?: string;
  published_at: string;
};

export async function publishJourney(baseURL: string, apiKey: string, id: string): Promise<JourneyVersion> {
  return requestJSON<JourneyVersion>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}/publish`, {
    method: "POST",
  });
}

export type JourneyRun = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  journey_id: string;
  journey_version_id: string;
  profile_id: string;
  subject_external_id: string;
  entry_key: string;
  reentry_sequence: number;
  status: "active" | "completed" | "canceled";
  current_node_id: string;
  state: Record<string, unknown>;
  wait_event_type?: string;
  wait_until?: string;
  goal_reached?: boolean;
  entered_at: string;
  updated_at: string;
  completed_at?: string;
};

export type JourneyTransition = {
  id: string;
  run_id: string;
  tenant_id: string;
  from_node?: string;
  to_node?: string;
  node_type: string;
  outcome: string;
  detail: Record<string, unknown>;
  occurred_at: string;
};

export type JourneyStep = {
  id: string;
  run_id: string;
  tenant_id: string;
  node_id: string;
  kind: "advance" | "timeout";
  status: "pending" | "processing" | "completed" | "failed" | "dead";
  attempts: number;
  available_at: string;
  locked_until?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
};

export type JourneyMessageIntent = {
  id: string;
  run_id: string;
  tenant_id: string;
  workspace_id: string;
  journey_id: string;
  journey_version_id: string;
  node_id: string;
  profile_id: string;
  template_id: string;
  channel: string;
  endpoint: string;
  transactional: boolean;
  status: "pending" | "processing" | "completed" | "failed" | "dead";
  attempts: number;
  available_at: string;
  locked_until?: string;
  decision?: string;
  reason?: string;
  provider_message_id?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
};

export async function updateJourneyVersionStatus(baseURL: string, apiKey: string, id: string, version: number, status: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}/versions/${version}`, {
    method: "PUT",
    body: JSON.stringify({ status }),
  });
}

export async function getJourneyVersion(baseURL: string, apiKey: string, id: string, version: number): Promise<JourneyVersion> {
  return requestJSON<JourneyVersion>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}/versions/${version}`);
}

export async function cancelJourneyRun(baseURL: string, apiKey: string, id: string, runID: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/cancel`, {
    method: "POST",
  });
}

export async function listJourneyRuns(baseURL: string, apiKey: string, id: string): Promise<JourneyRun[]> {
  return (await requestJSON<{ runs: JourneyRun[] }>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}/runs`)).runs ?? [];
}

export async function listJourneyRunTransitions(baseURL: string, apiKey: string, id: string, runID: string): Promise<JourneyTransition[]> {
  return (await requestJSON<{ transitions: JourneyTransition[] }>(baseURL, apiKey, `/v1/journeys/${encodeURIComponent(id)}/runs/${encodeURIComponent(runID)}/transitions`)).transitions ?? [];
}

export async function listJourneyDLQ(baseURL: string, apiKey: string): Promise<{ steps: JourneyStep[]; intents: JourneyMessageIntent[] }> {
  const result = await requestJSON<{ steps: JourneyStep[] | null; intents: JourneyMessageIntent[] | null }>(baseURL, apiKey, "/v1/journeys/dlq");
  return { steps: result.steps ?? [], intents: result.intents ?? [] };
}

export async function retryJourneyDLQ(baseURL: string, apiKey: string, kind: string, id: string): Promise<{ status: string }> {
  return requestJSON<{ status: string }>(baseURL, apiKey, `/v1/journeys/dlq/${encodeURIComponent(kind)}/${encodeURIComponent(id)}/retry`, {
    method: "POST",
  });
}

export type FlagVariant = {
  label: string;
  value: unknown;
  weight: number;
};

export type FlagTargetingRule = {
  dsl: Record<string, unknown>;
  variant: string;
};

export type FeatureFlag = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  app_id: string;
  environment: string;
  key: string;
  name?: string;
  description?: string;
  flag_type: "boolean" | "string" | "number" | "json";
  default_value: unknown;
  variants?: FlagVariant[];
  targeting_rules?: FlagTargetingRule[];
  rollout_pct: number;
  seed: string;
  enabled: boolean;
  status: "draft" | "published" | "disabled";
  current_version_id?: string;
  created_at: string;
  updated_at: string;
};

export type FeatureFlagExposure = {
  id: string;
  tenant_id: string;
  app_id: string;
  flag_id: string;
  environment: string;
  variant: string;
  exposures: number;
  first_seen?: string;
  last_seen?: string;
};

export async function listFeatureFlags(baseURL: string, apiKey: string): Promise<FeatureFlag[]> {
  const result = await requestJSON<{ flags: FeatureFlag[] | null }>(baseURL, apiKey, "/v1/flags");
  return Array.isArray(result.flags) ? result.flags : [];
}

export async function getFeatureFlag(baseURL: string, apiKey: string, id: string): Promise<FeatureFlag> {
  return requestJSON<FeatureFlag>(baseURL, apiKey, `/v1/flags/${encodeURIComponent(id)}`);
}

export async function createFeatureFlag(baseURL: string, apiKey: string, input: Partial<FeatureFlag>): Promise<FeatureFlag> {
  return requestJSON<FeatureFlag>(baseURL, apiKey, "/v1/flags", { method: "POST", body: JSON.stringify(input) });
}

export async function updateFeatureFlag(baseURL: string, apiKey: string, id: string, input: Partial<FeatureFlag>): Promise<FeatureFlag> {
  return requestJSON<FeatureFlag>(baseURL, apiKey, `/v1/flags/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export async function publishFeatureFlag(baseURL: string, apiKey: string, id: string): Promise<{ id: string; version: number }> {
  return requestJSON<{ id: string; version: number }>(baseURL, apiKey, `/v1/flags/${encodeURIComponent(id)}/publish`, { method: "POST" });
}

export async function setFeatureFlagStatus(baseURL: string, apiKey: string, id: string, status: string): Promise<FeatureFlag> {
  return requestJSON<FeatureFlag>(baseURL, apiKey, `/v1/flags/${encodeURIComponent(id)}/status`, { method: "PUT", body: JSON.stringify({ status }) });
}

export type TimeBucket = {
  time: string;
  funnel: ReportFunnel;
  deliverability: ReportDeliverability;
};

export type FunnelOverTimeReport = {
  campaign_id?: string;
  journey_id?: string;
  buckets: TimeBucket[];
};

export type CohortData = {
  cohort_time: string;
  sizes: number[];
};

export type RetentionReport = {
  campaign_id?: string;
  journey_id?: string;
  granularity: string;
  cohorts: CohortData[];
};

export type GrowthBucket = {
  time: string;
  new_profiles: number;
  net_growth: number;
  segment_memberships: number;
};

export type GrowthReport = {
  campaign_id?: string;
  journey_id?: string;
  buckets: GrowthBucket[];
};

export type CostBucket = {
  time: string;
  total_cost_micros: number;
  send_count: number;
  cost_per_send: number;
};

export type CostReport = {
  campaign_id?: string;
  journey_id?: string;
  buckets: CostBucket[];
};

export type SavedReport = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  report_type: string;
  query: Record<string, unknown>;
  created_by_user_id?: string;
  created_at: string;
  updated_at: string;
};

export type Catalog = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  app_id: string;
  key: string;
  name: string;
  description?: string;
  item_key_field: string;
  status: "active" | "archived";
  item_count: number;
  created_at: string;
  updated_at: string;
};

export type CatalogItem = {
  id: string;
  catalog_id: string;
  tenant_id: string;
  app_id: string;
  item_key: string;
  payload: Record<string, unknown>;
  updated_at: string;
};

export type ConnectedContentSource = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  allowed_host: string;
  auth_header_name?: string;
  auth_secret_ref?: string;
  default_ttl_seconds: number;
  timeout_ms: number;
  enabled: boolean;
  status: "draft" | "active" | "disabled";
  created_by_user_id?: string;
  created_at: string;
  updated_at: string;
};

export type Prompt = {
  id: string;
  tenant_id: string;
  workspace_id: string;
  name: string;
  task_type: string;
  current_version_id?: string;
  latest_version: number;
  created_at: string;
  updated_at: string;
};

export type PromptVersion = {
  id: string;
  prompt_id: string;
  tenant_id: string;
  version: number;
  template: string;
  input_schema: Record<string, unknown>;
  output_schema: Record<string, unknown>;
  provider: string;
  model: string;
  params: Record<string, unknown>;
  safety_policy: Record<string, unknown>;
  manifest_key?: string;
  status: "draft" | "active" | "archived" | string;
  eval_status: "pending" | "passed" | "failed" | string;
  published_by?: string;
  published_at?: string;
  created_at: string;
};


export async function getCampaignFunnelOverTimeReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<FunnelOverTimeReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/campaigns/${encodeURIComponent(id)}/funnel-over-time${params ? `?${params}` : ''}`;
  return requestJSON<FunnelOverTimeReport>(baseURL, apiKey, path);
}

export async function getJourneyFunnelOverTimeReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<FunnelOverTimeReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/journeys/${encodeURIComponent(id)}/funnel-over-time${params ? `?${params}` : ''}`;
  return requestJSON<FunnelOverTimeReport>(baseURL, apiKey, path);
}

export async function getCampaignRetentionReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<RetentionReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/campaigns/${encodeURIComponent(id)}/retention${params ? `?${params}` : ''}`;
  return requestJSON<RetentionReport>(baseURL, apiKey, path);
}

export async function getJourneyRetentionReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<RetentionReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/journeys/${encodeURIComponent(id)}/retention${params ? `?${params}` : ''}`;
  return requestJSON<RetentionReport>(baseURL, apiKey, path);
}

export async function getCampaignGrowthReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<GrowthReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/campaigns/${encodeURIComponent(id)}/growth${params ? `?${params}` : ''}`;
  return requestJSON<GrowthReport>(baseURL, apiKey, path);
}

export async function getJourneyGrowthReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<GrowthReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/journeys/${encodeURIComponent(id)}/growth${params ? `?${params}` : ''}`;
  return requestJSON<GrowthReport>(baseURL, apiKey, path);
}

export async function getCampaignCostReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<CostReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/campaigns/${encodeURIComponent(id)}/cost${params ? `?${params}` : ''}`;
  return requestJSON<CostReport>(baseURL, apiKey, path);
}

export async function getJourneyCostReport(
  baseURL: string,
  apiKey: string,
  id: string,
  query?: Record<string, unknown>,
): Promise<CostReport> {
  const params = query ? new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)])) : null;
  const path = `/v1/reports/journeys/${encodeURIComponent(id)}/cost${params ? `?${params}` : ''}`;
  return requestJSON<CostReport>(baseURL, apiKey, path);
}

export async function listSavedReports(baseURL: string, apiKey: string): Promise<SavedReport[]> {
  return (await requestJSON<{ reports: SavedReport[] | null }>(baseURL, apiKey, "/v1/saved-reports")).reports ?? [];
}

export async function createSavedReport(
  baseURL: string,
  apiKey: string,
  input: Omit<SavedReport, "id" | "tenant_id" | "created_by_user_id" | "created_at" | "updated_at">,
): Promise<SavedReport> {
  return requestJSON<SavedReport>(baseURL, apiKey, "/v1/saved-reports", { method: "POST", body: JSON.stringify(input) });
}

export async function getSavedReport(baseURL: string, apiKey: string, id: string): Promise<SavedReport> {
  return requestJSON<SavedReport>(baseURL, apiKey, `/v1/saved-reports/${encodeURIComponent(id)}`);
}

export async function deleteSavedReport(baseURL: string, apiKey: string, id: string): Promise<void> {
  await requestJSON<void>(baseURL, apiKey, `/v1/saved-reports/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listCatalogs(baseURL: string, apiKey: string): Promise<Catalog[]> {
  return (await requestJSON<{ catalogs: Catalog[] | null }>(baseURL, apiKey, "/v1/catalogs")).catalogs ?? [];
}

export async function createCatalog(baseURL: string, apiKey: string, input: Omit<Catalog, "id" | "tenant_id" | "workspace_id" | "item_count" | "created_at" | "updated_at">): Promise<Catalog> {
  return requestJSON<Catalog>(baseURL, apiKey, "/v1/catalogs", { method: "POST", body: JSON.stringify(input) });
}

export async function getCatalog(baseURL: string, apiKey: string, id: string): Promise<Catalog> {
  return requestJSON<Catalog>(baseURL, apiKey, `/v1/catalogs/${encodeURIComponent(id)}`);
}

export async function updateCatalog(baseURL: string, apiKey: string, id: string, input: Partial<Omit<Catalog, "id" | "tenant_id" | "workspace_id" | "created_at" | "updated_at">>): Promise<Catalog> {
  return requestJSON<Catalog>(baseURL, apiKey, `/v1/catalogs/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export async function deleteCatalog(baseURL: string, apiKey: string, id: string): Promise<void> {
  await requestJSON<void>(baseURL, apiKey, `/v1/catalogs/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listCatalogItems(baseURL: string, apiKey: string, catalogID: string, limit = 100): Promise<CatalogItem[]> {
  const params = new URLSearchParams({ limit: String(limit) });
  return (await requestJSON<{ items: CatalogItem[] | null }>(baseURL, apiKey, `/v1/catalogs/${encodeURIComponent(catalogID)}/items?${params}`)).items ?? [];
}

export async function bulkUploadCatalogItems(baseURL: string, apiKey: string, catalogID: string, file: File): Promise<void> {
  const body = new FormData(); body.append("file", file);
  const response = await fetch(`${baseURL}/v1/catalogs/${encodeURIComponent(catalogID)}/items:bulk`, { method: "POST", headers: { Authorization: `Bearer ${apiKey}` }, body });
  if (!response.ok) throw new Error(`Request failed (${response.status})`);
}

export async function listConnectedContentSources(baseURL: string, apiKey: string): Promise<ConnectedContentSource[]> {
  return (await requestJSON<{ sources: ConnectedContentSource[] | null }>(baseURL, apiKey, "/v1/connected-content-sources")).sources ?? [];
}

export async function createConnectedContentSource(baseURL: string, apiKey: string, input: Omit<ConnectedContentSource, "id" | "tenant_id" | "workspace_id" | "created_by_user_id" | "created_at" | "updated_at">): Promise<ConnectedContentSource> {
  return requestJSON<ConnectedContentSource>(baseURL, apiKey, "/v1/connected-content-sources", { method: "POST", body: JSON.stringify(input) });
}

export async function getConnectedContentSource(baseURL: string, apiKey: string, id: string): Promise<ConnectedContentSource> {
  return requestJSON<ConnectedContentSource>(baseURL, apiKey, `/v1/connected-content-sources/${encodeURIComponent(id)}`);
}

export async function updateConnectedContentSource(baseURL: string, apiKey: string, id: string, input: Partial<Omit<ConnectedContentSource, "id" | "tenant_id" | "workspace_id" | "created_by_user_id" | "created_at" | "updated_at">>): Promise<ConnectedContentSource> {
  return requestJSON<ConnectedContentSource>(baseURL, apiKey, `/v1/connected-content-sources/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export async function enableConnectedContentSource(baseURL: string, apiKey: string, id: string): Promise<ConnectedContentSource> {
  return requestJSON<ConnectedContentSource>(baseURL, apiKey, `/v1/connected-content-sources/${encodeURIComponent(id)}/enable`, { method: "POST", body: JSON.stringify({}) });
}

export async function deleteConnectedContentSource(baseURL: string, apiKey: string, id: string): Promise<void> {
  await requestJSON<void>(baseURL, apiKey, `/v1/connected-content-sources/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listPrompts(baseURL: string, apiKey: string): Promise<Prompt[]> {
  return (await requestJSON<{ prompts: Prompt[] | null }>(baseURL, apiKey, "/v1/ai/prompts")).prompts ?? [];
}

export async function createPrompt(
  baseURL: string,
  apiKey: string,
  input: { name: string; task_type: string },
): Promise<Prompt> {
  return requestJSON<Prompt>(baseURL, apiKey, "/v1/ai/prompts", { method: "POST", body: JSON.stringify(input) });
}

export async function getPrompt(baseURL: string, apiKey: string, id: string): Promise<Prompt> {
  return requestJSON<Prompt>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(id)}`);
}

export async function updatePrompt(
  baseURL: string,
  apiKey: string,
  id: string,
  input: Partial<{ name: string; task_type: string }>,
): Promise<Prompt> {
  return requestJSON<Prompt>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input) });
}

export async function deletePrompt(baseURL: string, apiKey: string, id: string): Promise<void> {
  await requestJSON<void>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listPromptVersions(baseURL: string, apiKey: string, promptID: string): Promise<PromptVersion[]> {
  return (await requestJSON<{ versions: PromptVersion[] | null }>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(promptID)}/versions`)).versions ?? [];
}

export async function createPromptVersion(
  baseURL: string,
  apiKey: string,
  promptID: string,
  input: Partial<Omit<PromptVersion, "id" | "prompt_id" | "tenant_id" | "version" | "status" | "eval_status" | "created_at">>,
): Promise<PromptVersion> {
  return requestJSON<PromptVersion>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(promptID)}/versions`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function getPromptVersion(
  baseURL: string,
  apiKey: string,
  promptID: string,
  versionOrID: string | number,
): Promise<PromptVersion> {
  return requestJSON<PromptVersion>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(promptID)}/versions/${encodeURIComponent(String(versionOrID))}`);
}

export async function setPromptVersionEvalStatus(
  baseURL: string,
  apiKey: string,
  promptID: string,
  versionOrID: string | number,
  evalStatus: string,
): Promise<PromptVersion> {
  return requestJSON<PromptVersion>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(promptID)}/versions/${encodeURIComponent(String(versionOrID))}/eval`, {
    method: "POST",
    body: JSON.stringify({ eval_status: evalStatus }),
  });
}

export async function publishPromptVersion(
  baseURL: string,
  apiKey: string,
  promptID: string,
  versionOrID: string | number,
): Promise<PromptVersion> {
  return requestJSON<PromptVersion>(baseURL, apiKey, `/v1/ai/prompts/${encodeURIComponent(promptID)}/versions/${encodeURIComponent(String(versionOrID))}/publish`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

