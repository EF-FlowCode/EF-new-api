import { createFileRoute } from '@tanstack/react-router'
import { Leaderboard } from '@/features/leaderboard'

export const Route = createFileRoute('/_authenticated/leaderboard/')({
  component: Leaderboard,
})
