/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import React, { useEffect, useState } from 'react';
import { Button, Card, Table } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

function normalizeDateLocale(language) {
  const compact = String(language || '')
    .replace(/[-_]/g, '')
    .toLowerCase();
  const candidate =
    compact === 'zhcn' || compact === 'zh'
      ? 'zh-CN'
      : compact === 'zhtw'
        ? 'zh-TW'
        : String(language || '').replace('_', '-');
  try {
    return Intl.getCanonicalLocales(candidate)[0];
  } catch {
    return undefined;
  }
}

export default function ActiveTasks() {
  const { i18n, t } = useTranslation();
  const [stats, setStats] = useState(null);
  const [history, setHistory] = useState([]);
  const [loading, setLoading] = useState(false);
  const load = async () => {
    setLoading(true);
    try {
      const [s, h] = await Promise.all([
        API.get('/api/active-task/stats'),
        API.get('/api/active-task/history?p=1&page_size=100'),
      ]);
      setStats(s.data?.data || null);
      setHistory(h.data?.data?.items || []);
    } catch (error) {
      showError(error?.message || t('Failed to load active tasks'));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    load();
  }, []);
  const columns = [
    { title: t('User'), dataIndex: 'username' },
    { title: t('User ID'), dataIndex: 'user_id' },
    { title: t('Active slots'), dataIndex: 'active_slots' },
  ];
  const historyColumns = [
    ...columns,
    { title: t('Global slots'), dataIndex: 'global_active_slots' },
    {
      title: t('Time'),
      render: (_, row) =>
        new Date(row.created_at * 1000).toLocaleString(
          normalizeDateLocale(i18n.language),
        ),
    },
  ];
  return (
    <div className='mt-[60px] max-h-[calc(100vh-60px)] overflow-auto px-2 pb-4'>
      <Card
        title={t('Active tasks')}
        headerExtraContent={
          <Button loading={loading} onClick={load}>
            {t('Refresh')}
          </Button>
        }
      >
        <p>
          {t('Active slots')}: {stats?.global_active_slots || 0} /{' '}
          {stats?.global_limit || 0}; {t('Active users')}:{' '}
          {stats?.active_users || 0}; {t('Per-user limit')}:{' '}
          {stats?.user_limit || 0}
        </p>
        <h5>{t('Current usage')}</h5>
        <Table
          columns={columns}
          dataSource={stats?.rank || []}
          pagination={false}
          rowKey='user_id'
          scroll={{ x: 'max-content' }}
        />
        <h5 style={{ marginTop: 24 }}>{t('Active task history')}</h5>
        <Table
          columns={historyColumns}
          dataSource={history}
          pagination={false}
          rowKey='id'
          scroll={{ x: 'max-content' }}
        />
      </Card>
    </div>
  );
}
