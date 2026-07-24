import { FormEvent, useEffect, useState } from "react";
import { InAppMessage, listMessages, createMessage, getProfileInbox, listTemplates, Template } from "../api";
import { EmptyState, ErrorState, useToast } from "../components";

const message = (e: unknown) => e instanceof Error ? e.message : "Request failed";
const blank = (): Partial<InAppMessage> => ({
  message_type: "card",
  rank: 0,
  categories: [],
  status: "pending",
});

export default function Messaging({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const { push: toast } = useToast();
  const [messages, setMessages] = useState<InAppMessage[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [profileInbox, setProfileInbox] = useState<InAppMessage[]>([]);
  const [draft, setDraft] = useState<Partial<InAppMessage>>(blank());
  const [selectedTemplate, setSelectedTemplate] = useState("");
  const [profileId, setProfileId] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [saving, setSaving] = useState(false);

  async function refresh() {
    try {
      const msgs = await listMessages(baseURL, apiKey);
      setMessages(msgs);
      const tmps = await listTemplates(baseURL, apiKey);
      setTemplates(tmps.filter(t => t.channel === "in_app"));
    } catch (e) {
      setError(message(e));
    }
  }

  useEffect(() => {
    if (apiKey) void refresh();
  }, [apiKey, baseURL]);

  async function save(e: FormEvent) {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    try {
      await createMessage(baseURL, apiKey, {
        ...draft,
        template_id: selectedTemplate || undefined,
      });
      setDraft(blank());
      setSelectedTemplate("");
      setNotice("Message created.");
      toast({ kind: "success", message: "Message created successfully" });
      await refresh();
    } catch (e) {
      setError(message(e));
    } finally {
      setSaving(false);
    }
  }

  async function loadProfileInbox() {
    if (!profileId) return;
    try {
      const inbox = await getProfileInbox(baseURL, apiKey, profileId);
      setProfileInbox(inbox);
    } catch (e) {
      setError(message(e));
    }
  }

  return (
    <section className="stack messaging-view">
      <article className="card">
        <div className="eyebrow">In-app messaging</div>
        <h2>Messages and content cards</h2>
        <p className="muted">Create and manage in-app messages, content cards, and web push campaigns.</p>
        {error && <ErrorState description={error} role="alert" />}
        {notice && <p className="success" role="status">{notice}</p>}
      </article>

      <div className="acquisition-grid">
        <article className="card">
          <div className="section-title">
            <h2>Messages</h2>
            <button onClick={() => { setDraft(blank()); setSelectedTemplate(""); }}>New message</button>
          </div>
          {messages.length ? (
            <table>
              <thead>
                <tr>
                  <th>Type</th>
                  <th>Status</th>
                  <th>Rank</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {messages.map(msg => (
                  <tr key={msg.id}>
                    <td>{msg.message_type}</td>
                    <td><span className={`pill ${msg.status}`}>{msg.status}</span></td>
                    <td>{msg.rank}</td>
                    <td>{new Date(msg.created_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState title="No messages yet" description="Create a message to get started" icon="plus" cta={{ label: "New message", onClick: () => { setDraft(blank()); setSelectedTemplate(""); } }} />
          )}
        </article>

        <article className="card">
          {draft.message_type ? (
            <>
              <div className="section-title">
                <h2>New message</h2>
              </div>
              <form className="governance-form" onSubmit={save}>
                <label>
                  Type
                  <select value={draft.message_type} onChange={e => setDraft({ ...draft, message_type: e.target.value as InAppMessage["message_type"] })}>
                    <option value="modal">Modal</option>
                    <option value="banner">Banner</option>
                    <option value="fullscreen">Fullscreen</option>
                    <option value="card">Card</option>
                  </select>
                </label>
                <label>
                  Rank
                  <input type="number" value={draft.rank || 0} onChange={e => setDraft({ ...draft, rank: Number(e.target.value) })} />
                </label>
                <label>
                  Categories (comma-separated)
                  <input value={(draft.categories || []).join(", ")} onChange={e => setDraft({ ...draft, categories: e.target.value.split(",").map(c => c.trim()).filter(Boolean) })} placeholder="promo, featured" />
                </label>
                <label>
                  Start at
                  <input type="datetime-local" value={draft.start_at ? new Date(draft.start_at).toISOString().slice(0, 16) : ""} onChange={e => setDraft({ ...draft, start_at: e.target.value ? new Date(e.target.value).toISOString() : "" })} />
                </label>
                <label>
                  Expires at (optional)
                  <input type="datetime-local" value={draft.expires_at ? new Date(draft.expires_at).toISOString().slice(0, 16) : ""} onChange={e => setDraft({ ...draft, expires_at: e.target.value ? new Date(e.target.value).toISOString() : undefined })} />
                </label>
                <label>
                  From template (optional)
                  <select value={selectedTemplate} onChange={e => setSelectedTemplate(e.target.value)}>
                    <option value="">Select a template…</option>
                    {templates.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
                  </select>
                </label>
                <button type="submit" disabled={saving}>{saving ? "Creating…" : "Create message"}</button>
              </form>
            </>
          ) : (
            <EmptyState title="No message selected" description="Choose a message type or select a template to begin" icon="search" cta={{ label: "New message", onClick: () => { setDraft(blank()); setSelectedTemplate(""); } }} />
          )}
        </article>
      </div>

      <article className="card">
        <div className="section-title">
          <h2>Profile inbox</h2>
        </div>
        <form className="single-action" onSubmit={e => { e.preventDefault(); void loadProfileInbox(); }}>
          <label>
            Profile ID
            <input value={profileId} onChange={e => setProfileId(e.target.value)} placeholder="uuid" required />
          </label>
          <button>Load inbox</button>
        </form>
        {profileInbox.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>Type</th>
                <th>Status</th>
                <th>Displayed</th>
                <th>Clicked</th>
                <th>Dismissed</th>
              </tr>
            </thead>
            <tbody>
              {profileInbox.map(msg => (
                <tr key={msg.id}>
                  <td>{msg.message_type}</td>
                  <td><span className={`pill ${msg.status}`}>{msg.status}</span></td>
                  <td>{msg.displayed_at ? new Date(msg.displayed_at).toLocaleString() : "—"}</td>
                  <td>{msg.clicked_at ? new Date(msg.clicked_at).toLocaleString() : "—"}</td>
                  <td>{msg.dismissed_at ? new Date(msg.dismissed_at).toLocaleString() : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </article>
    </section>
  );
}
