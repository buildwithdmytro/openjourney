import { cleanup, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import JsonField from "./JsonField";

afterEach(cleanup);

describe("JsonField", () => {
  it("renders a textarea element", () => {
    const onChange = vi.fn();
    render(<JsonField value="{}" onChange={onChange} />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toBeInTheDocument();
  });

  it("displays the provided value", () => {
    const onChange = vi.fn();
    const jsonValue = '{"key": "value"}';
    render(<JsonField value={jsonValue} onChange={onChange} />);
    const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
    expect(textarea.value).toBe(jsonValue);
  });

  it("calls onChange when user types", () => {
    const onChange = vi.fn();
    render(<JsonField value="" onChange={onChange} />);
    const textarea = screen.getByRole("textbox");
    fireEvent.change(textarea, { target: { value: '{"test": "data"}' } });
    expect(onChange).toHaveBeenCalled();
  });

  it("validates valid JSON on blur", async () => {
    const onChange = vi.fn();
    const onBlur = vi.fn();
    render(
      <JsonField value='{"key":"value"}' onChange={onChange} onBlur={onBlur} />
    );
    const textarea = screen.getByRole("textbox");
    fireEvent.blur(textarea);

    await waitFor(() => {
      expect(onBlur).toHaveBeenCalled();
    });

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows error for invalid JSON on blur", async () => {
    const onChange = vi.fn();
    render(<JsonField value='{"invalid": json}' onChange={onChange} />);
    const textarea = screen.getByRole("textbox");
    fireEvent.blur(textarea);

    await waitFor(() => {
      const alert = screen.getByRole("alert");
      expect(alert).toBeInTheDocument();
      expect(alert.textContent).toContain("Invalid JSON");
    });
  });

  it("sets aria-invalid when there's an error", async () => {
    const onChange = vi.fn();
    render(<JsonField value='invalid' onChange={onChange} />);
    const textarea = screen.getByRole("textbox");
    fireEvent.blur(textarea);

    await waitFor(() => {
      expect(textarea).toHaveAttribute("aria-invalid", "true");
    });
  });

  it("displays provided error prop", () => {
    const onChange = vi.fn();
    render(<JsonField value="{}" onChange={onChange} error="Custom error" />);
    const alert = screen.getByRole("alert");
    expect(alert).toBeInTheDocument();
    expect(alert.textContent).toBe("Custom error");
  });

  it("has a format button that pretty-prints JSON", async () => {
    const onChange = vi.fn();
    render(<JsonField value='{"a":1,"b":2}' onChange={onChange} />);
    const formatBtn = screen.getByRole("button", { name: /format/i });
    expect(formatBtn).toBeInTheDocument();
    fireEvent.click(formatBtn);

    await waitFor(() => {
      expect(onChange).toHaveBeenCalled();
    });
  });

  it("format button shows error for invalid JSON", async () => {
    const onChange = vi.fn();
    render(<JsonField value='invalid json' onChange={onChange} />);
    const formatBtn = screen.getByRole("button", { name: /format/i });
    fireEvent.click(formatBtn);

    await waitFor(() => {
      const alert = screen.getByRole("alert");
      expect(alert).toBeInTheDocument();
      expect(alert.textContent).toContain("Invalid JSON");
    });
  });

  it("forwards ref", () => {
    const ref = { current: null };
    const onChange = vi.fn();
    render(<JsonField ref={ref} value="{}" onChange={onChange} />);
    expect(ref.current).toBeInstanceOf(HTMLTextAreaElement);
  });

  it("does not validate on blur when validateOnBlur is false", async () => {
    const onChange = vi.fn();
    render(<JsonField value='invalid' onChange={onChange} validateOnBlur={false} />);
    const textarea = screen.getByRole("textbox");
    fireEvent.blur(textarea);

    await waitFor(() => {
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });
  });

  it("passes through other textarea attributes", () => {
    const onChange = vi.fn();
    render(<JsonField value="{}" onChange={onChange} placeholder="Enter JSON" rows={5} />);
    const textarea = screen.getByRole("textbox");
    expect(textarea).toHaveAttribute("placeholder", "Enter JSON");
    expect(textarea).toHaveAttribute("rows", "5");
  });

  it("renders format button", () => {
    const onChange = vi.fn();
    render(<JsonField value="{}" onChange={onChange} />);
    const formatBtn = screen.getByRole("button", { name: /format/i });
    expect(formatBtn).toBeInTheDocument();
  });
});
