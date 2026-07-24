import { FormEvent, useEffect, useState } from "react";
import { createScoringModel, createScoringModelVersion, listProfileScores, listScoringModels, ProfileScore, publishScoringModelVersion, ScoringModel } from "../api";
import { Card, DataTable, EmptyState, ErrorState, Field, JsonField } from "../components";
import { useForm } from "../useForm";

function message(error: unknown) { return error instanceof Error ? error.message : "Request failed"; }

export default function Scoring({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [models, setModels] = useState<ScoringModel[]>([]);
  const [kind, setKind] = useState<ScoringModel["kind"]>("expression");
  const [profileID, setProfileID] = useState(""); const [scores, setScores] = useState<ProfileScore[]>([]); const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const modelForm = useForm({
    initialValues: { name: "", scoreName: "purchase_propensity", manifestKey: "scoring-manifest", definition: '{"expr":"0.5","inputs":[]}' },
    validate: {
      name: (value: string) => value.trim() ? undefined : "Model name is required",
      scoreName: (value: string) => value.trim() ? undefined : "Score name is required",
      manifestKey: (value: string) => value.trim() ? undefined : "Manifest key is required",
      definition: (value: string) => { try { JSON.parse(value); return undefined; } catch { return "Definition must be valid JSON"; } },
    },
  });
  async function refresh() { try { setModels(await listScoringModels(baseURL, apiKey)); setError(""); } catch (e) { setError(message(e)); } }
  useEffect(() => { if (apiKey) void refresh(); }, [apiKey, baseURL]);
  async function create(event: FormEvent) { event.preventDefault(); if (saving) return; setSaving(true); try {
    if (!modelForm.isValid) { (Object.keys(modelForm.values) as Array<keyof typeof modelForm.values>).forEach(modelForm.touch); return; }
    const model = await createScoringModel(baseURL, apiKey, { name: modelForm.values.name, kind });
    await createScoringModelVersion(baseURL, apiKey, model.id, { score_name: modelForm.values.scoreName, definition: JSON.parse(modelForm.values.definition), manifest_key: modelForm.values.manifestKey, output_min: 0, output_max: 1 });
    modelForm.reset(); await refresh();
  } catch (e) { setError(message(e)); } finally { setSaving(false); } }
  async function publish(model: ScoringModel) { if (saving) return; setSaving(true); try { if (!model.latest_version) throw new Error("Create a version first"); await publishScoringModelVersion(baseURL, apiKey, model.id, model.latest_version, modelForm.values.manifestKey); await refresh(); } catch (e) { setError(message(e)); } finally { setSaving(false); } }
  async function inspect(event: FormEvent) { event.preventDefault(); try { setScores(await listProfileScores(baseURL, apiKey, profileID)); setError(""); } catch (e) { setError(message(e)); } }
  return <section className="stack scoring-view">
    <Card variant="article"><div className="eyebrow">Versioned artifacts</div><h2>Scoring model editor</h2>
      <form className="scoring-form" onSubmit={create}>
        <Field id="scoring-model-name" label="Name" required error={modelForm.getError("name")}><input aria-label="Scoring model name" name="name" value={modelForm.values.name} onChange={modelForm.handleChange} onBlur={modelForm.handleBlur} placeholder="Purchase propensity" /></Field>
        <Field id="scoring-kind" label="Kind"><select name="kind" value={kind} onChange={e => setKind(e.target.value as ScoringModel["kind"])}><option value="expression">Expression</option><option value="llm">LLM</option></select></Field>
        <Field id="scoring-score-name" label="Score name" required error={modelForm.getError("scoreName")}><input name="scoreName" value={modelForm.values.scoreName} onChange={modelForm.handleChange} onBlur={modelForm.handleBlur} /></Field>
        <Field id="scoring-manifest-key" label="Manifest key" required error={modelForm.getError("manifestKey")}><input name="manifestKey" value={modelForm.values.manifestKey} onChange={modelForm.handleChange} onBlur={modelForm.handleBlur} /></Field>
        <Field id="scoring-definition" label="Definition JSON" className="scoring-wide" required error={modelForm.getError("definition")}><JsonField aria-label="Definition JSON" name="definition" value={modelForm.values.definition} onChange={modelForm.handleChange} onBlur={modelForm.handleBlur} validateOnBlur={false} rows={4} /></Field>
        <button disabled={saving || !apiKey || !modelForm.isValid}>{saving ? "Creating…" : "Create draft version"}</button>
      </form><ErrorMessage value={error} /></Card>
    <Card variant="article"><div className="section-title"><div><div className="eyebrow">Registry</div><h2>Scoring models</h2></div><button onClick={() => void refresh()}>Refresh</button></div>{models.length > 0 ? <DataTable headers={["Name", "Kind", "Latest", "Action"]} rows={models.map(model => [model.name, model.kind, `v${model.latest_version}`, <button onClick={() => void publish(model)} disabled={saving || !model.latest_version}>{saving ? "Publishing…" : "Publish latest"}</button>])} /> : <EmptyState title="No scoring models" description="Create a new scoring model to begin evaluating profiles." icon="plus" cta={{ label: "Create model", onClick: () => document.querySelector<HTMLInputElement>('input[aria-label="Scoring model name"]')?.focus() }} />}</Card>
    <Card variant="article"><div className="eyebrow">Profile inspector</div><h2>Computed scores</h2><form className="single-action" onSubmit={inspect}><label>Profile ID<input value={profileID} onChange={e => setProfileID(e.target.value)} required placeholder="profile-uuid" /></label><button>Inspect scores</button></form>{scores.length > 0 && <DataTable headers={["Score", "Value", "Model version", "Computed"]} rows={scores.map(score => [score.score_name, score.value, `v${score.model_version}`, new Date(score.computed_at).toLocaleString()])} />}{scores.length === 0 && profileID && <EmptyState title="No computed scores" description="This profile has no scores yet. Create a scoring model and compute scores." icon="info" cta={{ label: "Create model", onClick: () => document.querySelector<HTMLInputElement>('input[aria-label="Scoring model name"]')?.focus() }} />}</Card>
  </section>;
}

function ErrorMessage({ value }: { value: string }) { return value ? <ErrorState description={value} role="alert" /> : null; }
