import { FormEvent, lazy, Suspense, useEffect, useRef, useState } from "react";
import {
  checkHealth, Consent, createAPIKey, createPrivacyRequest, createRole, createSchema, createUser,
  discardDeadLetter, getPrivacyRequest, getProfile, getQueueStatus, listAPIKeys, listAuditEvents,
  listDeadLetters, listRoles, listSchemas, listUsers, login, logout, Profile, replayVerify, retryDeadLetter, revokeAPIKey,
  APIKey, AuditEvent, DeadLetterItem, EventSchema, PrivacyRequest, QueueStatus, ReplayReport, Role, User,
  createSegment, listSegments, updateSegment, setSegmentMembers, Segment, SegmentMember,
  listTemplates, getTemplate, createTemplate, updateTemplate, previewTemplate,
  listSendingIdentities, createSendingIdentity, Template, SendingIdentity, TemplatePreview,
  listSuppressions, createSuppression, deleteSuppression, Suppression,
  listCampaigns, getCampaign, createCampaign, updateCampaign, Campaign,
  listJourneys, createJourney, Journey,
} from "./api";
import { oidcConfigured, restoreOIDCSession, signIn, signOut } from "./auth";

const Journeys = lazy(() => import("./sections/Journeys"));

const apiBase = import.meta.env.VITE_API_BASE_URL || "/api";
type View = "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments" | "templates" | "campaigns" | "journeys" | "suppressions" | "sender-identities";
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
  templates: ["Templates", "Design email templates with Liquid tags and live preview."],
  campaigns: ["Campaigns", "Schedule and manage sharded marketing campaigns linked to segments and templates."],
  journeys: ["Journeys", "Design, publish, and monitor automated customer experiences."],
  suppressions: ["Suppressions", "Manage bounces, complaints, and manually suppressed endpoints."],
  "sender-identities": ["Sender Identities", "Manage verified sender emails and webhook channels."],
};

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
];

export function App() {
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [view, setView] = useState<View>(() => {
    const hash = window.location.hash.slice(1) as View;
    return (hash in viewTitles) ? hash : "profiles";
  });
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
    if (apiKey) {
      window.location.hash = view;
    }
  }, [view, apiKey]);

  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash.slice(1) as View;
      if (hash in viewTitles) {
        setView(hash);
      }
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
          {(["profiles", "segments", "templates", "campaigns", "journeys", "suppressions", "sender-identities", "schemas", "api-keys", "privacy", "access", "operations", "audit"] as View[]).map((item) => (
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
        {view === "profiles" && <Profiles apiKey={apiKey} />}
        {view === "segments" && <Segments apiKey={apiKey} />}
        {view === "templates" && <Templates apiKey={apiKey} />}
        {view === "campaigns" && <Campaigns apiKey={apiKey} />}
        {view === "journeys" && (
          <Suspense fallback={<p role="status">Loading journey builder…</p>}>
            <Journeys apiKey={apiKey} />
          </Suspense>
        )}
        {view === "suppressions" && <Suppressions apiKey={apiKey} />}
        {view === "sender-identities" && <SenderIdentities apiKey={apiKey} />}
        {view === "schemas" && <Schemas apiKey={apiKey} />}
        {view === "api-keys" && <APIKeys apiKey={apiKey} />}
        {view === "privacy" && <Privacy apiKey={apiKey} />}
        {view === "access" && <Access apiKey={apiKey} />}
        {view === "operations" && <Operations apiKey={apiKey} />}
        {view === "audit" && <Audit apiKey={apiKey} />}
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
      setProfile(result.profile); setConsents(result.consents);
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

  async function refresh() {
    try {
      setItems(await listSegments(apiBase, apiKey));
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
    setEditing({ name: "", channel: "email", subject_template: "", html_template: "" });
    setPreview(null);
    setEditorView("new");
  };

  const startEdit = async (id: string) => {
    try {
      const t = await getTemplate(apiBase, apiKey, id);
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
    <section id="template-editor-section" style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "1.5rem", alignItems: "start" }}>
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
              onChange={e => schedulePreview({ ...editing, channel: e.target.value })}>
              <option value="email">Email</option>
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
          {editing.channel !== "webhook" && (
            <label>Subject (Liquid)
              <input id="template-subject" value={editing.subject_template ?? ""}
                placeholder="Hello {{ profile.attributes.name }}!"
                onChange={e => schedulePreview({ ...editing, subject_template: e.target.value })} />
            </label>
          )}
          <label>
            {editing.channel === "webhook" ? "Body (Liquid JSON)" : "HTML body (Liquid)"}
            <textarea id="template-body" rows={14}
              value={(editing.channel === "webhook" ? editing.body_template : editing.html_template) ?? ""}
              placeholder={editing.channel === "webhook"
                ? '{ "user_id": "{{ profile.external_id }}" }'
                : "<p>Hello <strong>{{ profile.attributes.name }}</strong>!</p>"}
              style={{ fontFamily: "monospace", fontSize: "0.85rem" }}
              onChange={e => {
                const key = editing.channel === "webhook" ? "body_template" : "html_template";
                schedulePreview({ ...editing, [key]: e.target.value });
              }} />
          </label>
          <button id="save-template-btn" type="submit" disabled={saving}>
            {saving ? "Saving…" : "Save template"}
          </button>
        </form>
      </div>

      <div>
        <h3 style={{ marginTop: 0, marginBottom: "0.75rem" }}>Live preview</h3>
        <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.75rem" }}>
          <input id="preview-profile-id" placeholder="Profile external_id"
            value={previewProfileID} onChange={e => setPreviewProfileID(e.target.value)}
            style={{ flex: 1 }} />
          <button id="preview-btn" className="secondary"
            onClick={() => void handlePreview()}
            disabled={!editing.id || previewLoading || !previewProfileID.trim()}>
            {previewLoading ? "…" : "Preview"}
          </button>
        </div>
        {!editing.id && (
          <p style={{ color: "var(--muted)", fontSize: "0.85rem" }}>Save the template first to enable live preview.</p>
        )}
        {preview && (
          <div style={{ border: "1px solid var(--border)", borderRadius: "8px", overflow: "hidden" }}>
            <div style={{ padding: "0.5rem 1rem", background: "rgba(255,255,255,0.05)", borderBottom: "1px solid var(--border)" }}>
              <span style={{ fontSize: "0.75rem", color: "var(--muted)" }}>Subject: </span>
              <strong style={{ fontSize: "0.9rem" }}>{preview.subject}</strong>
            </div>
            <iframe id="preview-iframe" title="Template preview" sandbox="allow-same-origin"
              srcDoc={preview.body}
              style={{ width: "100%", height: "480px", border: "none", background: "#fff" }} />
          </div>
        )}
        {!preview && editing.id && (
          <div style={{
            border: "1px dashed var(--border)", borderRadius: "8px", padding: "3rem 1rem",
            textAlign: "center", color: "var(--muted)"
          }}>
            Enter a profile external_id and click Preview to render the template.
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
  const [fromAddress, setFromAddress] = useState("");
  const [fromName, setFromName] = useState("");
  const [replyTo, setReplyTo] = useState("");
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

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    setSaving(true); setError("");
    try {
      await createSendingIdentity(apiBase, apiKey, { channel, from_address: fromAddress, display_name: fromName, reply_to: replyTo });
      setFromAddress("");
      setFromName("");
      setReplyTo("");
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
              <select value={channel} onChange={e => setChannel(e.target.value)}>
                <option value="email">Email</option>
                <option value="webhook">Webhook</option>
              </select>
            </label>
            <label>Sender email address (or webhook URL)
              <input value={fromAddress} onChange={e => setFromAddress(e.target.value)} required placeholder="no-reply@example.com" />
            </label>
            <label>Sender display name (or webhook name)
              <input value={fromName} onChange={e => setFromName(e.target.value)} placeholder="Marketing Team" />
            </label>
            {channel === "email" && (
              <label>Reply-to address
                <input value={replyTo} onChange={e => setReplyTo(e.target.value)} placeholder="support@example.com" />
              </label>
            )}
            <button type="submit" disabled={saving || !fromAddress.trim()}>{saving ? "Saving…" : "Save identity"}</button>
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
                  <strong>{item.display_name || item.from_address}</strong> <span style={{ color: "var(--muted)", fontSize: "0.8rem" }}>({item.channel})</span>
                  {item.display_name && <div style={{ fontSize: "0.85rem" }}>{item.from_address}</div>}
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
