/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { memo } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

export type ModelPerfBadgeData = {
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_rates?: number[]
  health_trends?: {
    last_24h: number | null
    last_12h: number | null
    last_6h: number | null
    last_10m: number | null
    last_1h: number | null
    last_5m: number | null
  }
}

export interface ModelPerfBadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  perf: ModelPerfBadgeData | undefined
}

function formatCompactNumber(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '—'
  return value > 1 ? String(Math.round(value)) : value.toFixed(1)
}

function formatCompactLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  if (ms >= 1_000) return `${formatCompactNumber(ms / 1_000)}s`
  return `${formatCompactNumber(ms)}ms`
}

function formatCompactThroughput(tps: number): string {
  if (!Number.isFinite(tps) || tps <= 0) return '—'
  if (tps >= 1_000) return `${formatCompactNumber(tps / 1_000)}Kt`
  return `${formatCompactNumber(tps)}t`
}

export const ModelPerfBadge = memo(function ModelPerfBadge(
  props: ModelPerfBadgeProps
) {
  const { t } = useTranslation()

  if (!props.perf) {
    return null
  }

  const { avg_latency_ms, avg_tps } = props.perf
  const healthTrends = props.perf.health_trends
  const statusBars = [
    { label: '24h', rate: healthTrends?.last_24h },
    { label: '12h', rate: healthTrends?.last_12h },
    { label: '6h', rate: healthTrends?.last_6h },
    { label: '1h', rate: healthTrends?.last_1h },
    { label: '10min', rate: healthTrends?.last_10m },
  ]

  const statusTitle = statusBars
    .map(
      ({ label, rate }) =>
        `${label}: ${typeof rate === 'number' && Number.isFinite(rate) ? `${rate.toFixed(1)}%` : '—'}`
    )
    .join(' · ')

  return (
    <div
      className={cn(
        'hidden w-[144px] grid-cols-[38px_48px_42px] gap-x-2 text-right tabular-nums min-[460px]:grid',
        props.className
      )}
    >
      <div title={t('Average latency')} className='min-w-0'>
        <div className='text-muted-foreground/55 text-[10px] leading-4'>
          {t('Latency short')}
        </div>
        <div className='text-muted-foreground/80 font-mono text-xs leading-4 whitespace-nowrap'>
          {formatCompactLatency(avg_latency_ms)}
        </div>
      </div>
      <div title={t('Throughput')} className='min-w-0'>
        <div className='text-muted-foreground/55 truncate text-[10px] leading-4'>
          {t('Throughput short')}
        </div>
        <div className='text-muted-foreground/80 font-mono text-xs leading-4 whitespace-nowrap'>
          {formatCompactThroughput(avg_tps)}
        </div>
      </div>
      <div
        title={`${t('Success rate')}: ${statusTitle}`}
        aria-label={`${t('Success rate')}: ${statusTitle}`}
        tabIndex={0}
        className='group/status focus-visible:ring-ring relative min-w-0 outline-none focus-visible:ring-2'
      >
        <div className='text-muted-foreground/55 truncate text-[10px] leading-4'>
          {t('Status short')}
        </div>
        <div className='bg-popover text-popover-foreground pointer-events-none absolute right-0 bottom-full z-50 mb-2 hidden w-max rounded-md border px-3 py-2 text-xs shadow-md group-focus-within/status:block group-hover/status:block'>
          {statusBars.map(({ label, rate }) => (
            <div key={label} className='flex justify-between gap-4 leading-5'>
              <span>{label}</span>
              <span>
                {typeof rate === 'number' && Number.isFinite(rate)
                  ? `${rate.toFixed(2)}%`
                  : '—'}
              </span>
            </div>
          ))}
        </div>
        <div className='flex h-4 items-center justify-end gap-0.5'>
          {statusBars.map(({ label, rate }) => {
            let backgroundColor = 'var(--muted-foreground)'
            const hasRate = typeof rate === 'number' && Number.isFinite(rate)
            if (hasRate) {
              const boundedRate = Math.min(100, Math.max(0, rate))
              if (boundedRate <= 60) {
                backgroundColor = `color-mix(in oklch, var(--destructive), var(--warning) ${(boundedRate / 60) * 100}%)`
              } else {
                backgroundColor = `color-mix(in oklch, var(--warning), var(--success) ${((boundedRate - 60) / 40) * 100}%)`
              }
            }
            return (
              <span
                key={label}
                title={`${label}: ${hasRate ? `${rate.toFixed(1)}%` : '—'}`}
                className={cn('h-3 w-1 rounded-full', !hasRate && 'opacity-20')}
                style={{ backgroundColor }}
              />
            )
          })}
        </div>
      </div>
    </div>
  )
})
