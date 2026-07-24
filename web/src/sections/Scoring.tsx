import { FormEvent, useEffect, useState } from "react";
import { createScoringModel, createScoringModelVersion, listProfileScores, listScoringModels, ProfileScore, publishScoringModelVersion, ScoringModel } from "../api";
import { Card, DataTable, EmptyState, ErrorState, JsonField } from "../components";

function message(error: unknown) { return error instanceof Error ? error.message : "Request failed"; }

export default function Scoring({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [models, setModels] = useState<ScoringModel[]>([]); const [name, setName] = useState("");
  const [kind, setKind] = useState<ScoringModel["kind"]>("expression"); const [scoreName, setScoreName] = useState("purchase_propensity");
  const [definition, setDefinition] = useState('{"expr":"0.5","inputs":[]}'); const [manifestKey, setManifestKey] = useState("scoring-manifest");
  const [profileID, setProfileID] = useState(""); const [scores, setScores] = useState<ProfileScore[]>([]); const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  async function refresh() { try { setModels(await listScoringModels(baseURL, apiKey)); setError(""); } catch (e) { setError(message(e)); } }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey, baseURL]);
  async function create(event: FormEvent) { event.preventDefault(); if (saving) return; setSaving(true); try {
    const model = await createScoringModel(baseURL, apiKey, { name, kind });
    await createScoringModelVersion(baseURL, apiKey, model.id, { score_name: scoreName, definition: JSON.parse(definition), manifest_key: manifestKey, output_min: 0, output_max: 1 });
    setName(""); await refresh();
  } catch (e) { setError(message(e)); } finally { setSaving(false); } }
  async function publish(model: ScoringModel) { if (saving) return; setSaving(true); try { if (!model.latest_version) throw new Error("Create a version first"); await publishScoringModelVersion(baseURL, apiKey, model.id, model.latest_version, manifestKey); await refresh(); } catch (e) { setError(message(e)); } finally { setSaving(false); } }
  async function inspect(event: FormEvent) { event.preventDefault(); try { setScores(await listProfileScores(baseURL, apiKey, profileID)); setError(""); } catch (e) { setError(message(e)); } }
  return <section className="stack scoring-view">
    <Card variant="article"><div className="eyebrow">Versioned artifacts</div><h2>Scoring model editor</h2>
      <form className="scoring-form" onSubmit={create}><label>Name<input aria-label="Scoring model name" value={name} onChange={e => setName(e.target.value)} required placeholder="Purchase propensity" /></label><label>Kind<select value={kind} onChange={e => setKind(e.target.value as ScoringModel["kind"])}><option value="expression">Expression</option><option value="llm">LLM</option></select></label><label>Score name<input value={scoreName} onChange={e => setScoreName(e.target.value)} required /></label><label>Manifest key<input value={manifestKey} onChange={e => setManifestKey(e.target.value)} required /></label><label className="scoring-wide">Definition JSON</label><JsonField value={definition} onChange={e => setDefinition((e.target as HTMLTextAreaElement).value)} onBlur={() => {}} rows={4} /><button disabled={saving || !apiKey}>{saving ? "Creating…" : "Create draft version"}</button></form><ErrorMessage value={error} /></Card>
    <Card variant="article"><div className="section-title"><div><div className="eyebrow">Registry</div><h2>Scoring models</h2></div><button onClick={() => void refresh()}>Refresh</button></div>{models.length > 0 ? <DataTable headers={["Name", "Kind", "Latest", "Action"]} rows={models.map(model => [model.name, model.kind, `v${model.latest_version}`, <button onClick={() => void publish(model)} disabled={saving || !model.latest_version}>{saving ? "Publishing…" : "Publish latest"}</button>])} /> : <EmptyState title="No scoring models" description="Create a new scoring model to begin evaluating profiles." icon="plus" cta={{ label: "Create model", onClick: () => document.querySelector<HTMLInputElement>('input[aria-label="Scoring model name"]')?.focus() }} />}</Card>
    <Card variant="article"><div className="eyebrow">Profile inspector</div><h2>Computed scores</h2><form className="single-action" onSubmit={inspect}><label>Profile ID<input value={profileID} onChange={e => setProfileID(e.target.value)} required placeholder="profile-uuid" /></label><button>Inspect scores</button></form>{scores.length > 0 && <DataTable headers={["Score", "Value", "Model version", "Computed"]} rows={scores.map(score => [score.score_name, score.value, `v${score.model_version}`, new Date(score.computed_at).toLocaleString()])} />}{scores.length === 0 && profileID && <EmptyState title="No computed scores" description="This profile has no scores yet. Create a scoring model and compute scores." icon="info" cta={{ label: "Create model", onClick: () => document.querySelector<HTMLInputElement>('input[aria-label="Scoring model name"]')?.focus() }} />}</Card>
  </section>;
}

function ErrorMessage({ value }: { value: string }) { return value ? <ErrorState description={value} role="alert" /> : null; }
