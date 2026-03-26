import { apiFetch } from './client'

export interface DigestRequest {
  from?: string       // RFC3339
  to?: string         // RFC3339
  rule_id?: number
  source_id?: number
  entity_id?: number
  limit?: number
}

export interface DigestResponse {
  digest: string
  tweet_count: number
}

export function runDigest(req: DigestRequest) {
  return apiFetch<DigestResponse>('/api/digest', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}
