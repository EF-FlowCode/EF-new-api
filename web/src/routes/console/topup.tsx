import { createFileRoute, redirect } from '@tanstack/react-router'

// 兼容 epay/stripe/waffo 老支付回调里硬编码的 /console/topup 路径
// 回调完成后统一进入充值账单页，不再维护钱包内的重复历史弹窗。

export const Route = createFileRoute('/console/topup')({
  beforeLoad: () => {
    throw redirect({
      to: '/topup-history',
    })
  },
})
