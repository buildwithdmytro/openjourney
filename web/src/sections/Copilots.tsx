import { FormEvent, useState } from "react";
import {
  CopilotResponse,
  createAudienceCopilot,
  createContentCopilot,
  createJourneyCopilot,
  createPerformanceCopilot,
} from "../api";

type CopilotKind = "content" | "audience" | "journey" | "performance";

const reviewViews: Record<CopilotKind, string> = {
  content: "templates",
  audience: "segments",
  journey: "journeys",
  performance: "campaigns",
};

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

function DraftCard({ kind, result }: { kind: CopilotKind; result: CopilotResponse }) {
  const draft = result.draft;
  return <article className="card copilot-draft" aria-label={`${kind} AI draft`}>
    <div className="section-title">
      <div><div className="eyebrow">Governed draft</div><h2>Ready for review</h2></div>
      <span className="pill draft">Draft only</span>
    </div>
    <p className="muted">AI has proposed this resource. Review it in the existing editor before a human approves publication.</p>
    {kind === "content" && draft && <div className="copilot-content-preview">
      <strong>{String(draft.subject_template || "Untitled subject")}</strong>
      <p>{String(draft.html_template || draft.body_template || "")}</p>
    </div>}
    <details><summary>Inspect structured draft</summary><pre>{JSON.stringify(result, null, 2)}</pre></details>
    <button type="button" onClick={() => { window.location.hash = reviewViews[kind]; }}>
      Review &amp; approve in {reviewViews[kind]}
    </button>
    {result.activity_id && <small className="field-help">Activity recorded: {result.activity_id}</small>}
  </article>;
}

export default function Copilots({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [kind, setKind] = useState<CopilotKind>("content");
  const [brief, setBrief] = useState("");
  const [locale, setLocale] = useState("en-US");
  const [name, setName] = useState("");
  const [campaignID, setCampaignID] = useState("");
  const [result, setResult] = useState<CopilotResponse | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true); setError(""); setResult(null);
    try {
      let next: CopilotResponse;
      if (kind === "content") next = await createContentCopilot(baseURL, apiKey, { brief, locale });
      else if (kind === "audience") next = await createAudienceCopilot(baseURL, apiKey, brief);
      else if (kind === "journey") next = await createJourneyCopilot(baseURL, apiKey, { brief, name: name || undefined });
      else {
        if (!campaignID.trim()) throw new Error("Campaign ID is required");
        next = await createPerformanceCopilot(baseURL, apiKey, campaignID.trim());
      }
      setResult(next);
    } catch (cause) { setError(errorMessage(cause)); }
    finally { setSaving(false); }
  }

  return <section className="stack copilot-view">
    <article className="card copilot-hero">
      <div><div className="eyebrow">Governed AI</div><h2>Draft with a copilot</h2>
        <p className="muted">Copilots create reviewable drafts only. Every proposal is validated and recorded before you approve it.</p></div>
      <div className="copilot-tabs" role="tablist" aria-label="Copilot type">
        {(["content", "audience", "journey", "performance"] as CopilotKind[]).map((item) =>
          <button type="button" role="tab" aria-selected={kind === item} className={kind === item ? "active" : "secondary"}
            key={item} onClick={() => { setKind(item); setResult(null); setError(""); }}>
            {item === "content" ? "Content" : item === "audience" ? "Audience" : item === "journey" ? "Journey" : "Performance"}
          </button>)}
      </div>
      <form onSubmit={submit} className="copilot-form">
        {kind === "performance" ? <label>Campaign ID<input value={campaignID} onChange={(event) => setCampaignID(event.target.value)} placeholder="campaign UUID" required /></label> :
          <label>{kind === "audience" ? "Describe the audience" : kind === "journey" ? "Describe the journey" : "Describe the content"}
            <textarea value={brief} onChange={(event) => setBrief(event.target.value)} placeholder={kind === "content" ? "Welcome new customers…" : "Customers who…"} rows={4} required /></label>}
        {kind === "content" && <label>Locale<input value={locale} onChange={(event) => setLocale(event.target.value)} /></label>}
        {kind === "journey" && <label>Journey name (optional)<input value={name} onChange={(event) => setName(event.target.value)} /></label>}
        <button disabled={saving || !apiKey}>{saving ? "Drafting…" : "Create governed draft"}</button>
      </form>
      {error && <p className="error" role="alert">{error}</p>}
    </article>
    {result && <DraftCard kind={kind} result={result} />}
  </section>;
}
