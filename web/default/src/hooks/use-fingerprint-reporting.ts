/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useEffect, useRef } from 'react'

import { api } from '@/lib/api'

const REPORT_INTERVAL_MS = 60 * 60 * 1000
const LAST_REPORT_KEY = 'new-api:fingerprint:last-report'

function reportKey(userID: number) {
  return `${LAST_REPORT_KEY}:${userID}`
}

export function useFingerprintReporting(userID?: number) {
  const startedForUser = useRef<number | null>(null)

  useEffect(() => {
    if (!userID || startedForUser.current === userID) return
    startedForUser.current = userID

    const lastReport = Number(window.localStorage.getItem(reportKey(userID)))
    if (
      Number.isFinite(lastReport) &&
      Date.now() - lastReport < REPORT_INTERVAL_MS
    )
      return

    let cancelled = false
    void (async () => {
      try {
        const FingerprintJS = await import('@fingerprintjs/fingerprintjs')
        const agent = await FingerprintJS.load()
        const { visitorId } = await agent.get()
        if (cancelled || !visitorId) return
        const result = await api.post('/api/fingerprint/record', {
          visitor_id: visitorId,
        })
        if (result.data?.success)
          window.localStorage.setItem(reportKey(userID), String(Date.now()))
      } catch {
        // Optional risk signal: SDK or network failure never blocks normal use.
      }
    })()
    return () => {
      cancelled = true
    }
  }, [userID])
}
