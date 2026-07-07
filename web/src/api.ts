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
  return (await requestJSON<{ schemas: EventSchema[] }>(baseURL, apiKey, "/v1/schemas")).schemas;
}
export async function createSchema(baseURL: string, apiKey: string,
  input: Omit<EventSchema, "id" | "status" | "created_at">): Promise<EventSchema> {
  return requestJSON(baseURL, apiKey, "/v1/schemas", { method: "POST", body: JSON.stringify(input) });
}
export async function listAPIKeys(baseURL: string, apiKey: string): Promise<APIKey[]> {
  return (await requestJSON<{ api_keys: APIKey[] }>(baseURL, apiKey, "/v1/api-keys")).api_keys;
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
  return (await requestJSON<{ queues: QueueStatus[] }>(baseURL, apiKey, "/v1/operations/queues")).queues;
}
export async function listDeadLetters(baseURL: string, apiKey: string, queue = ""): Promise<DeadLetterItem[]> {
  const query = queue ? `?queue=${encodeURIComponent(queue)}` : "";
  return (await requestJSON<{ dead_letters: DeadLetterItem[] }>(baseURL, apiKey, `/v1/operations/dlq${query}`)).dead_letters;
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
  return (await requestJSON<{ roles: Role[] }>(baseURL, apiKey, "/v1/roles")).roles;
}
export async function createRole(baseURL: string, apiKey: string, name: string, permissions: string[]): Promise<Role> {
  return requestJSON(baseURL, apiKey, "/v1/roles", { method: "POST", body: JSON.stringify({ name, permissions }) });
}
export async function listUsers(baseURL: string, apiKey: string): Promise<User[]> {
  return (await requestJSON<{ users: User[] }>(baseURL, apiKey, "/v1/users")).users;
}
export async function createUser(baseURL: string, apiKey: string, user: UserInput): Promise<User> {
  return requestJSON(baseURL, apiKey, "/v1/users", { method: "POST", body: JSON.stringify(user) });
}
export async function listAuditEvents(baseURL: string, apiKey: string, limit = 100): Promise<AuditEvent[]> {
  return (await requestJSON<{ audit_events: AuditEvent[] }>(baseURL, apiKey, `/v1/audit?limit=${limit}`)).audit_events;
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

export async function listSegments(baseURL: string, apiKey: string): Promise<Segment[]> {
  return (await requestJSON<{ segments: Segment[] }>(baseURL, apiKey, "/v1/segments")).segments;
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
  display_name: string;
  from_address: string;
  reply_to?: string;
  created_at: string;
};

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

export async function previewTemplate(baseURL: string, apiKey: string, id: string, externalId: string): Promise<TemplatePreview> {
  return requestJSON(baseURL, apiKey, `/v1/templates/${encodeURIComponent(id)}/preview`, {
    method: "POST",
    body: JSON.stringify({ external_id: externalId }),
  });
}

export async function listSendingIdentities(baseURL: string, apiKey: string): Promise<SendingIdentity[]> {
  return (await requestJSON<{ identities?: SendingIdentity[] }>(baseURL, apiKey, "/v1/sending-identities")).identities ?? [];
}

export async function createSendingIdentity(baseURL: string, apiKey: string, input: Partial<SendingIdentity>): Promise<SendingIdentity> {
  return requestJSON(baseURL, apiKey, "/v1/sending-identities", { method: "POST", body: JSON.stringify(input) });
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
  return requestJSON<Suppression[]>(baseURL, apiKey, "/v1/suppressions") ?? [];
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
  return requestJSON<Campaign[]>(baseURL, apiKey, "/v1/campaigns") ?? [];
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


