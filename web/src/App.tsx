import { FormEvent, useEffect, useState } from "react";
import {
  checkHealth, Consent, createAPIKey, createPrivacyRequest, createRole, createSchema, createUser,
  discardDeadLetter, getPrivacyRequest, getProfile, getQueueStatus, listAPIKeys, listAuditEvents,
  listDeadLetters, listRoles, listSchemas, listUsers, login, logout, Profile, replayVerify, retryDeadLetter, revokeAPIKey,
  APIKey, AuditEvent, DeadLetterItem, EventSchema, PrivacyRequest, QueueStatus, ReplayReport, Role, User,
  createSegment, listSegments, updateSegment, setSegmentMembers, Segment, SegmentMember,
} from "./api";
import { oidcConfigured, restoreOIDCSession, signIn, signOut } from "./auth";

const apiBase = import.meta.env.VITE_API_BASE_URL || "/api";
type View = "profiles" | "schemas" | "api-keys" | "privacy" | "access" | "operations" | "audit" | "segments";
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
};

export function App() {
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [view, setView] = useState<View>("profiles");
  const [apiKey, setAPIKey] = useState(() => sessionStorage.getItem("oj_session_token") || localStorage.getItem("oj_api_key") || "");
  const [credentialSource, setCredentialSource] = useState<CredentialSource>(() =>
    sessionStorage.getItem("oj_session_token") ? "session" : "manual");
  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginError, setLoginError] = useState("");

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

  async function handleSignOut() {
    if (credentialSource === "session" && apiKey) {
      await logout(apiBase, apiKey).catch(() => undefined);
    }
    setCredentialSource("manual");
    setAPIKey("");
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

  return (
    <div className="shell">
      <aside>
        <div className="brand"><span>O</span> OpenJourney</div>
        <nav aria-label="Primary">
          {(["profiles", "segments", "schemas", "api-keys", "privacy", "access", "operations", "audit"] as View[]).map((item) => (
            <button key={item} className={view === item ? "active" : ""}
              onClick={() => setView(item)}>{viewTitles[item][0]}</button>
          ))}
          <button disabled>Journeys <small>next</small></button>
        </nav>
        <div className={`health ${healthy ? "up" : ""}`}>
          <i /> API {healthy === null ? "checking" : healthy ? "ready" : "unavailable"}
        </div>
      </aside>
      <main>
        <header>
          <p>Platform kernel</p>
          <h1>{viewTitles[view][0]}</h1>
          <span>{viewTitles[view][1]}</span>
        </header>
        <section className="card credential">
          <form onSubmit={handleLocalLogin} className="single-action">
            <label>Email
              <input type="email" value={loginEmail} onChange={(event) => setLoginEmail(event.target.value)}
                placeholder="admin@example.com" />
            </label>
            <label>Password
              <input type="password" value={loginPassword} onChange={(event) => setLoginPassword(event.target.value)}
                placeholder="Self-hosted operator password" />
            </label>
            <button disabled={!loginEmail || !loginPassword}>Log in</button>
          </form>
          <ErrorMessage value={loginError} />
          <label>API key
            <input type="password" value={apiKey} onChange={(event) => {
              setCredentialSource("manual");
              setAPIKey(event.target.value);
            }}
              placeholder="Scoped API, local session, or OIDC token" />
          </label>
          {(oidcConfigured || apiKey) && <div className="auth-actions">
            {oidcConfigured && <button onClick={() => void signIn()}>Sign in with OIDC</button>}
            {apiKey && <button className="secondary" onClick={() => void handleSignOut()}>Sign out</button>}
          </div>}
        </section>
        {view === "profiles" && <Profiles apiKey={apiKey} />}
        {view === "segments" && <Segments apiKey={apiKey} />}
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
  const [scopes, setScopes] = useState("events:write,profiles:read");
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
      const result = await createAPIKey(apiBase, apiKey, name, scopes.split(",").map((scope) => scope.trim()).filter(Boolean), expiration);
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
      <label>Scopes<input value={scopes} onChange={(e) => setScopes(e.target.value)}
        placeholder="events:write,profiles:read" required /></label>
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
  const [permissions, setPermissions] = useState("profiles:read");
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
      await createRole(apiBase, apiKey, roleName, permissions.split(",").map((value) => value.trim()).filter(Boolean));
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
      <label>Permissions<input value={permissions} onChange={(e) => setPermissions(e.target.value)} required /></label>
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
