import React, { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Card,
  Input,
  Table,
  Tabs,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError } from '../../helpers';

const { TabPane } = Tabs;

export default function Fingerprint() {
  const [duplicates, setDuplicates] = useState([]);
  const [records, setRecords] = useState([]);
  const [related, setRelated] = useState([]);
  const [keyword, setKeyword] = useState('');
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState(null);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [duplicateResponse, recordResponse] = await Promise.all([
        API.get('/api/fingerprint/duplicates?p=1&page_size=100'),
        API.get('/api/fingerprint/', {
          params: { p: 1, page_size: 100, keyword },
        }),
      ]);
      setDuplicates(duplicateResponse.data?.data?.items || []);
      setRecords(recordResponse.data?.data?.items || []);
    } catch (error) {
      showError(error?.message || 'Failed to load fingerprint records');
    } finally {
      setLoading(false);
    }
  }, [keyword]);
  useEffect(() => {
    load();
  }, [load]);
  const inspect = async (row) => {
    try {
      const response = await API.get('/api/fingerprint/users', {
        params: {
          visitor_id: row.visitor_id,
          ip: row.ip,
          p: 1,
          page_size: 100,
        },
      });
      setRelated(response.data?.data?.items || []);
      setSelected(row);
    } catch (error) {
      showError(error?.message || 'Failed to load associated users');
    }
  };
  const columns = [
    {
      title: 'Visitor ID',
      dataIndex: 'visitor_id',
      render: (value) => (
        <Typography.Text
          copyable
          ellipsis={{ showTooltip: true }}
          style={{ maxWidth: 260 }}
        >
          {value}
        </Typography.Text>
      ),
    },
    { title: 'IP', dataIndex: 'ip' },
    { title: 'User', render: (_, row) => row.username || row.user_count },
    {
      title: 'Last seen',
      render: (_, row) => row.record_time || row.last_seen,
    },
    {
      title: 'Action',
      render: (_, row) => (
        <Button size='small' onClick={() => inspect(row)}>
          Inspect
        </Button>
      ),
    },
  ];
  return (
    <div className='mt-[60px] px-2'>
      <Card
        title='Fingerprint associations'
        headerExtraContent={
          <Button loading={loading} onClick={load}>
            Refresh
          </Button>
        }
      >
        <Input
          value={keyword}
          onChange={setKeyword}
          onEnterPress={load}
          placeholder='Search visitor ID, IP, username or email'
          style={{ width: 360, marginBottom: 16 }}
        />
        <Tabs>
          <TabPane tab='Shared fingerprints' itemKey='duplicates'>
            <Table
              columns={columns}
              dataSource={duplicates}
              pagination={false}
              rowKey={(row) => `${row.visitor_id}-${row.ip}`}
            />
          </TabPane>
          <TabPane tab='Recent records' itemKey='records'>
            <Table
              columns={columns}
              dataSource={records}
              pagination={false}
              rowKey={(row) => row.id}
            />
          </TabPane>
        </Tabs>
        {selected && (
          <>
            <Typography.Title heading={5} style={{ marginTop: 24 }}>
              Associated users: {selected.visitor_id} · {selected.ip}
            </Typography.Title>
            <Table
              columns={columns.filter((column) => column.title !== 'Action')}
              dataSource={related}
              pagination={false}
              rowKey={(row) => row.id}
            />
          </>
        )}
      </Card>
    </div>
  );
}
