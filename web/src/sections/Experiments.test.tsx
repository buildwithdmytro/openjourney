import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import Experiments from "./Experiments";

function response(body: unknown, status = 200) {
  return Promise.resolve({ ok: true, status, json: async () => body });
}

beforeEach(() => cleanup());
afterEach(() => cleanup());

it("round-trips variants, holdout, and a campaign binding", async () => {
  let experiments: Array<Record<string, unknown>> = [];
  let campaign: Record<string, unknown> = {
    id: "campaign-1", tenant_id: "tenant", workspace_id: "workspace", name: "Welcome campaign",
    segment_id: "segment-1", template_id: "template-base", status: "draft", segment_version: 1,
    template_version: 1, recipient_count: 0, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
  };
  const writes: Array<{ url: string; method: string; body: Record<string, unknown> }> = [];
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method || "GET";
    if (url.endsWith("/v1/campaigns") && method === "GET") return response([campaign]);
    if (url.endsWith("/v1/journeys") && method === "GET") return response({ journeys: [] });
    if (url.endsWith("/v1/templates") && method === "GET") return response({ templates: [
      { id: "template-base", name: "Base", channel: "email" },
      { id: "template-blue", name: "Blue CTA", channel: "email" },
    ] });
    if (url.endsWith("/v1/experiments") && method === "GET") return response(experiments);
    if (url.endsWith("/v1/experiments") && method === "POST") {
      const body = JSON.parse(String(init?.body));
      const created = { ...body, id: "experiment-1" };
      experiments = [created];
      writes.push({ url, method, body });
      return response(created, 201);
    }
    if (url.endsWith("/v1/experiments/experiment-1") && method === "GET") return response(experiments[0]);
    if (url.endsWith("/v1/experiments/experiment-1") && method === "PUT") {
      const body = JSON.parse(String(init?.body));
      const updated = { ...experiments[0], ...body, id: "experiment-1" };
      experiments = [updated];
      writes.push({ url, method, body });
      return response(updated);
    }
    if (url.endsWith("/v1/campaigns/campaign-1") && method === "PUT") {
      const body = JSON.parse(String(init?.body));
      campaign = { ...campaign, ...body };
      writes.push({ url, method, body });
      return response(campaign);
    }
    throw new Error(`Unexpected request: ${method} ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("crypto", { randomUUID: () => "fixed-seed" });

  render(<Experiments apiKey="key" baseURL="/api" />);
  await screen.findByText(/No experiments yet/);
  fireEvent.change(screen.getByLabelText("Experiment name"), { target: { value: "CTA test" } });
  fireEvent.change(screen.getByLabelText("Holdout %"), { target: { value: "10" } });
  fireEvent.change(screen.getByLabelText("Variant 2 label"), { target: { value: "blue" } });
  fireEvent.change(screen.getByLabelText("Variant 2 weight"), { target: { value: "60" } });
  fireEvent.change(screen.getByLabelText("Variant 2 template"), { target: { value: "template-blue" } });
  fireEvent.change(screen.getByLabelText("Bind to campaign"), { target: { value: "campaign-1" } });
  fireEvent.click(screen.getByRole("button", { name: "Create experiment" }));

  await screen.findByText("CTA test");
  const createWrite = writes.find((write) => write.method === "POST");
  expect(createWrite?.body).toMatchObject({
    name: "CTA test", seed: "fixed-seed", holdout_pct: 10,
    variants: [
      { label: "control", weight: 50, is_control: true },
      { label: "blue", weight: 60, is_control: false, template_id: "template-blue" },
    ],
  });
  expect(writes.find((write) => write.url.endsWith("/v1/campaigns/campaign-1"))?.body.experiment_id).toBe("experiment-1");

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  await waitFor(() => expect(screen.getByLabelText("Experiment name")).toHaveValue("CTA test"));
  expect(screen.getByLabelText("Variant 2 label")).toHaveValue("blue");
  expect(screen.getByLabelText("Holdout %")).toHaveValue(10);
  fireEvent.change(screen.getByLabelText("Experiment name"), { target: { value: "CTA test revised" } });
  fireEvent.change(screen.getByLabelText("Holdout %"), { target: { value: "15" } });
  fireEvent.click(screen.getByRole("button", { name: "Update experiment" }));

  await screen.findByText("CTA test revised");
  const updateWrite = writes.find((write) => write.url.endsWith("/v1/experiments/experiment-1") && write.method === "PUT");
  expect(updateWrite?.body).toMatchObject({ name: "CTA test revised", holdout_pct: 15 });
  expect(updateWrite?.body.variants).toEqual(expect.arrayContaining([
    expect.objectContaining({ label: "blue", weight: 60, template_id: "template-blue" }),
  ]));
});

it("binds a journey experiment to an editable message node", async () => {
  let journeyWrite: Record<string, unknown> | undefined;
  const journey = {
    id: "journey-1", tenant_id: "tenant", workspace_id: "workspace", name: "Onboarding", status: "draft",
    graph: {
      entry_node_id: "entry",
      nodes: [
        { id: "entry", type: "entry", config: { trigger: "event", event_type: "signup" } },
        { id: "welcome", type: "message", config: { template_id: "template-base" } },
        { id: "exit", type: "exit", config: {} },
      ],
      edges: [{ from: "entry", to: "welcome" }, { from: "welcome", to: "exit" }],
    },
    latest_version: 0, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
  };
  vi.stubGlobal("crypto", { randomUUID: () => "journey-seed" });
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method || "GET";
    if (url.endsWith("/v1/experiments") && method === "GET") return response([]);
    if (url.endsWith("/v1/campaigns") && method === "GET") return response([]);
    if (url.endsWith("/v1/journeys") && method === "GET") return response({ journeys: [journey] });
    if (url.endsWith("/v1/templates") && method === "GET") return response({ templates: [] });
    if (url.endsWith("/v1/experiments") && method === "POST") {
      return response({ ...JSON.parse(String(init?.body)), id: "experiment-journey" }, 201);
    }
    if (url.endsWith("/v1/journeys/journey-1") && method === "PUT") {
      journeyWrite = JSON.parse(String(init?.body));
      return response({ ...journey, ...journeyWrite });
    }
    throw new Error(`Unexpected request: ${method} ${url}`);
  }));

  render(<Experiments apiKey="key" baseURL="/api" />);
  await screen.findByText(/No experiments yet/);
  fireEvent.change(screen.getByLabelText("Experiment name"), { target: { value: "Welcome message test" } });
  fireEvent.change(screen.getByLabelText("Subject type"), { target: { value: "journey" } });
  fireEvent.change(screen.getByLabelText("Bind to journey"), { target: { value: "journey-1" } });
  fireEvent.change(screen.getByLabelText("Journey node"), { target: { value: "welcome" } });
  fireEvent.click(screen.getByRole("button", { name: "Create experiment" }));

  await waitFor(() => expect(journeyWrite).toBeDefined());
  const nodes = (journeyWrite?.graph as { nodes: JourneyNodeForTest[] }).nodes;
  expect(nodes.find((node) => node.id === "welcome")?.config.experiment_id).toBe("experiment-journey");
});

it("reviews a proposal and approves a new immutable version", async () => {
  const experiment = { id: "experiment-1", name: "CTA test", subject_type: "campaign", status: "running", method: "frequentist", seed: "stable-seed", holdout_pct: 12, variants: [] };
  const proposal = { id: "proposal-1", experiment_id: "experiment-1", kind: "winner", winner_variant: "treatment", rationale: "Significant with no guardrail regression.", status: "proposed", report_snapshot: {}, created_at: "2026-01-01T00:00:00Z" };
  const writes: string[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input); const method = init?.method || "GET";
    if (url.endsWith("/v1/experiments") && method === "GET") return response([experiment]);
    if (url.endsWith("/v1/campaigns") && method === "GET") return response([]);
    if (url.endsWith("/v1/journeys") && method === "GET") return response({ journeys: [] });
    if (url.endsWith("/v1/templates") && method === "GET") return response({ templates: [] });
    if (url.endsWith("/v1/experiments/experiment-1/optimize") && method === "POST") { writes.push(url); return response(proposal, 201); }
    if (url.endsWith("/v1/experiments/experiment-1/optimize/proposal-1/approve") && method === "POST") { writes.push(url); return response({ id: "version-2", experiment_id: "experiment-1", version: 2, seed: "stable-seed", holdout_pct: 12, variants: [], approved_by: "user-1", created_at: "2026-01-02T00:00:00Z" }, 201); }
    throw new Error(`Unexpected request: ${method} ${url}`);
  }));
  render(<Experiments apiKey="session-user" baseURL="/api" />);
  await screen.findByText("CTA test");
  fireEvent.click(screen.getByRole("button", { name: "Propose optimization" }));
  await screen.findByText("Winner: treatment");
  fireEvent.click(screen.getByRole("button", { name: "Approve new version" }));
  expect(await screen.findByText("approved")).toBeInTheDocument();
  expect(writes).toEqual([
    "/api/v1/experiments/experiment-1/optimize",
    "/api/v1/experiments/experiment-1/optimize/proposal-1/approve",
  ]);
});

type JourneyNodeForTest = { id: string; config: Record<string, unknown> };
