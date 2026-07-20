import { Component, ErrorInfo, FormEvent, lazy, ReactNode, Suspense, useEffect, useRef, useState } from "react";
import {
  checkHealth, Consent, createAPIKey, createPrivacyRequest, createRole, createSchema, createUser,
  discardDeadLetter, getPrivacyRequest, getProfile, getQueueStatus, listAPIKeys, listAuditEvents,
  listDeadLetters, listRoles, listSchemas, listUsers, login, logout, Profile, replayVerify, retryDeadLetter, revokeAPIKey,
  APIKey, AuditEvent, DeadLetterItem, EventSchema, PrivacyRequest, QueueStatus, ReplayReport, Role, User,
  createSegment, listSegments, updateSegment, setSegmentMembers, Segment, SegmentMember,
  listTemplates, getTemplate, createTemplate, updateTemplate, previewTemplate,
  listSendingIdentities, createSendingIdentity, Template, SendingIdentity, TemplatePreview,
  listMessages, getMessage, createMessage, getProfileInbox, InAppMessage,
  DeviceToken, listDeviceTokens, retireDeviceToken,
  listSuppressions, createSuppression, deleteSuppression, Suppression,
  listCampaigns, getCampaign, createCampaign, updateCampaign, Campaign,
  listJourneys, createJourney, Journey, listScoringModels, ScoringModel,
} from "./api";
import { oidcConfigured, restoreOIDCSession, signIn, signOut } from "./auth";

const Journeys = lazy(() => import("./sections/Journeys"));
const Experiments = lazy(() => import("./sections/Experiments"));
const Reports = lazy(() => import("./sections/Reports"));
const Copilots = lazy(() => import("./sections/Copilots"));
const Governance = lazy(() => import("./sections/Governance"));
const Extensions = lazy(() => import("./sections/Extensions"));
const Scoring = lazy(() => import("./sections/Scoring"));
const Acquisition = lazy(() => import("./sections/Acquisition"));
const Connectors = lazy(() => import("./sections/Connectors"));
const Messaging = lazy(() => import("./sections/Messaging"));

const apiBase = import.meta.env.VITE_API_BASE_URL || "/api";

class UIErrorBoundary extends Component<{ children: ReactNode; resetKey: string }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) { return { error }; }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("OpenJourney UI failed", error, info.componentStack);
  }

  componentDidUpdate(previous: Readonly<{ children: ReactNode; resetKey: string }>) {
    if (previous.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null });
  }

  render() {
    if (this.state.error) {
      return <section className="card ui-crash" role="alert"><h2>This view hit a problem</h2><p>{this.state.error.message}</p><button onClick={() => this.setState({ error: null })}>Try again</button></section>;
    }
    return this.props.children;
  }
}
type View = "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "scoring" | "templates" | "campaigns" | "journeys" | "experiments" | "reports" | "copilots" | "governance" | "extensions" | "connectors" | "suppressions" | "sender-identities" | "device-tokens" | "acquisition" | "messaging";
type CredentialSource = "manual" | "session" | "oidc";

const viewTitles: Record<View, [string, string]> = {
  profiles: ["Profiles", "Inspect the current customer and consent projection."],
  schemas: ["Event schemas", "Register typed event contracts and compatibility rules."],
  "api-keys": ["API keys", "Create scoped credentials and revoke access."],
  privacy: ["Privacy", "Submit and inspect DSAR export/delete operations."],
  access: ["Access", "Provision local/OIDC users and tenant roles."],
  operations: ["Operations", "Inspect queues, DLQs, and replay determinism."],
  audit: ["Audit", "Review tenant-scoped security and operations activity."],
  segments: ["Segments", "Manage customer segments and membership rules."],
  scoring: ["Scoring", "Publish governed scoring models and inspect profile scores."],
  templates: ["Templates", "Design email templates with Liquid tags and live preview."],
  campaigns: ["Campaigns", "Schedule and manage sharded marketing campaigns linked to segments and templates."],
  journeys: ["Journeys", "Design, publish, and monitor automated customer experiences."],
  experiments: ["Experiments", "Create controlled tests with stable audience assignment."],
  reports: ["Reports", "Compare delivery, conversion, and experiment performance."],
  copilots: ["AI Copilots", "Create governed drafts for review and human approval."],
  governance: ["AI Governance", "Manage providers, budgets, redaction, and AI activity."],
  extensions: ["Extensions", "Install signed providers, configure grants, and review extension health."],
  connectors: ["Connectors", "Move data through governed sources, sinks, exports, and identity commands."],
  suppressions: ["Suppressions", "Manage bounces, complaints, and manually suppressed endpoints."],
  "sender-identities": ["Sender Identities", "Manage verified sender emails, SMS, and push channels."],
  "device-tokens": ["Device Tokens", "Inspect and retire push device tokens per profile."],
  acquisition: ["Acquisition", "Build defended forms and immutable landing pages."],
  messaging: ["Messaging", "Create and manage in-app messages, content cards, and web push campaigns."],
};

function currentHashView(): View | null {
  const hash = window.location.hash.slice(1).split("?")[0] as View;
  return hash in viewTitles ? hash : null;
}

const AVAILABLE_SCOPES = [
  "*",
  "events:write",
  "profiles:read",
  "schemas:read",
  "schemas:write",
  "api_keys:read",
  "api_keys:write",
  "privacy:write",
  "operations:read",
  "operations:write",
  "users:read",
  "users:write",
  "roles:read",
  "roles:write",
  "segments:read",
  "segments:write",
  "templates:read",
  "templates:write",
  "campaigns:read",
  "campaigns:write",
  "suppressions:read",
  "suppressions:write",
  "journeys:read",
  "journeys:write",
  "journeys:publish",
  "experiments:read",
  "experiments:write",
  "reports:read",
  "messages:read",
  "messages:write",
  "connectors:read",
  "connectors:write",
  "connectors:run",
];

export function App() {
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [view, setView] = useState<View>(() => currentHashView() || "profiles");
  const [apiKey, setAPIKey] = useState(() => sessionStorage.getItem("oj_session_token") || localStorage.getItem("oj_api_key") || "");
  const [credentialSource, setCredentialSource] = useState<CredentialSource>(() =>
    sessionStorage.getItem("oj_session_token") ? "session" : "manual");
  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginError, setLoginError] = useState("");
  const [manualKey, setManualKey] = useState("");

  useEffect(() => {
    checkHealth(apiBase).then(setHealthy).catch(() => setHealthy(false));
    if (oidcConfigured) {
      restoreOIDCSession().then((token) => {
        if (token) {
          setCredentialSource("oidc");
          setAPIKey(token);
        }
      }).catch(() => undefined);
    }
  }, []);
  useEffect(() => {
    if (credentialSource === "session") {
      sessionStorage.setItem("oj_session_token", apiKey);
      localStorage.removeItem("oj_api_key");
      return;
    }
    sessionStorage.removeItem("oj_session_token");
    if (credentialSource === "oidc" || apiKey.trim() === "") {
      localStorage.removeItem("oj_api_key");
      return;
    }
    localStorage.setItem("oj_api_key", apiKey);
  }, [apiKey, credentialSource]);

  useEffect(() => {
    if (apiKey && currentHashView() !== view) {
      window.location.hash = view;
    }
  }, [view, apiKey]);

  useEffect(() => {
    const handleHashChange = () => {
      const hashView = currentHashView();
      if (hashView) setView(hashView);
    };
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, []);

  async function handleSignOut() {
    if (credentialSource === "session" && apiKey) {
      await logout(apiBase, apiKey).catch(() => undefined);
    }
    setCredentialSource("manual");
    setAPIKey("");
    setManualKey("");
    sessionStorage.removeItem("oj_session_token");
    localStorage.removeItem("oj_api_key");
    await signOut();
  }

  async function handleLocalLogin(event: FormEvent) {
    event.preventDefault();
    setLoginError("");
    try {
      const session = await login(apiBase, loginEmail, loginPassword);
      setCredentialSource("session");
      setAPIKey(session.access_token);
      setLoginPassword("");
    } catch (cause) {
      setLoginError(message(cause));
    }
  }

  if (!apiKey) {
    return (
      <div className="login-container">
        <div className="login-card">
          <div className="brand" style={{ display: "flex", justifyContent: "center", paddingBottom: "24px", margin: 0 }}>
            <span>O</span> OpenJourney
          </div>
          <h2>Welcome Back</h2>
          <p>Please log in using your credentials or provide a configured API Key to manage customer journeys.</p>
          
          <form onSubmit={handleLocalLogin} style={{ display: "flex", flexDirection: "column", gap: "16px", alignItems: "stretch" }}>
            <label style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
              Email
              <input type="email" value={loginEmail} onChange={(event) => setLoginEmail(event.target.value)}
                placeholder="admin@example.test" required style={{ width: "100%" }} />
            </label>
            <label style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
              Password
              <input type="password" value={loginPassword} onChange={(event) => setLoginPassword(event.target.value)}
                placeholder="Self-hosted operator password" required style={{ width: "100%" }} />
            </label>
            <button type="submit" disabled={!loginEmail || !loginPassword} style={{ width: "100%", marginTop: "8px" }}>
              Log in with credentials
            </button>
          </form>
          
          <ErrorMessage value={loginError} />
          
          <div style={{ margin: "24px 0", display: "flex", alignItems: "center", gap: "10px" }}>
            <div style={{ flex: 1, height: "1px", background: "#e8ebef" }} />
            <span style={{ fontSize: "11px", color: "#6c7787", fontWeight: "bold" }}>OR USE API KEY</span>
            <div style={{ flex: 1, height: "1px", background: "#e8ebef" }} />
          </div>
          
          <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
            <label style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
              Provide API Key / Token
              <input type="password" value={manualKey} onChange={(event) => setManualKey(event.target.value)}
                placeholder="Scoped API, local session, or OIDC token" style={{ width: "100%" }} />
            </label>
            <button onClick={() => {
              if (manualKey.trim()) {
                setCredentialSource("manual");
                setAPIKey(manualKey.trim());
              }
            }} disabled={!manualKey.trim()} style={{ width: "100%", background: "#101b2b" }}>
              Use API Key
            </button>
            
            {oidcConfigured && (
              <button onClick={() => void signIn()} style={{ width: "100%", background: "#e9edf2", color: "#344156" }}>
                Sign in with OIDC
              </button>
            )}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="shell">
      <aside>
        <div className="brand"><span>O</span> OpenJourney</div>
        <nav aria-label="Primary">
          {(["profiles", "segments", "scoring", "acquisition", "templates", "campaigns", "journeys", "experiments", "reports", "messaging", "copilots", "governance", "extensions", "connectors", "suppressions", "sender-identities", "device-tokens", "schemas", "api-keys", "privacy", "access", "operations", "audit"] as View[]).map((item) => (
            <button key={item} className={view === item ? "active" : ""}
              onClick={() => setView(item)}>{viewTitles[item][0]}</button>
          ))}
        </nav>
        <div style={{ marginTop: "auto", padding: "16px 0 0 0", borderTop: "1px solid rgba(255,255,255,0.1)", display: "flex", flexDirection: "column" }}>
          <button className="secondary small" onClick={() => void handleSignOut()} style={{ width: "100%", background: "transparent", border: "1px solid rgba(255,255,255,0.2)", color: "#dfe7f0", padding: "8px", borderRadius: "6px", cursor: "pointer", fontSize: "12px", fontWeight: "bold" }}>
            Sign out
          </button>
        </div>
        <div className={`health ${healthy ? "up" : ""}`} style={{ marginTop: "16px" }}>
          <i /> API {healthy === null ? "checking" : healthy ? "ready" : "unavailable"}
        </div>
      </aside>
      <main>
        <header>
          <p>Platform kernel</p>
          <h1>{viewTitles[view][0]}</h1>
          <span>{viewTitles[view][1]}</span>
        </header>
        <UIErrorBoundary resetKey={view}>
        {view === "profiles" && <Profiles apiKey={apiKey} />}
        {view === "segments" && <Segments apiKey={apiKey} />}
        {view === "scoring" && <Suspense fallback={<p role="status">Loading scoring…</p>}><Scoring apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "acquisition" && <Suspense fallback={<p role="status">Loading acquisition builder…</p>}><Acquisition apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "templates" && <Templates apiKey={apiKey} />}
        {view === "campaigns" && <Campaigns apiKey={apiKey} />}
        {view === "journeys" && (
          <Suspense fallback={<p role="status">Loading journey builder…</p>}>
            <Journeys apiKey={apiKey} />
          </Suspense>
        )}
        {view === "experiments" && <Suspense fallback={<p role="status">Loading experiments…</p>}><Experiments apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "reports" && <Suspense fallback={<p role="status">Loading reports…</p>}><Reports apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "messaging" && <Suspense fallback={<p role="status">Loading messaging…</p>}><Messaging apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "copilots" && <Suspense fallback={<p role="status">Loading AI copilots…</p>}><Copilots apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "governance" && <Suspense fallback={<p role="status">Loading AI governance…</p>}><Governance apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "extensions" && <Suspense fallback={<p role="status">Loading extensions…</p>}><Extensions apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "connectors" && <Suspense fallback={<p role="status">Loading connectors…</p>}><Connectors apiKey={apiKey} baseURL={apiBase} /></Suspense>}
        {view === "suppressions" && <Suppressions apiKey={apiKey} />}
        {view === "sender-identities" && <SenderIdentities apiKey={apiKey} />}
        {view === "device-tokens" && <DeviceTokensInspector apiKey={apiKey} />}
        {view === "schemas" && <Schemas apiKey={apiKey} />}
        {view === "api-keys" && <APIKeys apiKey={apiKey} />}
        {view === "privacy" && <Privacy apiKey={apiKey} />}
        {view === "access" && <Access apiKey={apiKey} />}
        {view === "operations" && <Operations apiKey={apiKey} />}
        {view === "audit" && <Audit apiKey={apiKey} />}
        </UIErrorBoundary>
      </main>
    </div>
  );
}

function Profiles({ apiKey }: { apiKey: string }) {
  const [externalID, setExternalID] = useState("");
  const [profile, setProfile] = useState<Profile | null>(null);
  const [consents, setConsents] = useState<Consent[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true); setError("");
    try {
      const result = await getProfile(apiBase, apiKey, externalID);
      setProfile(result.profile); setConsents(Array.isArray(result.consents) ? result.consents : []);
    } catch (cause) {
      setProfile(null); setConsents([]);
      setError(message(cause));
    } finally { setLoading(false); }
  }
  return <>
    <section className="card">
      <form onSubmit={submit} className="single-action">
        <label>External ID
          <input value={externalID} onChange={(event) => setExternalID(event.target.value)}
            placeholder="customer-123" required />
        </label>
        <button disabled={loading || !apiKey}>{loading ? "Loading…" : "Find profile"}</button>
      </form>
      <ErrorMessage value={error} />
    </section>
    {profile && <section className="profile-grid">
      <article className="card"><div className="eyebrow">Identity</div>
        <h2>{profile.external_id || profile.anonymous_id}</h2>
        <dl><div><dt>Profile ID</dt><dd>{profile.id}</dd></div>
          <div><dt>Version</dt><dd>{profile.version}</dd></div>
          <div><dt>Updated</dt><dd>{new Date(profile.updated_at).toLocaleString()}</dd></div></dl>
      </article>
      <article className="card"><div className="eyebrow">Attributes</div>
        <pre>{JSON.stringify(profile.attributes, null, 2)}</pre></article>
      <article className="card wide"><div className="eyebrow">Consent</div>
        {consents.length === 0 ? <p className="muted">No consent records.</p> :
          <table><thead><tr><th>Channel</th><th>Topic</th><th>State</th><th>Changed</th></tr></thead>
            <tbody>{consents.map((consent) => <tr key={`${consent.channel}:${consent.topic}`}>
              <td>{consent.channel}</td><td>{consent.topic}</td>
              <td><span className={`pill ${consent.state}`}>{consent.state}</span></td>
              <td>{new Date(consent.occurred_at).toLocaleString()}</td></tr>)}</tbody></table>}
      </article>
    </section>}
  </>;
}

function Schemas({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<EventSchema[]>([]);
  const [eventType, setEventType] = useState("");
  const [version, setVersion] = useState(1);
  const [definition, setDefinition] = useState('{"type":"object","properties":{}}');
  const [error, setError] = useState("");
  async function refresh() {
    try { setItems(await listSchemas(apiBase, apiKey)); setError(""); }
    catch (cause) { setError(message(cause)); }
  }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      await createSchema(apiBase, apiKey, {
        event_type: eventType, version, compatibility: "backward", schema: JSON.parse(definition),
      });
      setEventType(""); setVersion(version + 1); await refresh();
    } catch (cause) { setError(message(cause)); }
  }
  return <section className="stack">
    <article className="card"><form onSubmit={submit} className="schema-form">
      <label>Event type<input value={eventType} onChange={(e) => setEventType(e.target.value)}
        placeholder="product.viewed" required /></label>
      <label>Version<input type="number" min="1" value={version}
        onChange={(e) => setVersion(Number(e.target.value))} required /></label>
      <label className="full">JSON Schema<textarea value={definition}
        onChange={(e) => setDefinition(e.target.value)} rows={7} /></label>
      <button disabled={!apiKey}>Register schema</button>
    </form><ErrorMessage value={error} /></article>
    <article className="card"><div className="eyebrow">Registered schemas</div>
      <ResourceTable rows={items.map((item) => [item.event_type, `v${item.version}`, item.compatibility, item.status])}
        headers={["Event", "Version", "Compatibility", "Status"]} /></article>
  </section>;
}

function APIKeys({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<APIKey[]>([]);
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>(["events:write", "profiles:read"]);
  const [expiresAt, setExpiresAt] = useState("");
  const [secret, setSecret] = useState("");
  const [error, setError] = useState("");
  async function refresh() {
    try { setItems(await listAPIKeys(apiBase, apiKey)); setError(""); }
    catch (cause) { setError(message(cause)); }
  }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const expiration = expiresAt ? new Date(expiresAt).toISOString() : undefined;
      const result = await createAPIKey(apiBase, apiKey, name, scopes, expiration);
      setSecret(result.secret); setName(""); setExpiresAt(""); await refresh();
    } catch (cause) { setError(message(cause)); }
  }
  async function revoke(id: string) {
    try { await revokeAPIKey(apiBase, apiKey, id); await refresh(); }
    catch (cause) { setError(message(cause)); }
  }
  return <section className="stack">
    <article className="card"><form onSubmit={submit} className="single-action">
      <label>Name<input value={name} onChange={(e) => setName(e.target.value)}
        placeholder="Website ingestion" required /></label>
      <label>Scopes<ScopeSelector selected={scopes} onChange={setScopes} /></label>
      <label>Expires at<input type="datetime-local" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} /></label>
      <button disabled={!apiKey}>Create scoped key</button>
    </form>
      {secret && <div className="secret"><strong>Copy this secret now.</strong><code>{secret}</code></div>}
      <ErrorMessage value={error} /></article>
    <article className="card"><div className="eyebrow">Credentials</div>
      {items.map((item) => <div className="key-row" key={item.id}>
        <div><strong>{item.name}</strong><small>{item.scopes.join(", ")}</small>
          <small>Created {formatDate(item.created_at)} · Expires {formatDate(item.expires_at) || "never"} · Last used {formatDate(item.last_used_at) || "never"}</small></div>
        <button className="danger" disabled={Boolean(item.revoked_at)} onClick={() => void revoke(item.id)}>
          {item.revoked_at ? "Revoked" : "Revoke"}</button></div>)}</article>
  </section>;
}

function Privacy({ apiKey }: { apiKey: string }) {
  const [externalID, setExternalID] = useState("");
  const [requestType, setRequestType] = useState<"export" | "delete">("export");
  const [requestID, setRequestID] = useState("");
  const [item, setItem] = useState<PrivacyRequest | null>(null);
  const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const result = await createPrivacyRequest(apiBase, apiKey, externalID, requestType);
      setItem(result); setRequestID(result.id); setError("");
    } catch (cause) { setError(message(cause)); }
  }
  async function lookup(event: FormEvent) {
    event.preventDefault();
    try { setItem(await getPrivacyRequest(apiBase, apiKey, requestID)); setError(""); }
    catch (cause) { setError(message(cause)); }
  }
  return <section className="stack">
    <article className="card"><form onSubmit={submit}>
      <label>External ID<input value={externalID} onChange={(e) => setExternalID(e.target.value)}
        placeholder="customer-123" required /></label>
      <label>Request type<select value={requestType} onChange={(e) => setRequestType(e.target.value as "export" | "delete")}>
        <option value="export">Export</option><option value="delete">Delete</option></select></label>
      <button disabled={!apiKey}>Submit privacy request</button>
    </form></article>
    <article className="card"><form onSubmit={lookup} className="single-action">
      <label>Request ID<input value={requestID} onChange={(e) => setRequestID(e.target.value)}
        placeholder="privacy request UUID" required /></label>
      <button disabled={!apiKey}>Load request</button>
    </form><ErrorMessage value={error} />
      {item && <div className="details"><strong>{item.request_type} · {item.status}</strong>
        <span>{item.external_id}</span>{item.artifact_key && <code>{item.artifact_key}</code>}
        {item.error && <p className="error">{item.error}</p>}</div>}</article>
  </section>;
}

function Access({ apiKey }: { apiKey: string }) {
  const [roles, setRoles] = useState<Role[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [roleName, setRoleName] = useState("");
  const [permissions, setPermissions] = useState<string[]>(["profiles:read"]);
  const [issuer, setIssuer] = useState("https://identity.example.test");
  const [subject, setSubject] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [selectedRoles, setSelectedRoles] = useState("");
  const [error, setError] = useState("");
  async function refresh() {
    try {
      const [nextRoles, nextUsers] = await Promise.all([listRoles(apiBase, apiKey), listUsers(apiBase, apiKey)]);
      setRoles(nextRoles); setUsers(nextUsers); setError("");
    } catch (cause) { setError(message(cause)); }
  }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey]);
  async function addRole(event: FormEvent) {
    event.preventDefault();
    try {
      await createRole(apiBase, apiKey, roleName, permissions);
      setRoleName(""); await refresh();
    } catch (cause) { setError(message(cause)); }
  }
  async function addUser(event: FormEvent) {
    event.preventDefault();
    try {
      await createUser(apiBase, apiKey, {
        oidc_issuer: issuer || undefined, oidc_subject: subject || undefined,
        email, display_name: email, password: password || undefined,
        role_ids: selectedRoles.split(",").map((value) => value.trim()).filter(Boolean),
      });
      setSubject(""); setEmail(""); setPassword(""); await refresh();
    } catch (cause) { setError(message(cause)); }
  }
  return <section className="stack">
    <article className="card"><form onSubmit={addRole} className="schema-form">
      <label>Role name<input value={roleName} onChange={(e) => setRoleName(e.target.value)} required /></label>
      <label>Permissions<ScopeSelector selected={permissions} onChange={setPermissions} /></label>
      <button disabled={!apiKey}>Create role</button>
    </form></article>
    <article className="card"><form onSubmit={addUser} className="schema-form">
      <label>OIDC issuer<input value={issuer} onChange={(e) => setIssuer(e.target.value)}
        placeholder="leave blank for local user" /></label>
      <label>OIDC subject<input value={subject} onChange={(e) => setSubject(e.target.value)}
        placeholder="leave blank for local user" /></label>
      <label>Email<input value={email} onChange={(e) => setEmail(e.target.value)} /></label>
      <label>Password<input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
        placeholder="local user only, minimum 12 characters" /></label>
      <label>Role IDs<input value={selectedRoles} onChange={(e) => setSelectedRoles(e.target.value)}
        placeholder={roles[0]?.id || "comma-separated role ids"} required /></label>
      <button disabled={!apiKey}>Provision user</button>
    </form><ErrorMessage value={error} /></article>
    <article className="card"><div className="eyebrow">Roles</div>
      <ResourceTable headers={["Name", "Permissions", "System"]} rows={roles.map((role) =>
        [role.name, role.permissions.join(", "), role.system ? "yes" : "no"])} /></article>
    <article className="card"><div className="eyebrow">Users</div>
      <ResourceTable headers={["Email", "Type", "Subject", "Roles"]} rows={users.map((user) =>
        [user.email || "—", user.local ? "local" : "OIDC", user.oidc_subject, user.role_ids.join(", ")])} /></article>
  </section>;
}

function Operations({ apiKey }: { apiKey: string }) {
  const [queues, setQueues] = useState<QueueStatus[]>([]);
  const [deadLetters, setDeadLetters] = useState<DeadLetterItem[]>([]);
  const [dlqQueue, setDLQQueue] = useState("");
  const [report, setReport] = useState<ReplayReport | null>(null);
  const [error, setError] = useState("");
  async function refresh() {
    try {
      const [nextQueues, nextDeadLetters] = await Promise.all([
        getQueueStatus(apiBase, apiKey), listDeadLetters(apiBase, apiKey, dlqQueue),
      ]);
      setQueues(nextQueues); setDeadLetters(nextDeadLetters); setError("");
    }
    catch (cause) { setError(message(cause)); }
  }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey, dlqQueue]);
  async function replay() {
    try { setReport(await replayVerify(apiBase, apiKey)); }
    catch (cause) { setError(message(cause)); }
  }
  async function dlq(action: "retry" | "discard", item: DeadLetterItem) {
    try {
      if (action === "retry") await retryDeadLetter(apiBase, apiKey, item.queue, item.id);
      else await discardDeadLetter(apiBase, apiKey, item.queue, item.id);
      await refresh();
    } catch (cause) { setError(message(cause)); }
  }
  return <section className="stack">
    <article className="card"><div className="section-title"><div><div className="eyebrow">Durable work</div>
      <h2>Queue status</h2></div><button onClick={() => void refresh()}>Refresh</button></div>
      <ResourceTable rows={queues.map((q) => [q.queue, q.pending, q.processing, q.dead])}
        headers={["Queue", "Pending", "Processing", "Dead"]} /></article>
    <article className="card"><div className="section-title"><div><div className="eyebrow">Dead letters</div>
      <h2>DLQ actions</h2></div><label className="inline">Queue<select value={dlqQueue} onChange={(e) => setDLQQueue(e.target.value)}>
        <option value="">All</option><option value="projection">Projection</option>
        <option value="outbox">Outbox</option><option value="operations">Operations</option>
      </select></label></div>
      {deadLetters.length === 0 ? <p className="muted">No dead-letter items.</p> : deadLetters.map((item) =>
        <div className="key-row" key={`${item.queue}:${item.id}`}><div><strong>{item.queue} · {item.kind}</strong>
          <small>{item.subject_id || item.id} · attempts {item.attempts} · {item.last_error || "no error"}</small></div>
          <div className="row-actions"><button onClick={() => void dlq("retry", item)}>Retry</button>
            <button className="danger" onClick={() => void dlq("discard", item)}>Discard</button></div></div>)}</article>
    <article className="card"><div className="section-title"><div><div className="eyebrow">Determinism</div>
      <h2>Projection replay</h2></div><button onClick={() => void replay()}>Verify replay</button></div>
      {report && <div className={`replay ${report.match ? "match" : "drift"}`}>
        <strong>{report.match ? "Projection matches" : "Projection drift detected"}</strong>
        <span>{report.event_count} events · {report.profile_count} profiles</span>
        <code>{report.replay_checksum}</code></div>}
      <ErrorMessage value={error} /></article>
  </section>;
}

function Audit({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  async function refresh() {
    try { setItems(await listAuditEvents(apiBase, apiKey)); setError(""); }
    catch (cause) { setError(message(cause)); }
  }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey]);
  return <section className="card"><div className="section-title"><div><div className="eyebrow">Activity</div>
    <h2>Audit events</h2></div><button onClick={() => void refresh()}>Refresh</button></div>
    <ErrorMessage value={error} />
    <ResourceTable headers={["When", "Actor", "Action", "Resource"]} rows={items.map((item) => [
      new Date(item.occurred_at).toLocaleString(), `${item.actor_type}:${item.actor_id}`,
      item.action, `${item.resource_type}:${item.resource_id || ""}`,
    ])} /></section>;
}

function Segments({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<Segment[]>([]);
  const [editingSegment, setEditingSegment] = useState<Segment | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [type, setType] = useState<"static" | "dynamic" | "snapshot">("dynamic");
  const [status, setStatus] = useState<"draft" | "active" | "archived">("draft");
  const [dsl, setDsl] = useState("{}");
  const [memberProfileID, setMemberProfileID] = useState("");
  const [membership, setMembership] = useState<"include" | "exclude">("include");
  const [error, setError] = useState("");
  const [scoringModels, setScoringModels] = useState<ScoringModel[]>([]);
  const [scoreModel, setScoreModel] = useState("");
  const [scoreName, setScoreName] = useState("");
  const [scoreOperator, setScoreOperator] = useState("greater_than");
  const [scoreValue, setScoreValue] = useState(0);

  async function refresh() {
    try {
      const [segments, models] = await Promise.all([listSegments(apiBase, apiKey), listScoringModels(apiBase, apiKey)]);
      setItems(segments); setScoringModels(models);
      setError("");
    } catch (cause) {
      setError(message(cause));
    }
  }

  useEffect(() => {
    if (apiKey) void refresh();
  }, [apiKey]);

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    try {
      let parsedDSL = {};
      try {
        parsedDSL = JSON.parse(dsl);
      } catch (e) {
        throw new Error("Invalid DSL JSON: " + (e as Error).message);
      }
      if (scoreModel) parsedDSL = { type: "score", model: scoreModel, score_name: scoreName, operator: scoreOperator, value: scoreValue };
      await createSegment(apiBase, apiKey, {
        name,
        description,
        type,
        status,
        dsl: parsedDSL,
      });
      setName("");
      setDescription("");
      setDsl("{}");
      await refresh();
    } catch (cause) {
      setError(message(cause));
    }
  }

  async function handleUpdate(event: FormEvent) {
    event.preventDefault();
    if (!editingSegment) return;
    try {
      let parsedDSL = {};
      try {
        parsedDSL = JSON.parse(dsl);
      } catch (e) {
        throw new Error("Invalid DSL JSON: " + (e as Error).message);
      }
      if (scoreModel) parsedDSL = { type: "score", model: scoreModel, score_name: scoreName, operator: scoreOperator, value: scoreValue };
      await updateSegment(apiBase, apiKey, editingSegment.id, {
        name,
        description,
        type,
        status,
        dsl: parsedDSL,
      });
      setEditingSegment(null);
      setName("");
      setDescription("");
      setDsl("{}");
      await refresh();
    } catch (cause) {
      setError(message(cause));
    }
  }

  async function handleAddMember(event: FormEvent) {
    event.preventDefault();
    if (!editingSegment) return;
    try {
      if (!memberProfileID) throw new Error("Profile ID is required");
      await setSegmentMembers(apiBase, apiKey, editingSegment.id, [
        { profile_id: memberProfileID, membership: membership }
      ]);
      setMemberProfileID("");
      alert("Members updated successfully");
    } catch (cause) {
      setError(message(cause));
    }
  }

  function startEdit(seg: Segment) {
    setEditingSegment(seg);
    setName(seg.name);
    setDescription(seg.description || "");
    setType(seg.type);
    setStatus(seg.status);
    setDsl(JSON.stringify(seg.dsl, null, 2));
    const score = seg.dsl as { type?: string; model?: string; score_name?: string; operator?: string; value?: number };
    setScoreModel(score.type === "score" ? score.model || "" : ""); setScoreName(score.type === "score" ? score.score_name || "" : "");
    setScoreOperator(score.type === "score" ? score.operator || "greater_than" : "greater_than"); setScoreValue(score.type === "score" ? Number(score.value || 0) : 0);
  }

  return (
    <section className="stack">
      <article className="card">
        <h2>{editingSegment ? "Edit segment" : "Create segment"}</h2>
        <form onSubmit={editingSegment ? handleUpdate : handleCreate} className="schema-form">
          <label>Name
            <input value={name} onChange={(e) => setName(e.target.value)} required placeholder="SaaS Purchasers" />
          </label>
          <label>Description
            <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Customers who bought SaaS subscription" />
          </label>
          <label>Type
            <select value={type} onChange={(e) => setType(e.target.value as any)}>
              <option value="dynamic">dynamic</option>
              <option value="static">static</option>
              <option value="snapshot">snapshot</option>
            </select>
          </label>
          <label>Status
            <select value={status} onChange={(e) => setStatus(e.target.value as any)}>
              <option value="draft">draft</option>
              <option value="active">active</option>
              <option value="archived">archived</option>
            </select>
          </label>
          <label className="full">DSL Definition (JSON)
            <textarea value={dsl} onChange={(e) => setDsl(e.target.value)} rows={7} />
          </label>
          <fieldset className="full score-condition"><legend>Score condition (optional)</legend><div className="score-condition-fields"><label>Model<select value={scoreModel} onChange={e => setScoreModel(e.target.value)}><option value="">Use JSON DSL</option>{scoringModels.map(model => <option key={model.id} value={model.id}>{model.name}</option>)}</select></label><label>Score name<input value={scoreName} onChange={e => setScoreName(e.target.value)} placeholder="purchase_propensity" /></label><label>Operator<select value={scoreOperator} onChange={e => setScoreOperator(e.target.value)}><option value="greater_than">greater than</option><option value="less_than">less than</option><option value="equals">equals</option></select></label><label>Value<input type="number" step="any" value={scoreValue} onChange={e => setScoreValue(Number(e.target.value))} /></label></div><p className="field-help">Selecting a model writes a parameterized score leaf into the segment DSL.</p></fieldset>
          <div className="form-actions full">
            <button disabled={!apiKey}>{editingSegment ? "Update Segment" : "Create Segment"}</button>
            {editingSegment && <button type="button" className="secondary" onClick={() => {
              setEditingSegment(null);
              setName("");
              setDescription("");
              setDsl("{}");
            }}>Cancel</button>}
          </div>
        </form>
        <ErrorMessage value={error} />
      </article>

      {editingSegment && (
        <article className="card">
          <h2>Segment Membership</h2>
          <form onSubmit={handleAddMember} className="schema-form">
            <label>Profile ID
              <input value={memberProfileID} onChange={(e) => setMemberProfileID(e.target.value)} required placeholder="profile-uuid" />
            </label>
            <label>Membership
              <select value={membership} onChange={(e) => setMembership(e.target.value as any)}>
                <option value="include">include</option>
                <option value="exclude">exclude</option>
              </select>
            </label>
            <button disabled={!apiKey}>Set Member</button>
          </form>
        </article>
      )}

      <article className="card">
        <div className="section-title">
          <div>
            <div className="eyebrow">List</div>
            <h2>Segments</h2>
          </div>
          <button onClick={() => void refresh()}>Refresh</button>
        </div>
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Status</th>
              <th>Version</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map((seg) => (
              <tr key={seg.id}>
                <td><strong>{seg.name}</strong><br/><small className="muted">{seg.description || "No description"}</small></td>
                <td><span className="pill">{seg.type}</span></td>
                <td><span className={`pill ${seg.status}`}>{seg.status}</span></td>
                <td>{seg.version}</td>
                <td>{new Date(seg.updated_at).toLocaleString()}</td>
                <td>
                  <button onClick={() => startEdit(seg)}>Edit / Members</button>
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={6} className="muted text-center">No segments configured.</td>
              </tr>
            )}
          </tbody>
        </table>
      </article>
    </section>
  );
}

function ResourceTable({ headers, rows }: { headers: string[]; rows: (string | number)[][] }) {
  if (rows.length === 0) return <p className="muted">No records.</p>;
  return <table><thead><tr>{headers.map((header) => <th key={header}>{header}</th>)}</tr></thead>
    <tbody>{rows.map((row, index) => <tr key={index}>{row.map((value, cell) =>
      <td key={cell}>{value}</td>)}</tr>)}</tbody></table>;
}

function ErrorMessage({ value }: { value: string }) {
  return value ? <p className="error" role="alert">{value}</p> : null;
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : "";
}

function message(cause: unknown): string {
  return cause instanceof Error ? cause.message : "The operation failed";
}

// ─── Templates ───────────────────────────────────────────────────────────────

type TemplateEditorView = "list" | "new" | "edit";
type EmailComposer = { headline: string; message: string; buttonLabel: string; buttonURL: string; accentColor: string; backgroundColor: string };

const defaultEmailComposer: EmailComposer = {
  headline: "Welcome!",
  message: "Thanks for joining us. We’re glad you’re here.",
  buttonLabel: "Get started",
  buttonURL: "https://example.com",
  accentColor: "#6f5cff",
  backgroundColor: "#f4f6f8",
};

function escapeTemplateText(value: string): string {
  return value.replace(/&(?!#?\w+;)/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/\n/g, "<br>");
}

function buildComposerHTML(composer: EmailComposer): string {
  const button = composer.buttonLabel.trim() ? `<a data-oj-button href="${escapeTemplateText(composer.buttonURL)}" style="display:inline-block;padding:12px 20px;border-radius:8px;background:${composer.accentColor};color:#fff;text-decoration:none;font-weight:700">${escapeTemplateText(composer.buttonLabel)}</a>` : "";
  return `<div data-openjourney-builder="1" data-accent="${composer.accentColor}" data-background="${composer.backgroundColor}" style="margin:0;padding:32px 16px;background:${composer.backgroundColor};font-family:Arial,sans-serif;color:#1a2433"><div style="max-width:600px;margin:auto;padding:36px;background:#fff;border-radius:12px"><h1 data-oj-headline style="margin:0 0 16px;font-size:28px">${escapeTemplateText(composer.headline)}</h1><div data-oj-message style="margin:0 0 24px;line-height:1.65;color:#536071">${escapeTemplateText(composer.message)}</div>${button}</div></div>`;
}

function parseComposerHTML(html: string): EmailComposer | null {
  if (!html.includes("data-openjourney-builder")) return null;
  const document = new DOMParser().parseFromString(html, "text/html");
  const root = document.querySelector<HTMLElement>("[data-openjourney-builder]");
  if (!root) return null;
  const message = root.querySelector<HTMLElement>("[data-oj-message]")?.innerText.replace(/\n+/g, "\n").trim() || "";
  const button = root.querySelector<HTMLAnchorElement>("[data-oj-button]");
  return {
    headline: root.querySelector<HTMLElement>("[data-oj-headline]")?.textContent || "",
    message,
    buttonLabel: button?.textContent || "",
    buttonURL: button?.getAttribute("href") || "",
    accentColor: root.dataset.accent || "#6f5cff",
    backgroundColor: root.dataset.background || "#f4f6f8",
  };
}

function Templates({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<Template[]>([]);
  const [identities, setIdentities] = useState<SendingIdentity[]>([]);
  const [editorView, setEditorView] = useState<TemplateEditorView>("list");
  const [editing, setEditing] = useState<Partial<Template>>({});
  const [preview, setPreview] = useState<TemplatePreview | null>(null);
  const [previewProfileID, setPreviewProfileID] = useState("");
  const [previewLoading, setPreviewLoading] = useState(false);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [composerMode, setComposerMode] = useState<"visual" | "advanced">("visual");
  const [composer, setComposer] = useState<EmailComposer>(defaultEmailComposer);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const reload = async () => {
    try {
      const [ts, ids] = await Promise.all([
        listTemplates(apiBase, apiKey),
        listSendingIdentities(apiBase, apiKey),
      ]);
      setItems(ts);
      setIdentities(ids);
    } catch (e) { setError(message(e)); }
  };

  useEffect(() => { void reload(); }, [apiKey]);

  const startNew = () => {
    setComposer(defaultEmailComposer);
    setComposerMode("visual");
    setEditing({ name: "", channel: "email", subject_template: "Welcome, {{ profile.attributes.first_name | default: 'friend' }}!", html_template: buildComposerHTML(defaultEmailComposer) });
    setPreview(null);
    setEditorView("new");
  };

  const startEdit = async (id: string) => {
    try {
      const t = await getTemplate(apiBase, apiKey, id);
      const parsedComposer = parseComposerHTML(t.html_template || "");
      if (parsedComposer) setComposer(parsedComposer);
      setComposerMode(parsedComposer ? "visual" : "advanced");
      setEditing(t);
      setPreview(null);
      setEditorView("edit");
    } catch (e) { setError(message(e)); }
  };

  const handleSave = async (e: FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      if (editorView === "new") {
        await createTemplate(apiBase, apiKey, editing);
      } else if (editing.id) {
        await updateTemplate(apiBase, apiKey, editing.id, editing);
      }
      await reload();
      setEditorView("list");
    } catch (e) { setError(message(e)); }
    finally { setSaving(false); }
  };

  const handlePreview = async () => {
    if (!editing.id || !previewProfileID.trim()) return;
    setPreviewLoading(true);
    try {
      const p = await previewTemplate(apiBase, apiKey, editing.id, previewProfileID.trim());
      setPreview(p);
    } catch (e) { setError(message(e)); }
    finally { setPreviewLoading(false); }
  };

  const schedulePreview = (next: Partial<Template>) => {
    setEditing(next);
    if (!next.id || !previewProfileID.trim()) return;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(async () => {
      try {
        const p = await previewTemplate(apiBase, apiKey, next.id!, previewProfileID.trim());
        setPreview(p);
      } catch { /* silent */ }
    }, 700);
  };

  const updateComposer = (changes: Partial<EmailComposer>) => {
    const next = { ...composer, ...changes };
    setComposer(next);
    schedulePreview({ ...editing, html_template: buildComposerHTML(next) });
  };

  if (editorView === "list") {
    return (
      <section id="templates-section">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "1rem" }}>
          <h2>Templates</h2>
          <button id="new-template-btn" onClick={startNew}>+ New template</button>
        </div>
        {error && <p className="error">{error}</p>}
        {items.length === 0 ? (
          <p style={{ color: "var(--muted)" }}>No templates yet. Create one to get started.</p>
        ) : (
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                {["Name", "Channel", "Version", "Updated"].map(h => (
                  <th key={h} style={{ textAlign: "left", padding: "0.5rem 0.75rem", borderBottom: "1px solid var(--border)" }}>{h}</th>
                ))}
                <th />
              </tr>
            </thead>
            <tbody>
              {items.map(t => (
                <tr key={t.id} style={{ borderBottom: "1px solid var(--border)" }}>
                  <td style={{ padding: "0.5rem 0.75rem", fontWeight: 600 }}>{t.name}</td>
                  <td style={{ padding: "0.5rem 0.75rem" }}><span className="badge">{t.channel}</span></td>
                  <td style={{ padding: "0.5rem 0.75rem", color: "var(--muted)" }}>v{t.version}</td>
                  <td style={{ padding: "0.5rem 0.75rem", color: "var(--muted)", fontSize: "0.8rem" }}>{formatDate(t.updated_at)}</td>
                  <td style={{ padding: "0.5rem 0.75rem", textAlign: "right" }}>
                    <button id={`edit-template-${t.id}`} className="secondary" onClick={() => void startEdit(t.id)}>Edit</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    );
  }

  return (
    <section id="template-editor-section" className="template-editor-layout">
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: "0.75rem", marginBottom: "1rem" }}>
          <button className="secondary" id="back-to-templates-btn" onClick={() => setEditorView("list")}>← Back</button>
          <h2 style={{ margin: 0 }}>{editorView === "new" ? "New template" : `Edit: ${editing.name}`}</h2>
        </div>
        {error && <p className="error">{error}</p>}
        <form id="template-form" onSubmit={(e) => void handleSave(e)} style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
          <label>Name
            <input id="template-name" value={editing.name ?? ""} required
              onChange={e => schedulePreview({ ...editing, name: e.target.value })} />
          </label>
          <label>Channel
            <select id="template-channel" value={editing.channel ?? "email"}
              onChange={e => { const channel = e.target.value; if (channel === "webhook") setComposerMode("advanced"); schedulePreview({ ...editing, channel }); }}>
              <option value="email">Email</option>
              <option value="sms">SMS</option>
              <option value="push">Push</option>
              <option value="webhook">Webhook</option>
            </select>
          </label>
          {identities.length > 0 && (
            <label>Sending identity
              <select id="template-identity" value={editing.sending_identity_id ?? ""}
                onChange={e => schedulePreview({ ...editing, sending_identity_id: e.target.value || undefined })}>
                <option value="">— None —</option>
                {identities.map(id => (
                  <option key={id.id} value={id.id}>{id.display_name} &lt;{id.from_address}&gt;</option>
                ))}
              </select>
            </label>
          )}
          {editing.channel === "sms" && (
            <label>SMS body (Liquid)
              <textarea id="template-sms-body" rows={6} value={editing.text_template ?? ""}
                placeholder="Hi {{ profile.attributes.first_name | default: 'there' }}, your order is ready!"
                style={{ fontFamily: "monospace", fontSize: "0.85rem" }}
                onChange={e => schedulePreview({ ...editing, text_template: e.target.value })} />
              <button type="button" className="personalization-chip" onClick={() => schedulePreview({ ...editing, text_template: `${editing.text_template || ""} {{ profile.attributes.first_name }}`.trim() })}>+ First name</button>
            </label>
          )}
          {editing.channel === "push" && (<>
            <label>Push title
              <input id="template-push-title" value={editing.subject_template ?? ""} placeholder="New message for you"
                onChange={e => schedulePreview({ ...editing, subject_template: e.target.value })} />
              <button type="button" className="personalization-chip" onClick={() => schedulePreview({ ...editing, subject_template: `${editing.subject_template || ""} {{ profile.attributes.first_name }}`.trim() })}>+ First name</button>
            </label>
            <label>Push body
              <textarea id="template-push-body" rows={4} value={editing.text_template ?? ""}
                placeholder="Tap to see what&apos;s new"
                onChange={e => schedulePreview({ ...editing, text_template: e.target.value })} />
            </label>
            <label>Data payload (JSON key=value pairs, one per line)
              <textarea id="template-push-data" rows={4} value={editing.body_template ?? ""}
                placeholder={"action=view_order\norder_id=123"}
                style={{ fontFamily: "monospace", fontSize: "0.85rem" }}
                onChange={e => schedulePreview({ ...editing, body_template: e.target.value })} />
            </label>
          </>)}
          {editing.channel !== "webhook" && editing.channel !== "sms" && editing.channel !== "push" && <>
            <div className="composer-mode-tabs" role="tablist" aria-label="Template editor mode">
              <button type="button" role="tab" aria-selected={composerMode === "visual"} className={composerMode === "visual" ? "active" : ""} onClick={() => {
                if (composerMode === "advanced" && !parseComposerHTML(editing.html_template || "") && !window.confirm("Switching to the visual composer will replace the current custom HTML. Continue?")) return;
                setComposerMode("visual"); updateComposer({});
              }}>Visual composer</button>
              <button type="button" role="tab" aria-selected={composerMode === "advanced"} className={composerMode === "advanced" ? "active" : ""} onClick={() => setComposerMode("advanced")}>Advanced HTML</button>
            </div>
            <label>Email subject
              <input id="template-subject" value={editing.subject_template ?? ""} placeholder="A warm welcome from our team"
                onChange={e => schedulePreview({ ...editing, subject_template: e.target.value })} />
              <button type="button" className="personalization-chip" onClick={() => schedulePreview({ ...editing, subject_template: `${editing.subject_template || ""} {{ profile.attributes.first_name }}`.trim() })}>+ First name</button>
            </label>
          </>}

          {editing.channel === "email" && composerMode === "visual" ? <div className="email-composer-fields">
            <label>Headline<input value={composer.headline} onChange={e => updateComposer({ headline: e.target.value })} placeholder="Welcome to our community" /></label>
            <label>Message<textarea rows={6} value={composer.message} onChange={e => updateComposer({ message: e.target.value })} placeholder="Write your message in plain language…" /></label>
            <div className="composer-inline-fields"><label>Button label<input value={composer.buttonLabel} onChange={e => updateComposer({ buttonLabel: e.target.value })} placeholder="Get started" /></label><label>Button link<input type="url" value={composer.buttonURL} onChange={e => updateComposer({ buttonURL: e.target.value })} placeholder="https://example.com" /></label></div>
            <div className="composer-inline-fields"><label>Button color<input type="color" value={composer.accentColor} onChange={e => updateComposer({ accentColor: e.target.value })} /></label><label>Background<input type="color" value={composer.backgroundColor} onChange={e => updateComposer({ backgroundColor: e.target.value })} /></label></div>
            <div className="personalization-row"><span>Personalize message:</span>{[["First name", "{{ profile.attributes.first_name }}"], ["Email", "{{ profile.attributes.email }}"], ["Customer ID", "{{ profile.external_id }}"]].map(([label, token]) => <button type="button" className="personalization-chip" key={label} onClick={() => updateComposer({ message: `${composer.message} ${token}`.trim() })}>+ {label}</button>)}</div>
          </div> : <label>{editing.channel === "webhook" ? "Webhook body (JSON)" : "HTML and Liquid"}
            <textarea id="template-body" rows={16} value={(editing.channel === "webhook" ? editing.body_template : editing.html_template) ?? ""}
              placeholder={editing.channel === "webhook" ? '{ "user_id": "{{ profile.external_id }}" }' : "<p>Your HTML email…</p>"}
              style={{ fontFamily: "monospace", fontSize: "0.85rem" }} onChange={e => { const key = editing.channel === "webhook" ? "body_template" : "html_template"; schedulePreview({ ...editing, [key]: e.target.value }); }} />
          </label>}
          <button id="save-template-btn" type="submit" disabled={saving}>
            {saving ? "Saving…" : "Save template"}
          </button>
        </form>
      </div>

      <div>
        <h3 style={{ marginTop: 0, marginBottom: "0.25rem" }}>Email preview</h3>
        <p className="muted" style={{ fontSize: "0.8rem", marginTop: 0 }}>This updates while you type. Personalization tokens appear after testing with a saved customer.</p>
        {editing.id && <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.75rem" }}>
          <input id="preview-profile-id" placeholder="Profile external_id"
            value={previewProfileID} onChange={e => setPreviewProfileID(e.target.value)}
            style={{ flex: 1 }} />
          <button id="preview-btn" className="secondary"
            onClick={() => void handlePreview()}
            disabled={!editing.id || previewLoading || !previewProfileID.trim()}>
            {previewLoading ? "…" : "Test personalization"}
          </button>
        </div>}
        {(editing.channel === "email" || preview) && (
          <div className="email-preview-frame">
            <div style={{ padding: "0.5rem 1rem", background: "rgba(255,255,255,0.05)", borderBottom: "1px solid var(--border)" }}>
              <span style={{ fontSize: "0.75rem", color: "var(--muted)" }}>Subject: </span>
              <strong style={{ fontSize: "0.9rem" }}>{preview?.subject || editing.subject_template || "Your email subject"}</strong>
            </div>
            <iframe id="preview-iframe" title="Template preview" sandbox="allow-same-origin"
              srcDoc={preview?.body || editing.html_template || "<p>Start writing to preview your email.</p>"}
              style={{ width: "100%", height: "480px", border: "none", background: "#fff" }} />
          </div>
        )}
      </div>
    </section>
  );
}

function Suppressions({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<Suppression[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [channel, setChannel] = useState("email");
  const [endpoint, setEndpoint] = useState("");
  const [reason, setReason] = useState("admin");
  const [saving, setSaving] = useState(false);

  async function load() {
    setLoading(true); setError("");
    try {
      setItems((await listSuppressions(apiBase, apiKey)) ?? []);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { if (apiKey) void load(); }, [apiKey]);

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    setSaving(true); setError("");
    try {
      await createSuppression(apiBase, apiKey, { channel, endpoint, reason: reason as any });
      setEndpoint("");
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete(item: Suppression) {
    if (!confirm(`Are you sure you want to remove suppression for ${item.endpoint}?`)) return;
    setError("");
    try {
      await deleteSuppression(apiBase, apiKey, item.channel, item.endpoint);
      await load();
    } catch (cause) {
      setError(message(cause));
    }
  }

  return (
    <section className="card">
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "2rem" }}>
        <div>
          <h2>Add suppression</h2>
          <form onSubmit={handleCreate} className="panel">
            <label>Channel
              <select value={channel} onChange={e => setChannel(e.target.value)}>
                <option value="email">Email</option>
                <option value="webhook">Webhook</option>
              </select>
            </label>
            <label>Endpoint (Email address or Webhook target)
              <input value={endpoint} onChange={e => setEndpoint(e.target.value)} required placeholder="user@example.com" />
            </label>
            <label>Reason
              <select value={reason} onChange={e => setReason(e.target.value)}>
                <option value="admin">Admin override</option>
                <option value="unsubscribe">Unsubscribed</option>
                <option value="bounce">Bounced</option>
                <option value="complaint">Complaint reported</option>
              </select>
            </label>
            <button type="submit" disabled={saving || !endpoint.trim()}>{saving ? "Saving…" : "Suppress endpoint"}</button>
          </form>
          <ErrorMessage value={error} />
        </div>
        <div>
          <h2>Suppressed endpoints ({items.length})</h2>
          {loading && <p>Loading suppressions…</p>}
          {!loading && items.length === 0 && <p style={{ color: "var(--muted)" }}>No suppressed endpoints found.</p>}
          <ul className="list">
            {items.map(item => (
              <li key={item.id} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "0.5rem 0", borderBottom: "1px solid var(--border)" }}>
                <div>
                  <strong>{item.endpoint}</strong> <span style={{ color: "var(--muted)", fontSize: "0.8rem" }}>({item.channel} • {item.reason})</span>
                  <div style={{ fontSize: "0.75rem", color: "var(--muted)" }}>Suppressed {new Date(item.created_at).toLocaleString()}</div>
                </div>
                <button className="secondary danger small" onClick={() => void handleDelete(item)}>Remove</button>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

function SenderIdentities({ apiKey }: { apiKey: string }) {
  const [items, setItems] = useState<SendingIdentity[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [channel, setChannel] = useState("email");
  const [provider, setProvider] = useState("ses");
  const [fromAddress, setFromAddress] = useState("");
  const [fromName, setFromName] = useState("");
  const [replyTo, setReplyTo] = useState("");
  // SMS / Twilio
  const [twilioAccountSid, setTwilioAccountSid] = useState("");
  const [twilioAuthToken, setTwilioAuthToken] = useState("");
  const [twilioFromNumber, setTwilioFromNumber] = useState("");
  // Push / FCM
  const [fcmProjectId, setFcmProjectId] = useState("");
  const [fcmToken, setFcmToken] = useState("");
  // Push / APNs
  const [apnsPrivateKey, setApnsPrivateKey] = useState("");
  const [apnsKeyId, setApnsKeyId] = useState("");
  const [apnsTeamId, setApnsTeamId] = useState("");
  const [apnsTopic, setApnsTopic] = useState("");
  const [saving, setSaving] = useState(false);

  async function load() {
    setLoading(true); setError("");
    try {
      setItems(await listSendingIdentities(apiBase, apiKey));
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { if (apiKey) void load(); }, [apiKey]);

  function buildConfig(): Record<string, string> | undefined {
    if (channel === "sms" && provider === "twilio") {
      return { account_sid: twilioAccountSid, auth_token: twilioAuthToken, from_number: twilioFromNumber };
    }
    if (channel === "push" && provider === "fcm") {
      return { project_id: fcmProjectId, token: fcmToken };
    }
    if (channel === "push" && provider === "apns") {
      return { private_key: apnsPrivateKey, key_id: apnsKeyId, team_id: apnsTeamId, topic: apnsTopic };
    }
    return undefined;
  }

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    setSaving(true); setError("");
    try {
      await createSendingIdentity(apiBase, apiKey, {
        channel,
        provider: channel === "sms" || channel === "push" ? provider : undefined,
        from_address: fromAddress,
        display_name: fromName,
        reply_to: channel === "email" ? replyTo : undefined,
        config: buildConfig(),
      });
      setFromAddress(""); setFromName(""); setReplyTo("");
      setTwilioAccountSid(""); setTwilioAuthToken(""); setTwilioFromNumber("");
      setFcmProjectId(""); setFcmToken("");
      setApnsPrivateKey(""); setApnsKeyId(""); setApnsTeamId(""); setApnsTopic("");
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="card">
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "2rem" }}>
        <div>
          <h2>Add sender identity</h2>
          <form onSubmit={handleCreate} className="panel">
            <label>Channel
              <select id="identity-channel" value={channel} onChange={e => { setChannel(e.target.value); setProvider(e.target.value === "sms" ? "twilio" : "fcm"); }}>
                <option value="email">Email</option>
                <option value="sms">SMS</option>
                <option value="push">Push</option>
                <option value="webhook">Webhook</option>
              </select>
            </label>

            {channel === "sms" && (
              <label>Provider
                <select id="identity-sms-provider" value={provider} onChange={e => setProvider(e.target.value)}>
                  <option value="twilio">Twilio</option>
                </select>
              </label>
            )}
            {channel === "push" && (
              <label>Provider
                <select id="identity-push-provider" value={provider} onChange={e => setProvider(e.target.value)}>
                  <option value="fcm">FCM (Firebase)</option>
                  <option value="apns">APNs (Apple)</option>
                </select>
              </label>
            )}

            <label>Display name
              <input id="identity-display-name" value={fromName} onChange={e => setFromName(e.target.value)} placeholder={channel === "sms" ? "SMS channel" : channel === "push" ? "Push channel" : "Marketing Team"} />
            </label>

            {(channel === "email" || channel === "webhook") && (
              <label>{channel === "email" ? "From address" : "Webhook URL"}
                <input id="identity-from-address" value={fromAddress} onChange={e => setFromAddress(e.target.value)} required
                  placeholder={channel === "email" ? "no-reply@example.com" : "https://example.com/hook"} />
              </label>
            )}
            {channel === "email" && (
              <label>Reply-to address
                <input value={replyTo} onChange={e => setReplyTo(e.target.value)} placeholder="support@example.com" />
              </label>
            )}

            {channel === "sms" && provider === "twilio" && (<>
              <label>Twilio Account SID
                <input id="identity-twilio-sid" value={twilioAccountSid} onChange={e => setTwilioAccountSid(e.target.value)} required placeholder="ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" />
              </label>
              <label>Twilio Auth Token
                <input id="identity-twilio-token" type="password" value={twilioAuthToken} onChange={e => setTwilioAuthToken(e.target.value)} required placeholder="••••••••••••••••••••••••••••••••" />
              </label>
              <label>From number
                <input id="identity-twilio-from" value={twilioFromNumber} onChange={e => setTwilioFromNumber(e.target.value)} required placeholder="+15005550006" />
              </label>
            </>)}

            {channel === "push" && provider === "fcm" && (<>
              <label>FCM Project ID
                <input id="identity-fcm-project" value={fcmProjectId} onChange={e => setFcmProjectId(e.target.value)} required placeholder="my-firebase-project" />
              </label>
              <label>FCM Bearer Token
                <input id="identity-fcm-token" type="password" value={fcmToken} onChange={e => setFcmToken(e.target.value)} required placeholder="ya29.…" />
              </label>
            </>)}

            {channel === "push" && provider === "apns" && (<>
              <label>APNs Private Key (PEM)
                <textarea id="identity-apns-key" rows={5} value={apnsPrivateKey} onChange={e => setApnsPrivateKey(e.target.value)} required
                  style={{ fontFamily: "monospace", fontSize: "0.8rem" }}
                  placeholder="-----BEGIN PRIVATE KEY-----\n…\n-----END PRIVATE KEY-----" />
              </label>
              <label>Key ID
                <input id="identity-apns-kid" value={apnsKeyId} onChange={e => setApnsKeyId(e.target.value)} required placeholder="ABCDE12345" />
              </label>
              <label>Team ID
                <input id="identity-apns-team" value={apnsTeamId} onChange={e => setApnsTeamId(e.target.value)} required placeholder="FGHIJ67890" />
              </label>
              <label>Bundle ID (topic)
                <input id="identity-apns-topic" value={apnsTopic} onChange={e => setApnsTopic(e.target.value)} required placeholder="com.example.app" />
              </label>
            </>)}

            <button id="save-identity-btn" type="submit" disabled={saving || (!fromAddress.trim() && channel !== "sms" && channel !== "push")}>
              {saving ? "Saving…" : "Save identity"}
            </button>
          </form>
          <ErrorMessage value={error} />
        </div>
        <div>
          <h2>Sender identities ({items.length})</h2>
          {loading && <p>Loading identities…</p>}
          {!loading && items.length === 0 && <p style={{ color: "var(--muted)" }}>No sender identities found.</p>}
          <ul className="list">
            {items.map(item => (
              <li key={item.id} style={{ padding: "0.5rem 0", borderBottom: "1px solid var(--border)" }}>
                <div>
                  <strong>{item.display_name || item.from_address || item.id}</strong>
                  <span style={{ color: "var(--muted)", fontSize: "0.8rem", marginLeft: "0.4rem" }}>
                    ({item.channel}{item.provider ? ` / ${item.provider}` : ""})
                  </span>
                  {item.from_address && <div style={{ fontSize: "0.85rem" }}>{item.from_address}</div>}
                  {item.reply_to && <div style={{ fontSize: "0.8rem", color: "var(--muted)" }}>Reply-to: {item.reply_to}</div>}
                </div>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

function DeviceTokensInspector({ apiKey }: { apiKey: string }) {
  const [profileId, setProfileId] = useState("");
  const [tokens, setTokens] = useState<DeviceToken[]>([]);
  const [loading, setLoading] = useState(false);
  const [retiring, setRetiring] = useState<string | null>(null);
  const [error, setError] = useState("");

  async function handleSearch(event: FormEvent) {
    event.preventDefault();
    if (!profileId.trim()) return;
    setLoading(true); setError(""); setTokens([]);
    try {
      setTokens(await listDeviceTokens(apiBase, apiKey, profileId.trim()));
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoading(false);
    }
  }

  async function handleRetire(id: string) {
    if (!window.confirm("Retire this device token? It will no longer receive push notifications.")) return;
    setRetiring(id); setError("");
    try {
      await retireDeviceToken(apiBase, apiKey, id);
      setTokens(prev => prev.filter(t => t.id !== id));
    } catch (cause) {
      setError(message(cause));
    } finally {
      setRetiring(null);
    }
  }

  return (
    <section className="card">
      <h2>Device token inspector</h2>
      <form onSubmit={handleSearch} style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem" }}>
        <input id="device-token-profile-id" value={profileId} onChange={e => setProfileId(e.target.value)}
          placeholder="Profile external_id" style={{ flex: 1 }} />
        <button id="device-token-search-btn" type="submit" disabled={loading || !profileId.trim()}>
          {loading ? "Searching…" : "Search"}
        </button>
      </form>
      <ErrorMessage value={error} />
      {!loading && tokens.length === 0 && profileId && <p style={{ color: "var(--muted)" }}>No active device tokens found for this profile.</p>}
      {tokens.length > 0 && (
        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.85rem" }}>
          <thead>
            <tr style={{ borderBottom: "1px solid var(--border)", textAlign: "left" }}>
              <th style={{ padding: "0.4rem 0.6rem" }}>Platform</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Provider</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Token (truncated)</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Status</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Action</th>
            </tr>
          </thead>
          <tbody>
            {tokens.map(tok => (
              <tr key={tok.id} style={{ borderBottom: "1px solid var(--border)" }}>
                <td style={{ padding: "0.4rem 0.6rem" }}>{tok.platform}</td>
                <td style={{ padding: "0.4rem 0.6rem" }}>{tok.provider}</td>
                <td style={{ padding: "0.4rem 0.6rem", fontFamily: "monospace" }}>{tok.token.slice(0, 24)}…</td>
                <td style={{ padding: "0.4rem 0.6rem" }}>
                  <span style={{ color: tok.active ? "var(--accent)" : "var(--muted)" }}>
                    {tok.active ? "Active" : "Retired"}
                  </span>
                </td>
                <td style={{ padding: "0.4rem 0.6rem" }}>
                  {tok.active && (
                    <button id={`retire-token-${tok.id}`} className="secondary small"
                      disabled={retiring === tok.id}
                      onClick={() => void handleRetire(tok.id)}>
                      {retiring === tok.id ? "Retiring…" : "Retire"}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

export function Campaigns({ apiKey }: { apiKey: string }) {
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [segments, setSegments] = useState<Segment[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [editingCampaign, setEditingCampaign] = useState<Campaign | null>(null);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [segmentId, setSegmentId] = useState("");
  const [templateId, setTemplateId] = useState("");
  const [status, setStatus] = useState<Campaign["status"]>("draft");
  const [scheduledAt, setScheduledAt] = useState("");

  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [cRes, sRes, tRes] = await Promise.all([
        listCampaigns(apiBase, apiKey),
        listSegments(apiBase, apiKey),
        listTemplates(apiBase, apiKey),
      ]);
      setCampaigns(cRes);
      setSegments(sRes);
      setTemplates(tRes);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (apiKey) void load();
  }, [apiKey]);

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      if (!segmentId) throw new Error("Segment is required");
      if (!templateId) throw new Error("Template is required");

      const payload: Partial<Campaign> = {
        name,
        description: description || undefined,
        segment_id: segmentId,
        template_id: templateId,
        status,
        scheduled_at: status === "scheduled" ? (scheduledAt ? new Date(scheduledAt).toISOString() : new Date().toISOString()) : undefined,
      };

      await createCampaign(apiBase, apiKey, payload);
      resetForm();
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  }

  async function handleUpdate(event: FormEvent) {
    event.preventDefault();
    if (!editingCampaign) return;
    setSaving(true);
    setError("");
    try {
      if (!segmentId) throw new Error("Segment is required");
      if (!templateId) throw new Error("Template is required");

      const payload: Partial<Campaign> = {
        name,
        description: description || undefined,
        segment_id: segmentId,
        template_id: templateId,
        status,
        scheduled_at: status === "scheduled" ? (scheduledAt ? new Date(scheduledAt).toISOString() : new Date().toISOString()) : null as any,
      };

      await updateCampaign(apiBase, apiKey, editingCampaign.id, payload);
      resetForm();
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setSaving(false);
    }
  }

  async function handleLaunchNow(campaign: Campaign) {
    setError("");
    try {
      await updateCampaign(apiBase, apiKey, campaign.id, {
        ...campaign,
        status: "scheduled",
        scheduled_at: new Date().toISOString(),
      });
      await load();
    } catch (cause) {
      setError(message(cause));
    }
  }

  function startEdit(c: Campaign) {
    setEditingCampaign(c);
    setName(c.name);
    setDescription(c.description || "");
    setSegmentId(c.segment_id);
    setTemplateId(c.template_id);
    setStatus(c.status);
    if (c.scheduled_at) {
      const d = new Date(c.scheduled_at);
      const year = d.getFullYear();
      const month = String(d.getMonth() + 1).padStart(2, "0");
      const day = String(d.getDate()).padStart(2, "0");
      const hours = String(d.getHours()).padStart(2, "0");
      const minutes = String(d.getMinutes()).padStart(2, "0");
      setScheduledAt(`${year}-${month}-${day}T${hours}:${minutes}`);
    } else {
      setScheduledAt("");
    }
  }

  function resetForm() {
    setEditingCampaign(null);
    setName("");
    setDescription("");
    setSegmentId("");
    setTemplateId("");
    setStatus("draft");
    setScheduledAt("");
  }

  const getSegmentName = (id: string) => segments.find(s => s.id === id)?.name || id;
  const getTemplateName = (id: string) => templates.find(t => t.id === id)?.name || id;

  const getStatusStyle = (s: Campaign["status"]) => {
    switch (s) {
      case "completed":
        return { background: "#e9f8f1", color: "#187d56" };
      case "sending":
      case "building":
        return { background: "#e8f0fe", color: "#1a73e8" };
      case "scheduled":
        return { background: "#fff8df", color: "#b06000" };
      case "paused":
        return { background: "#f1f3f4", color: "#5f6368" };
      case "failed":
        return { background: "#fff0f0", color: "#a93838" };
      default: // draft
        return { background: "#f8f9fa", color: "#202124", border: "1px solid #dadce0" };
    }
  };

  return (
    <section className="stack">
      <div style={{ display: "grid", gridTemplateColumns: "1fr 2fr", gap: "2rem" }}>
        <article className="card" style={{ height: "fit-content" }}>
          <h2>{editingCampaign ? "Edit Campaign" : "Create Campaign"}</h2>
          <form onSubmit={editingCampaign ? handleUpdate : handleCreate} className="schema-form" style={{ gridTemplateColumns: "1fr" }}>
            <label>Name
              <input value={name} onChange={e => setName(e.target.value)} required placeholder="Summer Discount Promo" />
            </label>
            <label>Description
              <input value={description} onChange={e => setDescription(e.target.value)} placeholder="Send discount to SaaS users" />
            </label>
            <label>Segment
              <select value={segmentId} onChange={e => setSegmentId(e.target.value)} required>
                <option value="">-- Select Segment --</option>
                {segments.map(s => (
                  <option key={s.id} value={s.id}>{s.name} ({s.type})</option>
                ))}
              </select>
            </label>
            <label>Template
              <select value={templateId} onChange={e => setTemplateId(e.target.value)} required>
                <option value="">-- Select Template --</option>
                {templates.map(t => (
                  <option key={t.id} value={t.id}>{t.name} ({t.channel})</option>
                ))}
              </select>
            </label>
            <label>Status
              <select value={status} onChange={e => setStatus(e.target.value as any)}>
                <option value="draft">Draft</option>
                <option value="scheduled">Scheduled</option>
                <option value="paused">Paused</option>
                <option value="archived">Archived</option>
              </select>
            </label>
            {status === "scheduled" && (
              <label>Schedule Time (Local)
                <input type="datetime-local" value={scheduledAt} onChange={e => setScheduledAt(e.target.value)} />
                <span style={{ fontSize: "11px", color: "var(--muted)", marginTop: "2px" }}>Leave blank to run immediately upon scheduling.</span>
              </label>
            )}
            <div className="form-actions" style={{ display: "flex", gap: "8px", marginTop: "12px" }}>
              <button type="submit" disabled={saving || !segmentId || !templateId || !name}>{saving ? "Saving..." : (editingCampaign ? "Update Campaign" : "Create Campaign")}</button>
              {(editingCampaign || name || segmentId || templateId) && (
                <button type="button" className="secondary" onClick={resetForm}>Cancel</button>
              )}
            </div>
          </form>
          <ErrorMessage value={error} />
        </article>

        <article className="card">
          <h2>Campaigns ({campaigns.length})</h2>
          {loading && <p>Loading campaigns…</p>}
          {!loading && campaigns.length === 0 && <p style={{ color: "var(--muted)" }}>No campaigns found.</p>}
          {!loading && campaigns.length > 0 && (
            <div style={{ overflowX: "auto" }}>
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Segment</th>
                    <th>Template</th>
                    <th>Status</th>
                    <th>Scheduled</th>
                    <th>Recipients</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {campaigns.map(c => (
                    <tr key={c.id}>
                      <td>
                        <strong>{c.name}</strong>
                        {c.description && <div style={{ fontSize: "11px", color: "var(--muted)" }}>{c.description}</div>}
                      </td>
                      <td>{getSegmentName(c.segment_id)}</td>
                      <td>{getTemplateName(c.template_id)}</td>
                      <td>
                        <span className="pill" style={getStatusStyle(c.status)}>
                          {c.status}
                        </span>
                      </td>
                      <td>{c.scheduled_at ? formatDate(c.scheduled_at) : "Immediate"}</td>
                      <td>{c.status === "draft" ? "Pending" : c.recipient_count}</td>
                      <td>
                        <div style={{ display: "flex", gap: "6px" }}>
                          <button className="secondary" style={{ padding: "4px 8px", fontSize: "12px" }} onClick={() => startEdit(c)}>Edit</button>
                          <a className="report-link" href={`#reports?type=campaign&id=${encodeURIComponent(c.id)}`}>Report</a>
                          {c.status === "draft" && (
                            <button style={{ padding: "4px 8px", fontSize: "12px", background: "#48bd8b", color: "white" }} onClick={() => handleLaunchNow(c)}>Launch</button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </article>
      </div>
    </section>
  );
}

// Journeys component imported from sections/Journeys

function ScopeSelector({ selected, onChange }: { selected: string[]; onChange: (scopes: string[]) => void }) {
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const toggleScope = (scope: string) => {
    if (selected.includes(scope)) {
      onChange(selected.filter((s) => s !== scope));
    } else {
      onChange([...selected, scope]);
    }
  };

  return (
    <div className="scope-selector" ref={dropdownRef}>
      <button
        type="button"
        className="scope-selector-btn"
        onClick={() => setOpen(!open)}
      >
        {selected.length === 0 ? "Select scopes..." : selected.join(", ")}
      </button>
      {open && (
        <div className="scope-selector-dropdown">
          {AVAILABLE_SCOPES.map((scope) => (
            <label key={scope} htmlFor={`scope-${scope}`} className="scope-option">
              <input
                id={`scope-${scope}`}
                type="checkbox"
                checked={selected.includes(scope)}
                onChange={() => toggleScope(scope)}
              />
              <code>{scope}</code>
            </label>
          ))}
        </div>
      )}
    </div>
  );
}
