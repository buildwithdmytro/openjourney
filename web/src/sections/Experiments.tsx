import { FormEvent, useEffect, useState } from "react";
import { createExperiment, Experiment, listExperiments } from "../api";

export default function Experiments({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [items, setItems] = useState<Experiment[]>([]);
  const [name, setName] = useState("");
  const [subjectType, setSubjectType] = useState<Experiment["subject_type"]>("campaign");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function load() {
    try { setItems(await listExperiments(baseURL, apiKey)); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to load experiments"); }
  }

  useEffect(() => { void load(); }, [apiKey, baseURL]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true); setError("");
    try {
      await createExperiment(baseURL, apiKey, {
        name, subject_type: subjectType, status: "draft", method: "frequentist",
        seed: crypto.randomUUID(), holdout_pct: 0,
        variants: [{ label: "control", weight: 50, is_control: true }, { label: "b", weight: 50, is_control: false }],
      });
      setName("");
      await load();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to create experiment"); }
    finally { setSaving(false); }
  }

  return <>
    <section className="card">
      <h2>Create experiment</h2>
      <form onSubmit={submit} className="form-grid">
        <label>Experiment name<input value={name} onChange={(event) => setName(event.target.value)} required /></label>
        <label>Subject type<select value={subjectType} onChange={(event) => setSubjectType(event.target.value as Experiment["subject_type"])}><option value="campaign">Campaign</option><option value="journey">Journey</option></select></label>
        <button disabled={saving || !name.trim()}>{saving ? "Creating…" : "Create experiment"}</button>
      </form>
      {error && <p role="alert">{error}</p>}
    </section>
    <section className="card">
      <h2>Experiments ({items.length})</h2>
      {items.length === 0 ? <p className="muted">No experiments yet.</p> : <table><thead><tr><th>Name</th><th>Subject</th><th>Status</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td>{item.name}</td><td>{item.subject_type}</td><td><span className="pill">{item.status}</span></td></tr>)}</tbody></table>}
    </section>
  </>;
}
