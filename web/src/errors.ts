export function message(cause: unknown): string {
  return cause instanceof Error ? cause.message : "The operation failed";
}

export function errorMessage(cause: unknown): string {
  return message(cause);
}
