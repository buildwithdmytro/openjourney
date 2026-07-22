import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ToastProvider } from "../components";
import Prompts from "./Prompts";

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

describe("Prompts section", () => {
  it("lists prompts, creates a prompt and version, runs eval, and publishes via ConfirmDialog", async () => {
    const mockPrompt = {
      id: "prompt-1",
      tenant_id: "tenant-1",
      workspace_id: "ws-1",
      name: "Welcome Email Prompt",
      task_type: "content_draft",
      latest_version: 1,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    const mockVersion = {
      id: "version-1",
      prompt_id: "prompt-1",
      tenant_id: "tenant-1",
      version: 1,
      template: "Hello {{name}}",
      input_schema: {},
      output_schema: {},
      provider: "mock",
      model: "mock-model",
      params: {},
      safety_policy: {},
      status: "draft",
      eval_status: "pending",
      created_at: new Date().toISOString(),
    };

    const mockEvaluatedVersion = {
      ...mockVersion,
      eval_status: "passed",
    };

    const mockPublishedVersion = {
      ...mockVersion,
      status: "active",
      eval_status: "passed",
      published_by: "user-1",
      published_at: new Date().toISOString(),
    };

    let promptCreated = false;
    let versionCreated = false;
    let evalRan = false;
    let published = false;

    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method || "GET";

      if (url.endsWith("/v1/ai/prompts") && method === "GET") {
        return response({ prompts: promptCreated ? [mockPrompt] : [] });
      }
      if (url.endsWith("/v1/ai/prompts") && method === "POST") {
        promptCreated = true;
        return response(mockPrompt, 201);
      }
      if (url.includes("/v1/ai/prompts/prompt-1/versions") && method === "GET") {
        if (published) return response({ versions: [mockPublishedVersion] });
        if (evalRan) return response({ versions: [mockEvaluatedVersion] });
        return response({ versions: versionCreated ? [mockVersion] : [] });
      }
      if (url.includes("/v1/ai/prompts/prompt-1/versions") && method === "POST" && !url.endsWith("/eval") && !url.endsWith("/publish")) {
        versionCreated = true;
        return response(mockVersion, 201);
      }
      if (url.includes("/v1/ai/prompts/prompt-1/versions/1/eval") && method === "POST") {
        evalRan = true;
        return response(mockEvaluatedVersion, 200);
      }
      if (url.includes("/v1/ai/prompts/prompt-1/versions/1/publish") && method === "POST") {
        published = true;
        return response(mockPublishedVersion, 200);
      }

      throw new Error(`Unexpected request: ${method} ${url}`);
    });

    vi.stubGlobal("fetch", fetchMock);

    render(
      <ToastProvider>
        <Prompts apiKey="key" baseURL="/api" />
      </ToastProvider>
    );

    // 1. Initial render shows title and empty state
    expect(screen.getByText("Prompt Management")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("No prompts yet")).toBeInTheDocument());

    // 2. Create prompt
    fireEvent.click(screen.getByRole("button", { name: "New Prompt" }));
    fireEvent.change(screen.getByPlaceholderText("e.g. Content Draft Prompt"), {
      target: { value: "Welcome Email Prompt" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Prompt" }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/v1/ai/prompts"),
        expect.objectContaining({ method: "POST" })
      )
    );

    // 3. Prompt appears and is selected
    await waitFor(() => expect(screen.getAllByText("Welcome Email Prompt").length).toBeGreaterThan(0));

    // 4. Author version
    fireEvent.click(screen.getByRole("button", { name: "Author New Version" }));
    fireEvent.change(screen.getByPlaceholderText("Enter the prompt template text..."), {
      target: { value: "Hello {{name}}" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Version" }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/v1/ai/prompts/prompt-1/versions"),
        expect.objectContaining({ method: "POST" })
      )
    );

    // 5. Version v1 renders with Run Eval button
    await waitFor(() => expect(screen.getByText("v1")).toBeInTheDocument());
    expect(screen.getByText("Hello {{name}}")).toBeInTheDocument();

    // 6. Run Eval
    fireEvent.click(screen.getByRole("button", { name: "Run Eval" }));
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/v1/ai/prompts/prompt-1/versions/1/eval"),
        expect.objectContaining({ method: "POST" })
      )
    );

    // 7. Publish version via ConfirmDialog
    fireEvent.click(screen.getByRole("button", { name: "Publish" }));

    // ConfirmDialog opens
    expect(screen.getByText("Publish Prompt Version")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Publish Version" }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/v1/ai/prompts/prompt-1/versions/1/publish"),
        expect.objectContaining({ method: "POST" })
      )
    );

    // 8. Version status is updated to Published
    await waitFor(() => expect(screen.getByText("Published")).toBeInTheDocument());
  });
});
