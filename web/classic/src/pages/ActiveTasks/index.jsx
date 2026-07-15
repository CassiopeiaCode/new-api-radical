/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import React, { useEffect, useState } from 'react';
import { Button, Card, Table } from '@douyinfe/semi-ui';
import { API, showError } from '../../helpers';

export default function ActiveTasks() {
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
      showError(error?.message || 'Failed to load active tasks');
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    load();
  }, []);
  const columns = [
    { title: 'User', dataIndex: 'username' },
    { title: 'User ID', dataIndex: 'user_id' },
    { title: 'Active slots', dataIndex: 'active_slots' },
  ];
  const historyColumns = [
    ...columns,
    { title: 'Global slots', dataIndex: 'global_active_slots' },
    {
      title: 'Time',
      render: (_, row) => new Date(row.created_at * 1000).toLocaleString(),
    },
  ];
  return (
    <div className='mt-[60px] px-2'>
      <Card
        title='Active tasks'
        headerExtraContent={
          <Button loading={loading} onClick={load}>
            Refresh
          </Button>
        }
      >
        <p>
          Active slots: {stats?.global_active_slots || 0} /{' '}
          {stats?.global_limit || 0}; active users: {stats?.active_users || 0};
          per-user limit: {stats?.user_limit || 0}
        </p>
        <h5>Current usage</h5>
        <Table
          columns={columns}
          dataSource={stats?.rank || []}
          pagination={false}
          rowKey='user_id'
        />
        <h5 style={{ marginTop: 24 }}>Active task history</h5>
        <Table
          columns={historyColumns}
          dataSource={history}
          pagination={false}
          rowKey='id'
        />
      </Card>
    </div>
  );
}
