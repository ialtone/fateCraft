export interface Account {
  id: number
  player_id: string
  rewind_count: number
  total_rewinds_earned: number
  rewinds_used: number
  total_spirit_stones: number
  successful_trials: number
  failed_trials: number
  created_at: string
  updated_at: string
}
