import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ToastProvider } from "../components";
import Assistant from "./Assistant";

const response = (body: unknown, status = 200) =>
  Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    })
  );

beforeEach(() => {
  const modalRoot = document.createElement("div");
  modalRoot.setAttribute("id", "modal-root");
  document.body.appendChild(modalRoot);
});

afterEach(() => {
  cleanup();
  const modalRoot = document.getElementById("modal-root");
  if (modalRoot) {
    document.body.removeChild(modalRoot);
  }
});

describe("Assistant section", () => {
  it("renders assistant view, allows asking question, and displays grounded answer with tool trace", async () => {
    const mockInsightsResponse = {
      summary: "Retention rate is steady at 85% with high funnel conversion.",
      insights: ["Funnel conversion increased by 12%", "Cost per conversion remained flat"],
      key_metrics: [
        { name: "Retention Rate", value: 0.85, source: "retention_report" },
        { name: "Conversion Rate", value: 0.42, source: "funnel_report" },
      ],
      activity_id: "act-12345",
      trace: [
        {
          step: 1,
          action: "tool",
          tool: "report.timeseries",
          args: { report_type: "retention" },
          result: '{"rate": 0.85}',
          activity_id: "act-step-1",
        },
        {
          step: 2,
          action: "final",
          activity_id: "act-step-2",
        },
      ],
      status: "completed",
    };

    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method || "GET";

      if (url.endsWith("/v1/ai/copilots/insights") && method === "POST") {
        return response(mockInsightsResponse);
      }
      return response({ error: "not found" }, 404);
    });

    vi.stubGlobal("fetch", fetchMock);

    render(
      <ToastProvider>
        <Assistant apiKey="test-key" baseURL="http://localhost:8080" />
      </ToastProvider>
    );

    expect(screen.getByText("Conversational Analytics Assistant")).toBeInTheDocument();
    expect(screen.getByText("No query performed yet")).toBeInTheDocument();

    const input = screen.getByPlaceholderText(/e\.g\. What is our retention rate/i);
    fireEvent.change(input, { target: { value: "What is our retention rate?" } });

    const submitBtn = screen.getByRole("button", { name: /Ask Assistant/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(screen.getByText("Retention rate is steady at 85% with high funnel conversion.")).toBeInTheDocument();
    });

    expect(screen.getByText("Funnel conversion increased by 12%")).toBeInTheDocument();
    expect(screen.getByText("Retention Rate")).toBeInTheDocument();
    expect(screen.getByText("0.85")).toBeInTheDocument();
    expect(screen.getByText("Source: retention_report")).toBeInTheDocument();

    expect(screen.getByText("Audited Tool-Use Trace")).toBeInTheDocument();
    expect(screen.getByText(/Step 1: tool \(report\.timeseries\)/i)).toBeInTheDocument();
    expect(screen.getByText("act-12345")).toBeInTheDocument();
  });
});
