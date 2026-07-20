import React from "react";

interface PageHeaderProps {
  title: string;
  description: string;
}

export const PageHeader = React.forwardRef<HTMLElement, PageHeaderProps>(
  ({ title, description }, ref) => {
    return (
      <header ref={ref}>
        <p>Platform kernel</p>
        <h1>{title}</h1>
        <span>{description}</span>
      </header>
    );
  }
);

PageHeader.displayName = "PageHeader";
