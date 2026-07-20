import { FormEvent, useEffect, useState } from "react";
import { AIActivity, AIBudget, AIProviderConfig, FieldClassification, getAIBudget, listAIActivity, listAIProviders, listFieldClassifications, saveAIProvider, saveFieldClassification } from "../api";
import { Button, Card, DataTable, Field, Input, Select, Textarea } from "../components";

const blankProvider: Partial<AIProviderConfig> = { provider: "fake", status: "active", is_default: true, monthly_budget_cents: 0, endpoint_allowlist: [] };
function message(error: unknown) { return error instanceof Error ? error.message : "Request failed"; }

export default function Governance({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [providers, setProviders] = useState<AIProviderConfig[]>([]); const [budget, setBudget] = useState<AIBudget | null>(null);
  const [activity, setActivity] = useState<AIActivity[]>([]); const [classifications, setClassifications] = useState<FieldClassification[]>([]);
  const [provider, setProvider] = useState<Partial<AIProviderConfig>>(blankProvider); const [field, setField] = useState<Partial<FieldClassification>>({ entity_type: "profile", classification: "internal", send_to_model: "redact" });
  const [error, setError] = useState(""); const [saving, setSaving] = useState(false);
  async function load() { try { const [p, b, a, c] = await Promise.all([listAIProviders(baseURL, apiKey), getAIBudget(baseURL, apiKey), listAIActivity(baseURL, apiKey), listFieldClassifications(baseURL, apiKey)]); setProviders(p); setBudget(b); setActivity(a); setClassifications(c); setProvider(p[0] || blankProvider); } catch (e) { setError(message(e)); } }
  useEffect(() => { void load(); }, [apiKey, baseURL]);
  async function saveProvider(e: FormEvent) { e.preventDefault(); setSaving(true); setError(""); try { await saveAIProvider(baseURL, apiKey, { ...provider, endpoint_allowlist: (provider.endpoint_allowlist || []).filter(Boolean) }); await load(); } catch (e) { setError(message(e)); } finally { setSaving(false); } }
  async function saveField(e: FormEvent) { e.preventDefault(); setSaving(true); setError(""); try { await saveFieldClassification(baseURL, apiKey, field); setField({ entity_type: "profile", classification: "internal", send_to_model: "redact" }); await load(); } catch (e) { setError(message(e)); } finally { setSaving(false); } }
  return (
    <section className="stack governance-view">
      <Card variant="article">
        <div className="eyebrow">Provider and budget</div>
        <h2>AI governance settings</h2>
        <p className="muted">Secrets are managed by the server and are never displayed here.</p>
        <form className="governance-form" onSubmit={saveProvider}>
          <Field id="provider" label="Provider">
            <Select
              value={provider.provider || "fake"}
              onChange={e =>
                setProvider({
                  ...provider,
                  provider: e.target.value as AIProviderConfig["provider"],
                })
              }
            >
              <option value="fake">Fake (development)</option>
              <option value="anthropic">Anthropic</option>
              <option value="openai">OpenAI-compatible</option>
            </Select>
          </Field>
          <Field id="budget" label="Monthly budget (cents)">
            <Input
              type="number"
              min="0"
              value={provider.monthly_budget_cents || 0}
              onChange={e =>
                setProvider({
                  ...provider,
                  monthly_budget_cents: Number(e.target.value),
                })
              }
            />
          </Field>
          <Field
            id="allowlist"
            label="Endpoint allowlist (one host per line)"
            className="governance-wide"
          >
            <Textarea
              value={(provider.endpoint_allowlist || []).join("\n")}
              onChange={e =>
                setProvider({
                  ...provider,
                  endpoint_allowlist: e.target.value
                    .split("\n")
                    .map(v => v.trim()),
                })
              }
            />
          </Field>
          <Button disabled={saving}>
            {provider.id ? "Save provider" : "Add provider"}
          </Button>
        </form>
        {budget && (
          <p className="field-help">
            {budget.usage.period}: {budget.usage.cost_cents}¢ used of{" "}
            {budget.monthly_budget_cents || "unlimited"}¢ ·{" "}
            {budget.usage.input_tokens + budget.usage.output_tokens} tokens
          </p>
        )}
      </Card>
      <Card variant="article">
        <div className="eyebrow">Permission-aware egress</div>
        <h2>Field classifications</h2>
        <form className="governance-form" onSubmit={saveField}>
          <Field id="entity" label="Entity">
            <Select
              value={field.entity_type}
              onChange={e =>
                setField({
                  ...field,
                  entity_type: e.target.value as FieldClassification["entity_type"],
                })
              }
            >
              <option value="profile">Profile</option>
              <option value="event">Event</option>
            </Select>
          </Field>
          <Field id="field_path" label="Field path">
            <Input
              required
              value={field.field_path || ""}
              onChange={e => setField({ ...field, field_path: e.target.value })}
              placeholder="email"
            />
          </Field>
          <Field id="classification" label="Classification">
            <Select
              value={field.classification}
              onChange={e =>
                setField({
                  ...field,
                  classification: e.target.value as FieldClassification["classification"],
                })
              }
            >
              <option>public</option>
              <option>internal</option>
              <option>confidential</option>
              <option>restricted</option>
            </Select>
          </Field>
          <Field id="send_to_model" label="Model action">
            <Select
              value={field.send_to_model}
              onChange={e =>
                setField({
                  ...field,
                  send_to_model: e.target.value as FieldClassification["send_to_model"],
                })
              }
            >
              <option>redact</option>
              <option>tokenize</option>
              <option>allow</option>
              <option>deny</option>
            </Select>
          </Field>
          <Button disabled={saving}>Add classification</Button>
        </form>
        <DataTable
          headers={["Entity", "Field", "Classification", "Model action"]}
          rows={classifications.map(c => [
            c.entity_type,
            c.field_path,
            c.classification,
            c.send_to_model,
          ])}
        />
      </Card>
      <Card variant="article">
        <div className="eyebrow">Append-only audit</div>
        <h2>AI activity</h2>
        <DataTable
          headers={["Action", "Provider/model", "Decision", "Cost", "When"]}
          rows={activity.map(a => [
            a.action,
            `${a.provider}/${a.model}`,
            a.policy_decision,
            `${a.cost_cents}¢`,
            new Date(a.created_at).toLocaleString(),
          ])}
        />
      </Card>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
