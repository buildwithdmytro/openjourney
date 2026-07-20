import React from "react";

export interface DataTableProps extends React.TableHTMLAttributes<HTMLTableElement> {
  headers: string[];
  rows: (React.ReactNode | React.ReactNode[])[][];
}

const DataTable = React.forwardRef<HTMLTableElement, DataTableProps>(
  ({ headers, rows, className, ...props }, ref) => {
    return (
      <table
        ref={ref}
        className={`data-table ${className || ""}`.trim()}
        {...props}
      >
        <thead>
          <tr>
            {headers.map((header) => (
              <th key={header}>{header}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, rowIndex) => (
            <tr key={rowIndex}>
              {row.map((cell, cellIndex) => (
                <td key={cellIndex}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    );
  }
);

DataTable.displayName = "DataTable";

export default DataTable;
