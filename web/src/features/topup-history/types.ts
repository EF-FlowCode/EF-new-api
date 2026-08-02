export type TopupRecord = {
  id: number
  user_id?: number
  trade_no: string
  amount: number
  money: number
  payment_method?: string
  status: string
  create_time: number
  // Optional fields the backend may or may not include
  username?: string
  pay_amount?: number
}

export type TopupListResponse = {
  items: TopupRecord[]
  total: number
}

export type TopupStatus = '' | 'success' | 'pending' | 'failed' | 'expired'
