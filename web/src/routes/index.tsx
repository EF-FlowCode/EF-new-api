import { createFileRoute } from '@tanstack/react-router'
import { Portal } from '@/features/portal/portal'

// eflow: 首页改成营销门户 portal,代替之前「redirect 到 /dashboard 后弹 sign-in」的流程。
// 现在登没登都直接看 portal,登录入口在 portal 自带 nav / hero CTA 里。
export const Route = createFileRoute('/')({
  component: Portal,
})
