import { createFileRoute, redirect } from '@tanstack/react-router'

import { RecentCalls } from '@/features/recent-calls'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
export const Route = createFileRoute('/_authenticated/recent-calls/')({
  beforeLoad: () => {
    if ((useAuthStore.getState().auth.user?.role ?? 0) < ROLE.ADMIN)
      throw redirect({ to: '/403' })
  },
  component: RecentCalls,
})
