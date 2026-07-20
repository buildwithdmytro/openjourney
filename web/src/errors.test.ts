import { describe, it, expect } from "vitest";
import { message, errorMessage } from "./errors";

describe("errors", () => {
  it("message() converts Error to string", () => {
    const err = new Error("Test error");
    expect(message(err)).toBe("Test error");
  });

  it("message() returns default string for non-Error", () => {
    expect(message("unknown")).toBe("The operation failed");
    expect(message(123)).toBe("The operation failed");
    expect(message(null)).toBe("The operation failed");
  });

  it("errorMessage() is an alias for message()", () => {
    const err = new Error("Test");
    expect(errorMessage(err)).toBe(message(err));
  });
});
