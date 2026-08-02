import { createFileRoute, redirect } from '@tanstack/react-router'

// 兼容 epay/stripe 老支付回调里硬编码的 /console/log 路径
// 这里只做重定向，不渲染任何 UI；命中后由目标页 _authenticated 守卫处理登录态
export const Route = createFileRoute('/console/log')({
  beforeLoad: () => {
    throw redirect({ to: '/topup-history' })
  },
})
