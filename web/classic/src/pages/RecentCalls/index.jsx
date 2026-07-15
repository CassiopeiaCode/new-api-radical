import React, { useEffect, useState } from 'react';
import { Button, Card, Table, Typography } from '@douyinfe/semi-ui';
import { API, showError } from '../../helpers';

const { Text } = Typography;

export default function RecentCalls() {
  const [items, setItems] = useState([]);
  const [selected, setSelected] = useState(null);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/debug/recent_calls?limit=100');
      setItems(res.data?.data || []);
    } catch (error) {
      showError(error?.message || 'Failed to load recent calls');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const columns = [
    { title: 'ID', dataIndex: 'id' },
    { title: '模型', dataIndex: 'model_name' },
    { title: '路径', render: (_, row) => `${row.method} ${row.path}` },
    {
      title: '状态',
      render: (_, row) =>
        row.error?.status || row.response?.status_code || '-',
    },
  ];

  return (
    <div className='mt-[60px] max-h-[calc(100vh-60px)] overflow-auto px-2 pb-4'>
      <Card
        title='Recent Calls'
        extra={
          <Button loading={loading} onClick={load}>
            刷新
          </Button>
        }
      >
        <Table
          dataSource={items}
          columns={columns}
          pagination={false}
          scroll={{ x: 'max-content' }}
          onRow={(row) => ({
            onClick: async () => {
              try {
                const res = await API.get(`/api/debug/recent_calls/${row.id}`);
                setSelected(res.data?.data || null);
              } catch (error) {
                showError(error?.message || 'Failed to load details');
              }
            },
          })}
        />
        <Text strong>详情</Text>
        <pre
          style={{
            maxHeight: 'min(500px, 55vh)',
            maxWidth: '100%',
            overflow: 'auto',
          }}
        >
          {selected
            ? JSON.stringify(selected, null, 2)
            : '选择一条调用查看脱敏请求、响应、流 chunk 和错误信息。'}
        </pre>
      </Card>
    </div>
  );
}
