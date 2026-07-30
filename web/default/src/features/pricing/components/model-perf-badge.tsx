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

import { getPricingHealthTrendDotClass } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

export type ModelPerfBadgeData = {
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_rates?: number[]
  health_trends?: {
    last_24h: number | null
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

  const { avg_latency_ms, avg_tps, success_rate } = props.perf

  const healthTrends = props.perf.health_trends
  const recentRates =
    props.perf.recent_success_rates?.filter((rate) => Number.isFinite(rate)) ??
    []
  const fallbackSource = recentRates.length > 0 ? recentRates : [success_rate]
  const fallbackRates = [
    ...Array(Math.max(0, 3 - fallbackSource.length)).fill(null),
    ...fallbackSource.slice(-3),
  ].slice(-3)
  const statusBars = healthTrends
    ? [
        { label: '24h', rate: healthTrends.last_24h },
        { label: '1h', rate: healthTrends.last_1h },
        { label: '5m', rate: healthTrends.last_5m },
      ]
    : fallbackRates.map((rate, index) => ({
        label: ['24h', '1h', '5m'][index],
        rate,
      }))

  const statusTitle = statusBars
    .map(
      ({ label, rate }) =>
        `${label}: ${typeof rate === 'number' && Number.isFinite(rate) ? `${rate.toFixed(1)}%` : '—'}`
    )
    .join(' · ')

  return (
    <div
      className={cn(
        'hidden w-[132px] grid-cols-[38px_48px_30px] gap-x-2 text-right tabular-nums min-[460px]:grid',
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
      <div title={`${t('Success rate')}: ${statusTitle}`} className='min-w-0'>
        <div className='text-muted-foreground/55 truncate text-[10px] leading-4'>
          {t('Status short')}
        </div>
        <div className='flex h-4 items-center justify-end gap-0.5'>
          {statusBars.map(({ label, rate }, index) => (
            <span
              key={label}
              title={`${label}: ${typeof rate === 'number' && Number.isFinite(rate) ? `${rate.toFixed(1)}%` : '—'}`}
              className={cn(
                'w-1 rounded-full',
                index === 0 && 'h-2',
                index === 1 && 'h-2.5',
                index === 2 && 'h-3',
                typeof rate !== 'number' || !Number.isFinite(rate)
                  ? index === 0
                    ? 'bg-muted-foreground/10'
                    : 'bg-muted-foreground/15'
                  : getPricingHealthTrendDotClass(rate)
              )}
            />
          ))}
        </div>
      </div>
    </div>
  )
})
