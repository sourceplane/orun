export function createPlatformClient(baseUrl: string): string {
  return new URL("/v1", baseUrl).toString();
}