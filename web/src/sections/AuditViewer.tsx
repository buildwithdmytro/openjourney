import { FormEvent, useEffect, useState } from "react";
import { AuditEvent, AuditFilter, AuditVerificationResult, listAuditEvents, verifyAuditChain } from "../api";
import { Badge, Button, Card, DataTable, ErrorState, Field, Input, Spinner } from "../components";

function message(error: unknown) {
  return error instanceof Error ? error.message : "Request failed";
}

export default function AuditViewer({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [verification, setVerification] = useState<AuditVerificationResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [error, setError] = useState("");

  const [actorId, setActorId] = useState("");
  const [resourceType, setResourceType] = useState("");
  const [action, setAction] = useState("");
  const [startTime, setStartTime] = useState("");
  const [endTime, setEndTime] = useState("");

  async function loadEvents(filters?: AuditFilter) {
    setLoading(true);
    setError("");
    try {
      const data = await listAuditEvents(baseURL, apiKey, filters);
      setEvents(data);
    } catch (err) {
      setError(message(err));
    } finally {
      setLoading(false);
    }
  }

  async function checkChain() {
    setVerifying(true);
    try {
      const res = await verifyAuditChain(baseURL, apiKey);
      setVerification(res);
    } catch (err) {
      setError(message(err));
    } finally {
      setVerifying(false);
    }
  }

  useEffect(() => {
    if (apiKey) {
      void loadEvents();
      void checkChain();
    }
  }, [apiKey, baseURL]);

  function handleFilter(e: FormEvent) {
    e.preventDefault();
    const filter: AuditFilter = {};
    if (actorId.trim()) filter.actor_id = actorId.trim();
    if (resourceType.trim()) filter.resource_type = resourceType.trim();
    if (action.trim()) filter.action = action.trim();
    if (startTime) filter.start_time = new Date(startTime).toISOString();
    if (endTime) filter.end_time = new Date(endTime).toISOString();
    void loadEvents(filter);
  }

  function handleReset() {
    setActorId("");
    setResourceType("");
    setAction("");
    setStartTime("");
    setEndTime("");
    void loadEvents();
  }

  return (
    <section className="stack audit-viewer-section">
      <Card variant="article">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <div>
            <div className="eyebrow">Tamper-evident log</div>
            <h2>Audit log viewer</h2>
          </div>
          <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
            {verification && (
              <Badge kind={verification.intact ? "success" : "danger"}>
                {verification.intact ? "Chain Intact" : `Tampered (Seq ${verification.first_broken_seq ?? "unknown"})`}
              </Badge>
            )}
            <Button variant="secondary" onClick={() => void checkChain()} disabled={verifying}>
              {verifying ? <Spinner size="sm" /> : "Verify chain"}
            </Button>
          </div>
        </div>

        <form onSubmit={handleFilter} style={{ marginTop: "16px", display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: "12px" }}>
          <Field id="actor_id" label="Actor ID">
            <Input value={actorId} onChange={e => setActorId(e.target.value)} placeholder="e.g. user-123" />
          </Field>
          <Field id="resource_type" label="Resource type">
            <Input value={resourceType} onChange={e => setResourceType(e.target.value)} placeholder="e.g. journey" />
          </Field>
          <Field id="action" label="Action">
            <Input value={action} onChange={e => setAction(e.target.value)} placeholder="e.g. publish" />
          </Field>
          <Field id="start_time" label="Start time">
            <Input type="datetime-local" value={startTime} onChange={e => setStartTime(e.target.value)} />
          </Field>
          <Field id="end_time" label="End time">
            <Input type="datetime-local" value={endTime} onChange={e => setEndTime(e.target.value)} />
          </Field>
          <div style={{ display: "flex", gap: "8px", alignItems: "flex-end" }}>
            <Button type="submit" disabled={loading}>
              Filter
            </Button>
            <Button type="button" variant="secondary" onClick={handleReset} disabled={loading}>
              Reset
            </Button>
          </div>
        </form>

        {error && <ErrorState description={error} style={{ marginTop: "16px" }} />}

        <div style={{ marginTop: "24px" }}>
          {loading ? (
            <Spinner />
          ) : (
            <DataTable
              headers={["Seq", "Occurred at", "Actor", "Action", "Resource", "Row hash"]}
              rows={events.map(ev => [
                ev.seq ? String(ev.seq) : "-",
                new Date(ev.occurred_at).toLocaleString(),
                `${ev.actor_type}:${ev.actor_id}`,
                ev.action,
                `${ev.resource_type}${ev.resource_id ? ":" + ev.resource_id : ""}`,
                ev.row_hash ? `${ev.row_hash.slice(0, 12)}...` : "-",
              ])}
            />
          )}
        </div>
      </Card>
    </section>
  );
}
