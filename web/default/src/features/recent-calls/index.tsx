import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'

type RecentCall = {
  id: number
  created_at: string
  user_id: number
  channel_id?: number
  model_name?: string
  method: string
  path: string
  request?: { headers?: Record<string, string>; body?: string }
  response?: { status_code: number; body?: string }
  stream?: { chunks?: string[]; aggregated_text?: string }
  error?: { message?: string; type?: string; code?: string; status?: number }
}

export function RecentCalls() {
  const { t } = useTranslation()
  const [items, setItems] = useState<RecentCall[]>([])
  const [selected, setSelected] = useState<RecentCall | null>(null)
  const [loading, setLoading] = useState(false)
  const load = async () => {
    setLoading(true)
    try {
      const result = await api.get('/api/debug/recent_calls?limit=100')
      setItems(result.data?.data ?? [])
    } finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [])
  const detail = async (id: number) => {
    const result = await api.get(`/api/debug/recent_calls/${id}`)
    setSelected(result.data?.data ?? null)
  }
  return <SectionPageLayout fixedContent>
    <SectionPageLayout.Title>{t('Recent Calls')}</SectionPageLayout.Title>
    <SectionPageLayout.Actions><Button onClick={() => void load()} disabled={loading}>{loading ? t('Loading...') : t('Refresh')}</Button></SectionPageLayout.Actions>
    <SectionPageLayout.Content><div className='grid gap-4 lg:grid-cols-2'>
      <div className='rounded-md border'><table className='w-full text-sm'><thead><tr className='text-left'><th>ID</th><th>{t('Model')}</th><th>{t('Path')}</th><th>{t('Status')}</th></tr></thead><tbody>{items.map(item => <tr className='cursor-pointer border-t hover:bg-muted/50' key={item.id} onClick={() => void detail(item.id)}><td>{item.id}</td><td>{item.model_name || '-'}</td><td>{item.method} {item.path}</td><td>{item.error?.status || item.response?.status_code || '-'}</td></tr>)}</tbody></table></div>
      <pre className='max-h-[70vh] overflow-auto rounded-md bg-muted p-3 text-xs'>{selected ? JSON.stringify(selected, null, 2) : t('Select a call to inspect its sanitized request, response, stream chunks and error.')}</pre>
    </div></SectionPageLayout.Content>
  </SectionPageLayout>
}
