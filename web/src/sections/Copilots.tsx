import { FormEvent, useState } from "react";
import {
  CopilotResponse,
  createAudienceCopilot,
  createContentCopilot,
  createJourneyCopilot,
  createPerformanceCopilot,
} from "../api";
import {
  Badge,
  Button,
  Card,
  Field,
  Input,
  Textarea,
  useToast,
} from "../components";

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

function DraftCard({
  kind,
  result,
  onRefine,
  onAccept,
}: {
  kind: CopilotKind;
  result: CopilotResponse;
  onRefine: () => void;
  onAccept: () => void;
}) {
  const [accepted, setAccepted] = useState(false);
  const draft = result.draft;

  const handleAccept = () => {
    setAccepted(true);
    onAccept();
  };

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <span style={{ fontSize: "12px", textTransform: "uppercase", letterSpacing: "0.05em", color: "var(--color-primary, #3b82f6)", fontWeight: 600 }}>
            Governed draft
          </span>
          <h3 style={{ margin: "4px 0 0 0", fontSize: "20px" }}>Ready for review</h3>
        </div>
        <Badge kind={accepted ? "success" : "draft"}>
          {accepted ? "Accepted" : "Draft only"}
        </Badge>
      </div>

      <p style={{ margin: 0, color: "var(--color-ink-muted)", fontSize: "14px" }}>
        AI has proposed this resource. Review and approve it in place or in the full editor before publishing.
      </p>

      {kind === "content" && draft && (
        <div style={{ padding: "12px", background: "rgba(0,0,0,0.2)", borderRadius: "6px" }}>
          <strong>{String(draft.subject_template || "Untitled subject")}</strong>
          <p style={{ margin: "8px 0 0 0", fontSize: "13px" }}>
            {String(draft.html_template || draft.body_template || "")}
          </p>
        </div>
      )}

      {kind === "audience" && draft && (
        <div style={{ padding: "12px", background: "rgba(0,0,0,0.2)", borderRadius: "6px" }}>
          <strong>Audience Segment Draft</strong>
          <p style={{ margin: "8px 0 0 0", fontSize: "13px" }}>
            {String(draft.dsl || draft.description || JSON.stringify(draft))}
          </p>
        </div>
      )}

      {kind === "journey" && draft && (
        <div style={{ padding: "12px", background: "rgba(0,0,0,0.2)", borderRadius: "6px" }}>
          <strong>Journey Draft: {String(draft.name || "Untitled Journey")}</strong>
          <p style={{ margin: "8px 0 0 0", fontSize: "13px" }}>
            {String(draft.description || JSON.stringify(draft.steps || draft))}
          </p>
        </div>
      )}

      {kind === "performance" && draft && (
        <div style={{ padding: "12px", background: "rgba(0,0,0,0.2)", borderRadius: "6px" }}>
          <strong>Performance Analysis Summary</strong>
          <p style={{ margin: "8px 0 0 0", fontSize: "13px" }}>
            {String(draft.summary || draft.recommendation || JSON.stringify(draft))}
          </p>
        </div>
      )}

      <details style={{ fontSize: "12px", color: "var(--color-ink-muted)" }}>
        <summary style={{ cursor: "pointer" }}>Inspect structured draft</summary>
        <pre style={{ margin: "8px 0 0 0", padding: "8px", background: "rgba(0,0,0,0.3)", borderRadius: "4px", overflowX: "auto" }}>
          {JSON.stringify(result, null, 2)}
        </pre>
      </details>

      <div style={{ display: "flex", gap: "12px", flexWrap: "wrap", alignItems: "center" }}>
        <Button variant="primary" onClick={handleAccept} disabled={accepted}>
          {accepted ? "Accepted" : "Accept Draft"}
        </Button>
        <Button variant="secondary" onClick={onRefine}>
          Refine Draft
        </Button>
        <Button
          variant="secondary"
          onClick={() => {
            window.location.hash = reviewViews[kind];
          }}
        >
          Review &amp; approve in {reviewViews[kind]}
        </Button>
      </div>

      {result.activity_id && (
        <span style={{ fontSize: "12px", color: "var(--color-ink-muted)" }}>
          Activity recorded: {result.activity_id}
        </span>
      )}
    </Card>
  );
}

export default function Copilots({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const { push: toast } = useToast();
  const [kind, setKind] = useState<CopilotKind>("content");
  const [brief, setBrief] = useState("");
  const [locale, setLocale] = useState("en-US");
  const [name, setName] = useState("");
  const [campaignID, setCampaignID] = useState("");
  const [result, setResult] = useState<CopilotResponse | null>(null);
  const [saving, setSaving] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setResult(null);
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
      toast({ kind: "success", message: `Generated ${kind} draft` });
    } catch (cause) {
      toast({ kind: "error", message: errorMessage(cause) });
    } finally {
      setSaving(false);
    }
  }

  function handleRefine() {
    toast({ kind: "info", message: "Adjust your description or settings and generate again to refine." });
  }

  function handleAccept() {
    toast({ kind: "success", message: "Draft accepted inline for approval workflow." });
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "24px" }}>
      <Card style={{ display: "flex", flexDirection: "column", gap: "20px" }}>
        <div>
          <span style={{ fontSize: "12px", textTransform: "uppercase", letterSpacing: "0.05em", color: "var(--color-primary, #3b82f6)", fontWeight: 600 }}>
            Governed AI
          </span>
          <h2 style={{ margin: "4px 0 0 0", fontSize: "24px" }}>Draft with a copilot</h2>
          <p style={{ margin: "4px 0 0 0", color: "var(--color-ink-muted)", fontSize: "14px" }}>
            Copilots create reviewable drafts only. Every proposal is validated and recorded before you approve it.
          </p>
        </div>

        <div style={{ display: "flex", gap: "8px", borderBottom: "1px solid rgba(255,255,255,0.1)", paddingBottom: "12px" }}>
          {(["content", "audience", "journey", "performance"] as CopilotKind[]).map((item) => (
            <Button
              key={item}
              variant={kind === item ? "primary" : "secondary"}
              onClick={() => {
                setKind(item);
                setResult(null);
              }}
            >
              {item === "content" ? "Content" : item === "audience" ? "Audience" : item === "journey" ? "Journey" : "Performance"}
            </Button>
          ))}
        </div>

        <form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
          {kind === "performance" ? (
            <Field label="Campaign ID">
              <Input
                value={campaignID}
                onChange={(event) => setCampaignID(event.target.value)}
                placeholder="campaign UUID"
                required
              />
            </Field>
          ) : (
            <Field
              label={
                kind === "audience"
                  ? "Describe the audience"
                  : kind === "journey"
                  ? "Describe the journey"
                  : "Describe the content"
              }
            >
              <Textarea
                value={brief}
                onChange={(event) => setBrief(event.target.value)}
                placeholder={kind === "content" ? "Welcome new customers…" : "Customers who…"}
                rows={4}
                required
              />
            </Field>
          )}

          {kind === "content" && (
            <Field label="Locale">
              <Input value={locale} onChange={(event) => setLocale(event.target.value)} />
            </Field>
          )}

          {kind === "journey" && (
            <Field label="Journey name (optional)">
              <Input value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
          )}

          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <Button type="submit" variant="primary" disabled={saving || !apiKey}>
              {saving ? "Drafting…" : "Create governed draft"}
            </Button>
          </div>
        </form>
      </Card>

      {result && (
        <DraftCard
          kind={kind}
          result={result}
          onRefine={handleRefine}
          onAccept={handleAccept}
        />
      )}
    </div>
  );
}
