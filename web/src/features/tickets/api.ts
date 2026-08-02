import { api } from '@/lib/api'
import type {
  CreateInvoiceTicketPayload,
  CreateRefundTicketPayload,
  CreateTicketPayload,
  EligibleTopUpOrder,
  InvoiceStatus,
  Ticket,
  TicketDetail,
  TicketListResponse,
  TicketStatus,
  UpdateRefundStatusPayload,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

/** 可排序列，与后端 resolveTicketOrder 白名单一一对应。 */
export type TicketSortField =
  | 'id'
  | 'created_time'
  | 'updated_time'
  | 'status'
  | 'priority'

export type TicketListParams = {
  page: number
  pageSize: number
  status?: TicketStatus | ''
  type?: string
  admin?: boolean
  orderBy?: TicketSortField
  order?: 'asc' | 'desc'
}

function buildQuery(params: TicketListParams): string {
  const search = new URLSearchParams()
  search.set('p', String(params.page))
  search.set('page_size', String(params.pageSize))
  if (params.status !== undefined && params.status !== '') {
    search.set('status', String(params.status))
  }
  if (params.type) search.set('type', params.type)
  if (params.orderBy) {
    search.set('order_by', params.orderBy)
    search.set('order', params.order === 'asc' ? 'asc' : 'desc')
  }
  return search.toString()
}

export async function fetchTickets(
  params: TicketListParams
): Promise<TicketListResponse> {
  const base = params.admin ? '/api/ticket/admin' : '/api/ticket/self'
  const res = await api.get<ApiResponse<TicketListResponse>>(
    `${base}?${buildQuery(params)}`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load tickets')
  }
  return res.data.data
}

export async function fetchTicketDetail(
  ticketId: number,
  admin: boolean
): Promise<TicketDetail> {
  const path = admin
    ? `/api/ticket/admin/${ticketId}`
    : `/api/ticket/self/${ticketId}`
  const res = await api.get<ApiResponse<TicketDetail>>(path)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load ticket')
  }
  return res.data.data
}

export async function postTicketReply(
  ticketId: number,
  content: string,
  admin: boolean,
  attachmentIds: number[] = []
): Promise<boolean> {
  const path = admin
    ? `/api/ticket/admin/${ticketId}/message`
    : `/api/ticket/self/${ticketId}/message`
  const res = await api.post<ApiResponse<unknown>>(path, {
    content,
    attachment_ids: attachmentIds,
  })
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to send reply')
  }
  return true
}

export async function closeTicket(
  ticketId: number,
  admin: boolean
): Promise<boolean> {
  const path = admin
    ? `/api/ticket/admin/${ticketId}/status`
    : `/api/ticket/self/${ticketId}/close`
  const res = admin
    ? await api.put<ApiResponse<unknown>>(path, { status: 4 })
    : await api.put<ApiResponse<unknown>>(path)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to close ticket')
  }
  return true
}

export async function adminUpdateTicketStatus(
  ticketId: number,
  status: TicketStatus
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    `/api/ticket/admin/${ticketId}/status`,
    { status }
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update ticket status')
  }
  return true
}

export type BatchUpdateStatusResult = {
  updated: number
  updated_ids: number[]
  failed: number
  failed_ids: number[]
  requested: number
}

/** 批量把选中工单改为同一状态（管理/客服侧，典型用于批量关闭）。 */
export async function batchAdminUpdateTicketStatus(
  ids: number[],
  status: TicketStatus
): Promise<BatchUpdateStatusResult> {
  const res = await api.put<ApiResponse<BatchUpdateStatusResult>>(
    '/api/ticket/admin/batch-status',
    { ids, status }
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update ticket status')
  }
  return res.data.data
}

export async function createTicket(
  payload: CreateTicketPayload
): Promise<Ticket | null> {
  const res = await api.post<ApiResponse<Ticket>>('/api/ticket/', payload)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to create ticket')
  }
  return res.data.data ?? null
}

// ----------------------------------------------------------------------------
// Refund flow
// ----------------------------------------------------------------------------

export async function createRefundTicket(
  payload: CreateRefundTicketPayload
): Promise<Ticket | null> {
  const res = await api.post<ApiResponse<Ticket>>(
    '/api/ticket/refund/',
    payload
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to create refund ticket')
  }
  return res.data.data ?? null
}

export async function updateRefundStatus(
  ticketId: number,
  payload: UpdateRefundStatusPayload
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    `/api/ticket/admin/${ticketId}/refund/status`,
    payload
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update refund status')
  }
  return true
}

// ----------------------------------------------------------------------------
// Invoice flow
// ----------------------------------------------------------------------------

export async function fetchEligibleInvoiceOrders(): Promise<
  EligibleTopUpOrder[]
> {
  const res = await api.get<ApiResponse<EligibleTopUpOrder[] | null>>(
    '/api/ticket/invoice/eligible_orders'
  )
  if (!res.data.success) {
    throw new Error(
      res.data.message || 'Failed to load orders eligible for invoice'
    )
  }
  return res.data.data ?? []
}

export async function createInvoiceTicket(
  payload: CreateInvoiceTicketPayload
): Promise<Ticket | null> {
  const res = await api.post<ApiResponse<Ticket>>(
    '/api/ticket/invoice/',
    payload
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to create invoice ticket')
  }
  return res.data.data ?? null
}

export async function updateInvoiceStatus(
  ticketId: number,
  status: InvoiceStatus
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    `/api/ticket/admin/${ticketId}/invoice/status`,
    { invoice_status: status }
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update invoice status')
  }
  return true
}

export async function fetchUserQuota(
  userId: number
): Promise<number | null> {
  try {
    const res = await api.get<ApiResponse<{ quota: number }>>(
      `/api/user/${userId}`
    )
    if (!res.data.success) return null
    return Number(res.data.data?.quota ?? 0)
  } catch {
    return null
  }
}
