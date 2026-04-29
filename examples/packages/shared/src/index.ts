export function createTraceHeaders(traceId: string): Record<string, string> {
  return {
    "x-trace-id": traceId,
    "x-platform": "example-platform",
  };
}