/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'

type Fingerprint = {
  id: number
  username: string
  visitor_id: string
  ip: string
  record_time: string
}
type Duplicate = {
  visitor_id: string
  ip: string
  user_count: number
  last_seen: string
}

export function Fingerprints() {
  const { t } = useTranslation()
  const [duplicates, setDuplicates] = useState<Duplicate[]>([])
  const [records, setRecords] = useState<Fingerprint[]>([])
  const [related, setRelated] = useState<Fingerprint[]>([])
  const [keyword, setKeyword] = useState('')
  const [selected, setSelected] = useState<{
    visitorID: string
    ip: string
  } | null>(null)
  const [loading, setLoading] = useState(false)
  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [duplicatesResponse, recordsResponse] = await Promise.all([
        api.get('/api/fingerprint/duplicates?p=1&page_size=100'),
        api.get('/api/fingerprint/', {
          params: { p: 1, page_size: 100, keyword },
        }),
      ])
      setDuplicates(duplicatesResponse.data?.data?.items ?? [])
      setRecords(recordsResponse.data?.data?.items ?? [])
    } finally {
      setLoading(false)
    }
  }, [keyword])
  useEffect(() => {
    void load()
  }, [load])
  const inspect = async (visitorID: string, ip: string) => {
    const response = await api.get('/api/fingerprint/users', {
      params: { visitor_id: visitorID, ip, p: 1, page_size: 100 },
    })
    setSelected({ visitorID, ip })
    setRelated(response.data?.data?.items ?? [])
  }
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Fingerprint associations')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Input
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          placeholder={t('Search visitor ID, IP, username or email')}
          className='w-72'
        />
        <Button onClick={() => void load()} disabled={loading}>
          {loading ? t('Loading...') : t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          <section>
            <h2 className='mb-2 text-lg font-semibold'>
              {t('Shared fingerprints')}
            </h2>
            <p className='text-muted-foreground mb-3 text-sm'>
              {t(
                'Only the same visitor ID and IP used by multiple users are listed.'
              )}
            </p>
            <FingerprintTable
              rows={duplicates}
              onInspect={(row) => void inspect(row.visitor_id, row.ip)}
            />
          </section>
          <section>
            <h2 className='mb-2 text-lg font-semibold'>
              {t('Recent fingerprint records')}
            </h2>
            <FingerprintTable
              rows={records}
              onInspect={(row) => void inspect(row.visitor_id, row.ip)}
            />
          </section>
          {selected && (
            <section className='rounded-md border p-4'>
              <h2 className='mb-2 font-semibold'>
                {t('Associated users')}: <code>{selected.visitorID}</code> ·{' '}
                <code>{selected.ip}</code>
              </h2>
              <FingerprintTable rows={related} />
            </section>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function FingerprintTable({
  rows,
  onInspect,
}: {
  rows: Array<Fingerprint | Duplicate>
  onInspect?: (row: Fingerprint | Duplicate) => void
}) {
  return (
    <div className='overflow-x-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-left'>
          <tr>
            <th className='p-2'>Visitor ID</th>
            <th className='p-2'>IP</th>
            <th className='p-2'>User</th>
            <th className='p-2'>Last seen</th>
            {onInspect && <th className='p-2' />}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr
              className='border-t'
              key={`${row.visitor_id}-${row.ip}-${index}`}
            >
              <td className='max-w-72 truncate p-2 font-mono text-xs'>
                {row.visitor_id}
              </td>
              <td className='p-2 font-mono text-xs'>{row.ip}</td>
              <td className='p-2'>
                {'username' in row ? row.username : row.user_count}
              </td>
              <td className='p-2'>
                {'record_time' in row ? row.record_time : row.last_seen}
              </td>
              {onInspect && (
                <td className='p-2 text-right'>
                  <Button
                    size='sm'
                    variant='outline'
                    onClick={() => onInspect(row)}
                  >
                    Inspect
                  </Button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
