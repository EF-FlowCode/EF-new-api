import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { TaskPricingModel } from '../models/task-pricing-sources'

export const TASK_PRICING_USAGE_QUERY_KEY = ['pricing', 'task-editor']

/** Root-only effective task pricing per model; `['pricing']` saves refresh it. */
export function useTaskPricingUsage() {
  return useQuery({
    queryKey: TASK_PRICING_USAGE_QUERY_KEY,
    queryFn: async () =>
      (
        await api.get<{ success: boolean; data: TaskPricingModel[] }>(
          '/api/option/task_pricing_usage'
        )
      ).data,
  })
}
