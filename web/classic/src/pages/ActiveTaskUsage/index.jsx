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

export default function ActiveTaskUsage() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const load = async () => {
    setLoading(true);
    try {
      const response = await API.get('/api/active-task/usage/self');
      setItems(response.data?.data?.items || []);
    } catch (error) {
      showError(error?.message || 'Failed to load model usage');
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    load();
  }, []);
  return (
    <div className='mt-[60px] px-2'>
      <Card
        title='My model usage (last 24 hours)'
        headerExtraContent={
          <Button loading={loading} onClick={load}>
            Refresh
          </Button>
        }
      >
        <Table
          columns={[
            { title: 'Model', dataIndex: 'model_name' },
            { title: 'Tokens', dataIndex: 'token_used' },
            { title: 'Requests', dataIndex: 'request_count' },
          ]}
          dataSource={items}
          pagination={false}
          rowKey='model_name'
        />
      </Card>
    </div>
  );
}
