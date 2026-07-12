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
  return (await requestJSON<{ schemas: EventSchema[] | null }>(baseURL, apiKey, "/v1/schemas")).schemas ?? [];
}
export async function createSchema(baseURL: string, apiKey: string,
  input: Omit<EventSchema, "id" | "status" | "created_at">): Promise<EventSchema> {
  return requestJSON(baseURL, apiKey, "/v1/schemas", { method: "POST", body: JSON.stringify(input) });
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
export async function listAuditEvents(baseURL: string, apiKey: string, limit = 100): Promise<AuditEvent[]> {
  return (await requestJSON<{ audit_events: AuditEvent[] | null }>(baseURL, apiKey, `/v1/audit?limit=${limit}`)).audit_events ?? [];
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
