import { FormEvent, useState } from "react";
import { createPrivacyRequest, downloadPrivacyRequest, getPrivacyRequest, PrivacyRequest, rejectPrivacyRequest, verifyPrivacyRequest } from "../api";
import { Button, Card, ErrorState, Field, Input, Select } from "../components";

export default function Privacy({ apiKey, baseURL }: { apiKey: string; baseURL: string }) {
  const [externalID, setExternalID] = useState(""); const [requestType, setRequestType] = useState<"export" | "delete">("export");
  const [requestID, setRequestID] = useState(""); const [token, setToken] = useState(""); const [reason, setReason] = useState("");
  const [item, setItem] = useState<PrivacyRequest | null>(null); const [error, setError] = useState("");
  async function run(action: () => Promise<PrivacyRequest>) { try { setItem(await action()); setError(""); } catch (cause) { setError(cause instanceof Error ? cause.message : "Request failed"); } }
  async function submit(event: FormEvent) { event.preventDefault(); await run(() => createPrivacyRequest(baseURL, apiKey, externalID, requestType)); }
  async function lookup(event: FormEvent) { event.preventDefault(); await run(() => getPrivacyRequest(baseURL, apiKey, requestID)); }
  async function verify() { if (item) await run(() => verifyPrivacyRequest(baseURL, apiKey, item.id, token)); }
  async function reject() { if (item) await run(() => rejectPrivacyRequest(baseURL, apiKey, item.id, reason)); }
  async function download() { if (!item) return; try { const blob = await downloadPrivacyRequest(baseURL, apiKey, item.id); const url = URL.createObjectURL(blob); const link = document.createElement("a"); link.href = url; link.download = "privacy-export.json"; link.click(); URL.revokeObjectURL(url); } catch (cause) { setError(cause instanceof Error ? cause.message : "Download failed"); } }
  return <section className="stack privacy-console"><div className="section-title"><div><div className="eyebrow">Governed privacy</div><h2>Data-subject requests</h2></div></div>
    {error && <ErrorState description={error} />}
    <div className="acquisition-grid"><Card variant="article"><h3>Request intake</h3><form className="governance-form" onSubmit={submit}><Field id="dsr-external-id" label="External ID"><Input value={externalID} onChange={e => setExternalID(e.target.value)} required /></Field><Field id="dsr-type" label="Request type"><Select value={requestType} onChange={e => setRequestType(e.target.value as "export" | "delete")}><option value="export">Export</option><option value="delete">Erase</option></Select></Field><Button type="submit">Submit privacy request</Button></form></Card>
      <Card variant="article"><h3>Load request</h3><form className="governance-form" onSubmit={lookup}><Field id="dsr-request-id" label="Request ID"><Input value={requestID} onChange={e => setRequestID(e.target.value)} required /></Field><Button type="submit">Load status</Button></form></Card></div>
    {item && <Card variant="article"><div className="section-title"><div><h3>{item.request_type} · {item.status}</h3><p className="muted">{item.external_id} · SLA {item.sla_due_at ? new Date(item.sla_due_at).toLocaleDateString() : "not set"}</p></div></div><div className="governance-form"><Field id="dsr-token" label="Requester verification token"><Input value={token} onChange={e => setToken(e.target.value)} placeholder="Token supplied to requester" /></Field><Button onClick={() => void verify()} disabled={item.verification_status === "verified" || item.status === "rejected"}>Verify and process</Button><Field id="dsr-reason" label="Rejection reason"><Input value={reason} onChange={e => setReason(e.target.value)} /></Field><Button variant="secondary" onClick={() => void reject()} disabled={item.status === "rejected" || item.status === "completed"}>Reject</Button>{item.request_type === "export" && <Button onClick={() => void download()} disabled={item.status !== "completed"}>Download export</Button>}</div></Card>}
  </section>;
}
