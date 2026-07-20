import React from "react";

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: "default" | "article";
}

const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ variant = "default", className, children, ...props }, ref) => {
    const tag = variant === "article" ? "article" : "div";
    const Tag = tag as any;

    return (
      <Tag
        ref={ref}
        className={`card ${className || ""}`.trim()}
        {...props}
      >
        {children}
      </Tag>
    );
  }
);

Card.displayName = "Card";

export default Card;
