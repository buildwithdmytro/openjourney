import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import Experiments from "./Experiments";

it("lists and creates experiments", async () => {
  let created = false;
  const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === "POST") { created = true; return Promise.resolve({ ok: true, status: 201, json: async () => ({ id: "exp-2" }) }); }
    return Promise.resolve({ ok: true, status: 200, json: async () => created ? [{ id: "exp-2", name: "CTA test", subject_type: "campaign", status: "draft", method: "frequentist", seed: "seed", holdout_pct: 0 }] : [{ id: "exp-1", name: "Subject lines", subject_type: "campaign", status: "draft", method: "frequentist", seed: "seed", holdout_pct: 0 }] });
  });
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("crypto", { randomUUID: () => "fixed-seed" });
  render(<Experiments apiKey="key" baseURL="/api" />);
  await screen.findByText("Subject lines");
  fireEvent.change(screen.getByLabelText("Experiment name"), { target: { value: "CTA test" } });
  fireEvent.click(screen.getByRole("button", { name: "Create experiment" }));
  await screen.findByText("CTA test");
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/v1/experiments"), expect.objectContaining({ method: "POST", body: expect.stringContaining("fixed-seed") })));
});
