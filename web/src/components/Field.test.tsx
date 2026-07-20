import { cleanup, render, screen } from "@testing-library/react";
import { beforeEach, describe, it, expect } from "vitest";
import Field from "./Field";
import Input from "./Input";
import Select from "./Select";

beforeEach(cleanup);

describe("Field", () => {
  it("associates label with input via htmlFor", () => {
    render(
      <Field id="test-field" label="Test Label">
        <Input />
      </Field>
    );
    const label = screen.getByText("Test Label");
    const input = screen.getByRole("textbox");
    expect(label).toHaveAttribute("for", "test-field");
    expect(input).toHaveAttribute("id", "test-field");
  });

  it("generates id if not provided", () => {
    render(
      <Field label="Test Label">
        <Input />
      </Field>
    );
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute("id");
  });

  it("displays help text with proper aria-describedby", () => {
    render(
      <Field id="field1" label="Email" help="Enter your email address">
        <Input type="email" />
      </Field>
    );
    const help = screen.getByText("Enter your email address");
    const input = screen.getByRole("textbox");
    expect(help).toHaveAttribute("id", "field1-help");
    expect(input).toHaveAttribute("aria-describedby", "field1-help");
  });

  it("displays error text with proper aria-describedby", () => {
    render(
      <Field id="field2" label="Password" error="Password is required">
        <Input type="password" />
      </Field>
    );
    const error = screen.getByText("Password is required");
    const input = screen.getByLabelText("Password");
    expect(error).toHaveAttribute("id", "field2-error");
    expect(input).toHaveAttribute("aria-describedby", "field2-error");
    expect(input).toHaveAttribute("aria-invalid", "true");
  });

  it("combines help and error in aria-describedby", () => {
    render(
      <Field
        id="field3"
        label="Username"
        help="3-20 characters"
        error="This username is taken"
      >
        <Input />
      </Field>
    );
    const input = screen.getByRole("textbox");
    expect(input).toHaveAttribute(
      "aria-describedby",
      "field3-help field3-error"
    );
  });

  it("displays required indicator when required is true", () => {
    render(
      <Field id="field4" label="Name" required>
        <Input />
      </Field>
    );
    const required = screen.getByLabelText("required");
    expect(required).toHaveTextContent("*");
  });

  it("does not display required indicator when required is false", () => {
    render(
      <Field id="field5" label="Optional Field">
        <Input />
      </Field>
    );
    const label = screen.getByText("Optional Field");
    expect(label.parentElement).not.toHaveTextContent("*");
  });

  it("works with select input", () => {
    render(
      <Field id="field6" label="Country">
        <Select>
          <option value="us">United States</option>
          <option value="uk">United Kingdom</option>
        </Select>
      </Field>
    );
    const label = screen.getByText("Country");
    const select = screen.getByRole("combobox");
    expect(label).toHaveAttribute("for", "field6");
    expect(select).toHaveAttribute("id", "field6");
  });
});
