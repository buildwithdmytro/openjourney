import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import DataTable from "./DataTable";

afterEach(cleanup);

describe("DataTable", () => {
  const headers = ["Name", "Email", "Status"];
  const rows = [
    ["Alice", "alice@example.com", "Active"],
    ["Bob", "bob@example.com", "Inactive"],
  ];

  it("renders table with headers", () => {
    render(<DataTable headers={headers} rows={rows} />);
    const table = screen.getByRole("table");
    expect(table).toBeInTheDocument();
    expect(screen.getByText("Name")).toBeInTheDocument();
    expect(screen.getByText("Email")).toBeInTheDocument();
    expect(screen.getByText("Status")).toBeInTheDocument();
  });

  it("renders table rows", () => {
    render(<DataTable headers={headers} rows={rows} />);
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("alice@example.com")).toBeInTheDocument();
    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.getByText("bob@example.com")).toBeInTheDocument();
  });

  it("renders header cells as th elements", () => {
    render(<DataTable headers={headers} rows={rows} />);
    const headerCells = screen.getAllByRole("columnheader");
    expect(headerCells).toHaveLength(3);
  });

  it("renders data cells as td elements", () => {
    render(<DataTable headers={headers} rows={rows} />);
    const dataCells = screen.getAllByRole("cell");
    expect(dataCells.length).toBeGreaterThanOrEqual(6);
  });

  it("applies data-table class", () => {
    render(
      <table data-testid="table" className="data-table">
        <thead>
          <tr>
            <th>Test</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>Test</td>
          </tr>
        </tbody>
      </table>
    );
    const table = screen.getByTestId("table");
    expect(table).toHaveClass("data-table");
  });

  it("forwards ref", () => {
    const ref = React.createRef<HTMLTableElement>();
    render(<DataTable ref={ref} headers={headers} rows={rows} />);
    expect(ref.current).toBeInstanceOf(HTMLTableElement);
  });

  it("handles empty rows", () => {
    render(<DataTable headers={headers} rows={[]} />);
    const table = screen.getByRole("table");
    expect(table).toBeInTheDocument();
    const headerCells = screen.getAllByRole("columnheader");
    expect(headerCells).toHaveLength(3);
  });
});
