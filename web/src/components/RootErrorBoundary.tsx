import { Component, ErrorInfo, ReactNode } from "react";
import ErrorState from "./ErrorState";

interface RootErrorBoundaryProps {
  children: ReactNode;
}

interface RootErrorBoundaryState {
  error: Error | null;
}

export class RootErrorBoundary extends Component<RootErrorBoundaryProps, RootErrorBoundaryState> {
  state: RootErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): RootErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("OpenJourney root UI failed", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <ErrorState
          role="alert"
          title="OpenJourney could not load"
          description="The application hit an unexpected error. Try again to recover your workspace."
          onRetry={() => this.setState({ error: null })}
        />
      );
    }

    return this.props.children;
  }
}

export default RootErrorBoundary;
