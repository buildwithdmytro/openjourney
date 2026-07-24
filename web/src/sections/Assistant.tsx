import { FormEvent, useState } from "react";
import { createInsightsCopilot, InsightsCopilotResponse } from "../api";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  Field,
  Input,
  Spinner,
  useToast,
} from "../components";

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

export default function Assistant({
  apiKey,
  baseURL,
}: {
  apiKey: string;
  baseURL: string;
}) {
  const { push: toast } = useToast();
  const [question, setQuestion] = useState("");
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<InsightsCopilotResponse | null>(null);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!question.trim()) return;

    setLoading(true);
    try {
      const res = await createInsightsCopilot(baseURL, apiKey, question.trim());
      setResult(res);
      toast({ message: "Analytics question answered with grounded insights", kind: "success" });
    } catch (err) {
      toast({ message: errorMessage(err), kind: "error" });
    } finally {
      setLoading(false);
    }
  };

  const sampleQuestions = [
    "What is our retention rate and funnel conversion over time?",
    "How have our campaign delivery costs evolved this month?",
    "Show cohort retention performance and growth metrics.",
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "24px" }}>
      <Card style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
        <div>
          <h3 style={{ margin: "0 0 4px 0" }}>Conversational Analytics Assistant</h3>
          <p style={{ margin: 0, color: "var(--color-ink-muted)", fontSize: "14px" }}>
            Ask natural-language analytics questions. Answers are calculated using governed read-only tools over platform report data, citation-grounded, and fully audited.
          </p>
        </div>

        <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
          <Field label="Your analytics question">
            <Input
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              placeholder="e.g. What is our retention rate and conversion over time?"
              disabled={loading}
            />
          </Field>
          <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
            <Button type="submit" disabled={loading || !question.trim()}>
              {loading ? (
                <>
                  <Spinner /> Asking Assistant…
                </>
              ) : (
                "Ask Assistant"
              )}
            </Button>
          </div>
        </form>

        <div style={{ marginTop: "8px" }}>
          <span style={{ fontSize: "12px", color: "var(--color-ink-muted)", fontWeight: 600 }}>Suggested questions:</span>
          <div style={{ display: "flex", flexWrap: "wrap", gap: "8px", marginTop: "8px" }}>
            {sampleQuestions.map((q) => (
              <button
                key={q}
                type="button"
                className="secondary small"
                onClick={() => setQuestion(q)}
                style={{
                  background: "transparent",
                  border: "1px solid rgba(255,255,255,0.15)",
                  color: "var(--color-ink-muted)",
                  padding: "6px 12px",
                  borderRadius: "16px",
                  fontSize: "12px",
                  cursor: "pointer",
                }}
              >
                {q}
              </button>
            ))}
          </div>
        </div>
      </Card>

      {!result && !loading && (
        <EmptyState
          title="No query performed yet"
          description="Enter a question above or pick a suggested question to run the governed AI assistant."
        />
      )}

      {result && (
        <div style={{ display: "flex", flexDirection: "column", gap: "20px" }}>
          {/* Grounded Answer & Summary */}
          <Card style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <div>
                <span style={{ fontSize: "12px", textTransform: "uppercase", letterSpacing: "0.05em", color: "var(--color-primary, #3b82f6)", fontWeight: 600 }}>
                  Grounded Answer
                </span>
                <h3 style={{ margin: "4px 0 0 0" }}>Summary</h3>
              </div>
              <Badge kind={result.status === "completed" ? "success" : "warn"}>
                {result.status}
              </Badge>
            </div>

            <p style={{ fontSize: "15px", lineHeight: "1.6", margin: 0 }}>
              {result.summary}
            </p>

            {result.insights && result.insights.length > 0 && (
              <div style={{ marginTop: "8px" }}>
                <h4 style={{ margin: "0 0 8px 0", fontSize: "14px" }}>Key Insights</h4>
                <ul style={{ margin: 0, paddingLeft: "20px", display: "flex", flexDirection: "column", gap: "6px", fontSize: "14px", color: "var(--color-ink-muted)" }}>
                  {result.insights.map((insight, idx) => (
                    <li key={idx}>{insight}</li>
                  ))}
                </ul>
              </div>
            )}

            {result.activity_id && (
              <div style={{ fontSize: "12px", color: "var(--color-ink-muted)", borderTop: "1px solid rgba(255,255,255,0.1)", paddingTop: "8px" }}>
                Activity Audit ID: <code>{result.activity_id}</code>
              </div>
            )}
          </Card>

          {/* Key Metrics Grounding Grid */}
          {result.key_metrics && result.key_metrics.length > 0 && (
            <Card style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
              <h3 style={{ margin: 0 }}>Citation-Grounded Metrics</h3>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))", gap: "16px" }}>
                {result.key_metrics.map((km, idx) => {
                  return (
                    <div
                      key={idx}
                      style={{
                        padding: "12px",
                        background: "rgba(0,0,0,0.2)",
                        borderRadius: "8px",
                        border: "1px solid rgba(255,255,255,0.08)",
                        display: "flex",
                        flexDirection: "column",
                        gap: "8px",
                      }}
                    >
                      <span style={{ fontSize: "12px", color: "var(--color-ink-muted)" }}>{km.name}</span>
                      <div style={{ display: "flex", alignItems: "baseline", gap: "8px" }}>
                        <span style={{ fontSize: "22px", fontWeight: 700 }}>{String(km.value)}</span>
                      </div>
                      <span style={{ fontSize: "11px", color: "var(--color-ink-muted)", fontStyle: "italic" }}>
                        Source: {km.source}
                      </span>
                    </div>
                  );
                })}
              </div>
            </Card>
          )}

          {/* Audited Tool-Use Trace */}
          {result.trace && result.trace.length > 0 && (
            <Card style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
              <div>
                <h3 style={{ margin: "0 0 4px 0" }}>Audited Tool-Use Trace</h3>
                <p style={{ margin: 0, color: "var(--color-ink-muted)", fontSize: "13px" }}>
                  Execution log of bounded read-only tool calls evaluated during reasoning.
                </p>
              </div>

              <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
                {result.trace.map((step, idx) => (
                  <div
                    key={idx}
                    style={{
                      padding: "12px",
                      background: "rgba(0,0,0,0.25)",
                      borderRadius: "6px",
                      borderLeft: "3px solid var(--color-primary, #3b82f6)",
                      display: "flex",
                      flexDirection: "column",
                      gap: "6px",
                    }}
                  >
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                      <span style={{ fontWeight: 600, fontSize: "13px" }}>
                        Step {step.step}: {step.action} {step.tool ? `(${step.tool})` : ""}
                      </span>
                      {step.activity_id && (
                        <span style={{ fontSize: "11px", color: "var(--color-ink-muted)" }}>
                          ai_activity: <code>{step.activity_id}</code>
                        </span>
                      )}
                    </div>

                    {step.args && (
                      <div style={{ fontSize: "12px" }}>
                        <strong>Args:</strong> <code>{JSON.stringify(step.args)}</code>
                      </div>
                    )}

                    {step.result && (
                      <div style={{ fontSize: "12px", color: "var(--color-ink-muted)" }}>
                        <strong>Result snippet:</strong>{" "}
                        <code>{step.result.length > 150 ? step.result.slice(0, 150) + "…" : step.result}</code>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}
