// Only transport failures and transient HTTP failures justify automatic reads.
export function isRetryableReadError(error: unknown): boolean {
  const failure = error as { response?: { status?: number }; status?: number } | null
  const status = failure?.response?.status ?? failure?.status
  return !status || status === 408 || status === 429 || status >= 500
}
