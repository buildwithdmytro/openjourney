import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import Select from "./Select";

afterEach(cleanup);

describe("Select", () => {
  it("renders a select element", () => {
    render(
      <Select>
        <option value="a">Option A</option>
        <option value="b">Option B</option>
      </Select>
    );
    const select = screen.getByRole("combobox");
    expect(select).toBeInTheDocument();
  });

  it("renders options from children", () => {
    render(
      <Select>
        <option value="a">Option A</option>
        <option value="b">Option B</option>
      </Select>
    );
    expect(screen.getByText("Option A")).toBeInTheDocument();
    expect(screen.getByText("Option B")).toBeInTheDocument();
  });

  it("renders options from options prop", () => {
    render(
      <Select
        options={[
          { value: "a", label: "Option A" },
          { value: "b", label: "Option B" },
        ]}
      />
    );
    expect(screen.getByText("Option A")).toBeInTheDocument();
    expect(screen.getByText("Option B")).toBeInTheDocument();
  });

  it("forwards ref", () => {
    const ref = { current: null };
    render(
      <Select ref={ref}>
        <option value="a">Option A</option>
      </Select>
    );
    expect(ref.current).toBeInstanceOf(HTMLSelectElement);
  });

  it("sets aria-invalid when error prop is provided", () => {
    render(
      <Select error="This field is required">
        <option value="">Select</option>
      </Select>
    );
    const select = screen.getByRole("combobox");
    expect(select).toHaveAttribute("aria-invalid", "true");
  });

  it("does not set aria-invalid when no error", () => {
    render(
      <Select>
        <option value="">Select</option>
      </Select>
    );
    const select = screen.getByRole("combobox");
    expect(select).not.toHaveAttribute("aria-invalid");
  });

  it("has focus-visible styling", () => {
    render(
      <Select>
        <option>Select</option>
      </Select>
    );
    const select = screen.getByRole("combobox");
    expect(select).toHaveClass("select");
  });
});
