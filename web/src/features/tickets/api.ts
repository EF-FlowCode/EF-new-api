import { isAxiosError } from 'axios'
import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'
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
  companyName?: string
}

function buildQuery(params: TicketListParams): string {
  const search = new URLSearchParams()
  search.set('p', String(params.page))
  search.set('page_size', String(params.pageSize))
  if (params.status !== undefined && params.status !== '') {
    search.set('status', String(params.status))
  }
  if (params.type) search.set('type', params.type)
  if (params.admin && params.companyName?.trim()) {
    search.set('company_name', params.companyName.trim())
  }
  if (params.orderBy) {
    search.set('order_by', params.orderBy)
    search.set('order', params.order === 'asc' ? 'asc' : 'desc')
  }
  return search.toString()
}

export async function fetchTickets(
  params: TicketListParams,
  signal?: AbortSignal
): Promise<TicketListResponse> {
  const base = params.admin ? '/api/ticket/admin' : '/api/ticket/self'
  const res = await api.get<ApiResult<TicketListResponse>>(
    `${base}?${buildQuery(params)}`,
    { ...CALLER_HANDLED_REQUEST_CONFIG, signal }
  )
  return unwrapApiResult(res.data, 'Failed to load tickets')
}

const INVOICE_WORKBOOK_TYPE =
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'

async function invoiceExportError(data: unknown): Promise<string> {
  if (data instanceof Blob && data.type.includes('json')) {
    try {
      const body = JSON.parse(await data.text()) as { message?: unknown }
      if (typeof body.message === 'string' && body.message) return body.message
    } catch {
      /* use the translated fallback */
    }
  }
  return 'Failed to export invoices'
}

export async function exportTicketInvoices(
  ids: number[]
): Promise<{ blob: Blob; filename: string }> {
  let response
  try {
    response = await api.post<Blob>(
      '/api/ticket/admin/invoice-export',
      { ids },
      {
        ...CALLER_HANDLED_REQUEST_CONFIG,
        responseType: 'blob',
      }
    )
  } catch (error) {
    if (isAxiosError(error) && error.response?.data instanceof Blob) {
      throw new Error(await invoiceExportError(error.response.data), {
        cause: error,
      })
    }
    throw error
  }
  if (
    !(response.data instanceof Blob) ||
    response.data.size === 0 ||
    !String(response.headers['content-type']).startsWith(INVOICE_WORKBOOK_TYPE)
  ) {
    throw new Error(await invoiceExportError(response.data))
  }
  const disposition = String(response.headers['content-disposition'] || '')
  const filename =
    /filename="(invoice-requests-[0-9-]+\.xlsx)"/.exec(disposition)?.[1] ||
    'invoice-requests.xlsx'
  return { blob: response.data, filename }
}

export async function fetchTicketDetail(
  ticketId: number,
  admin: boolean
): Promise<TicketDetail> {
  const path = admin
    ? `/api/ticket/admin/${ticketId}`
    : `/api/ticket/self/${ticketId}`
  const res = await api.get<ApiResult<TicketDetail>>(
    path,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load ticket')
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
  const res = await api.post<ApiResult<unknown>>(
    path,
    {
      content,
      attachment_ids: attachmentIds,
    },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to send reply')
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
    ? await api.put<ApiResult<unknown>>(
        path,
        { status: 4 },
        CALLER_HANDLED_REQUEST_CONFIG
      )
    : await api.put<ApiResult<unknown>>(
        path,
        undefined,
        CALLER_HANDLED_REQUEST_CONFIG
      )
  unwrapApiResult(res.data, 'Failed to close ticket')
  return true
}

export async function adminUpdateTicketStatus(
  ticketId: number,
  status: TicketStatus
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    `/api/ticket/admin/${ticketId}/status`,
    { status },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update ticket status')
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
  const res = await api.put<ApiResult<BatchUpdateStatusResult>>(
    '/api/ticket/admin/batch-status',
    { ids, status },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to update ticket status')
}

export async function createTicket(
  payload: CreateTicketPayload
): Promise<Ticket | null> {
  const res = await api.post<ApiResult<{ ticket?: Ticket } | null>>(
    '/api/ticket/',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to create ticket')?.ticket ?? null
}

// ----------------------------------------------------------------------------
// Refund flow
// ----------------------------------------------------------------------------

export async function createRefundTicket(
  payload: CreateRefundTicketPayload
): Promise<Ticket | null> {
  const res = await api.post<ApiResult<{ ticket?: Ticket } | null>>(
    '/api/ticket/refund/',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return (
    unwrapApiResult(res.data, 'Failed to create refund ticket')?.ticket ?? null
  )
}

export async function updateRefundStatus(
  ticketId: number,
  payload: UpdateRefundStatusPayload
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    `/api/ticket/admin/${ticketId}/refund/status`,
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update refund status')
  return true
}

// ----------------------------------------------------------------------------
// Invoice flow
// ----------------------------------------------------------------------------

export async function fetchEligibleInvoiceOrders(): Promise<
  EligibleTopUpOrder[]
> {
  const res = await api.get<ApiResult<EligibleTopUpOrder[] | null>>(
    '/api/ticket/invoice/eligible_orders',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return (
    unwrapApiResult(res.data, 'Failed to load orders eligible for invoice') ??
    []
  )
}

export async function createInvoiceTicket(
  payload: CreateInvoiceTicketPayload
): Promise<Ticket | null> {
  const res = await api.post<ApiResult<{ ticket?: Ticket } | null>>(
    '/api/ticket/invoice/',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return (
    unwrapApiResult(res.data, 'Failed to create invoice ticket')?.ticket ?? null
  )
}

export async function updateInvoiceStatus(
  ticketId: number,
  status: InvoiceStatus
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    `/api/ticket/admin/${ticketId}/invoice/status`,
    { invoice_status: status },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update invoice status')
  return true
}

export async function fetchUserQuota(userId: number): Promise<number | null> {
  try {
    const res = await api.get<ApiResult<{ quota: number } | null>>(
      `/api/user/${userId}`,
      CALLER_HANDLED_REQUEST_CONFIG
    )
    const data = unwrapApiResult(res.data, 'Failed to load user quota')
    return data ? Number(data.quota ?? 0) : null
  } catch {
    return null
  }
}
