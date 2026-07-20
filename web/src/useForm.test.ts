import { renderHook, act } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { useForm } from "./useForm";

describe("useForm", () => {
  it("initializes with provided values", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "Test", email: "" },
      })
    );

    expect(result.current.values).toEqual({ name: "Test", email: "" });
  });

  it("updates a field value", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "", email: "" },
      })
    );

    act(() => {
      result.current.setValue("name", "John Doe");
    });

    expect(result.current.values.name).toBe("John Doe");
  });

  it("validates a field on change", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "" },
        validate: {
          name: (value) => {
            if (!value) return "Name is required";
            if (value.length < 3) return "Name must be at least 3 characters";
            return undefined;
          },
        },
      })
    );

    act(() => {
      result.current.setValue("name", "");
    });

    expect(result.current.errors.name).toBe("Name is required");

    act(() => {
      result.current.setValue("name", "Jo");
    });

    expect(result.current.errors.name).toBe("Name must be at least 3 characters");

    act(() => {
      result.current.setValue("name", "John");
    });

    expect(result.current.errors.name).toBeUndefined();
  });

  it("tracks touched fields", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "" },
      })
    );

    expect(result.current.touched.has("name")).toBe(false);

    act(() => {
      result.current.touch("name");
    });

    expect(result.current.touched.has("name")).toBe(true);
  });

  it("only returns error for touched fields", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "", email: "" },
        validate: {
          name: (value) => (!value ? "Name is required" : undefined),
          email: (value) => (!value ? "Email is required" : undefined),
        },
      })
    );

    // Set invalid values but don't touch
    act(() => {
      result.current.setValue("name", "");
      result.current.setValue("email", "");
    });

    // Errors exist in the errors object
    expect(result.current.errors.name).toBe("Name is required");
    expect(result.current.errors.email).toBe("Email is required");

    // But getError only returns for touched fields
    expect(result.current.getError("name")).toBeUndefined();
    expect(result.current.getError("email")).toBeUndefined();

    act(() => {
      result.current.touch("name");
    });

    expect(result.current.getError("name")).toBe("Name is required");
    expect(result.current.getError("email")).toBeUndefined();
  });

  it("determines isValid based on presence of errors", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "" },
        validate: {
          name: (value) => (!value ? "Name is required" : undefined),
        },
      })
    );

    expect(result.current.isValid).toBe(false);

    act(() => {
      result.current.setValue("name", "John");
    });

    expect(result.current.isValid).toBe(true);
  });

  it("resets to initial values", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "Initial", email: "" },
      })
    );

    act(() => {
      result.current.setValue("name", "Changed");
      result.current.touch("name");
    });

    expect(result.current.values.name).toBe("Changed");
    expect(result.current.touched.has("name")).toBe(true);

    act(() => {
      result.current.reset();
    });

    expect(result.current.values.name).toBe("Initial");
    expect(result.current.touched.size).toBe(0);
  });

  it("handles handleChange event", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "", count: 0 },
      })
    );

    act(() => {
      const event = {
        target: { name: "name", value: "John", type: "text" },
      } as React.ChangeEvent<HTMLInputElement>;
      result.current.handleChange(event);
    });

    expect(result.current.values.name).toBe("John");

    act(() => {
      const event = {
        target: { name: "count", value: "42", type: "number" },
      } as React.ChangeEvent<HTMLInputElement>;
      result.current.handleChange(event);
    });

    expect(result.current.values.count).toBe(42);
  });

  it("handles handleBlur event", () => {
    const { result } = renderHook(() =>
      useForm({
        initialValues: { name: "" },
      })
    );

    act(() => {
      const event = {
        target: { name: "name" },
      } as React.FocusEvent<HTMLInputElement>;
      result.current.handleBlur(event);
    });

    expect(result.current.touched.has("name")).toBe(true);
  });
});
