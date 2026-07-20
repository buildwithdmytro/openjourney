import React from "react";

export interface SkeletonProps extends React.HTMLAttributes<HTMLDivElement> {
  width?: string | number;
  height?: string | number;
  circle?: boolean;
}

const Skeleton = React.forwardRef<HTMLDivElement, SkeletonProps>(
  ({ width, height, circle, className, ...props }, ref) => {
    const style: React.CSSProperties = {
      ...(props.style || {}),
      width: width || "100%",
      height: height || "20px",
      borderRadius: circle ? "50%" : undefined,
    };

    return (
      <div
        ref={ref}
        className={`skeleton ${className || ""}`.trim()}
        style={style}
        {...props}
      />
    );
  }
);

Skeleton.displayName = "Skeleton";

export default Skeleton;
