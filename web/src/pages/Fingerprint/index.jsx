/*
Copyright (C) 2025 QuantumNous

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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Button,
  Card,
  Input,
  Table,
  Tag,
  Typography,
  Space,
  Modal,
  Tabs,
  TabPane,
} from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh } from '@douyinfe/semi-icons';
import { API, showError, showSuccess } from '../../helpers';
import { useTranslation } from 'react-i18next';

function formatTime(timeStr) {
  if (!timeStr) return '-';
  const date = new Date(timeStr);
  return date.toLocaleString('zh-CN');
}

function renderStatus(status) {
  if (status === 1) {
    return <Tag color='green'>正常</Tag>;
  }
  return <Tag color='red'>禁用</Tag>;
}

function renderRole(role) {
  switch (role) {
    case 100:
      return <Tag color='orange'>超管</Tag>;
    case 10:
      return <Tag color='blue'>管理</Tag>;
    default:
      return <Tag>普通</Tag>;
  }
}

export default function FingerprintPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const [activeTab, setActiveTab] = useState('duplicates');
  const [loading, setLoading] = useState(false);
  const [searchKeyword, setSearchKeyword] = useState('');

  // 重复指纹数据
  const [duplicates, setDuplicates] = useState([]);
  const [duplicatesTotal, setDuplicatesTotal] = useState(0);
  const [duplicatesPage, setDuplicatesPage] = useState(1);

  // 所有指纹数据
  const [fingerprints, setFingerprints] = useState([]);
  const [fingerprintsTotal, setFingerprintsTotal] = useState(0);
  const [fingerprintsPage, setFingerprintsPage] = useState(1);

  // 关联用户弹窗
  const [usersModalVisible, setUsersModalVisible] = useState(false);
  const [selectedVisitorId, setSelectedVisitorId] = useState('');
  const [selectedIp, setSelectedIp] = useState('');
  const [relatedUsers, setRelatedUsers] = useState([]);
  const [relatedUsersLoading, setRelatedUsersLoading] = useState(false);
  const [relatedQueryType, setRelatedQueryType] = useState('visitor_id'); // visitor_id | ip | visitor_id_ip

  const pageSize = 20;

  // 加载重复指纹
  const loadDuplicates = useCallback(async (page = 1) => {
    setLoading(true);
    try {
      const res = await API.get('/api/fingerprint/duplicates', {
        params: { p: page, page_size: pageSize },
      });
      const { success, data, message } = res.data;
      if (success) {
        setDuplicates(data?.items || []);
        setDuplicatesTotal(data?.total || 0);
      } else {
        showError(message || '加载失败');
      }
    } catch (e) {
      showError(e.message || '请求失败');
    } finally {
      setLoading(false);
    }
  }, []);

  // 加载所有指纹
  const loadFingerprints = useCallback(async (page = 1, keyword = '') => {
    setLoading(true);
    try {
      const url = keyword
        ? '/api/fingerprint/search'
        : '/api/fingerprint/';
      const res = await API.get(url, {
        params: { p: page, page_size: pageSize, keyword },
      });
      const { success, data, message } = res.data;
      if (success) {
        setFingerprints(data?.items || []);
        setFingerprintsTotal(data?.total || 0);
      } else {
        showError(message || '加载失败');
      }
    } catch (e) {
      showError(e.message || '请求失败');
    } finally {
      setLoading(false);
    }
  }, []);

  // 加载关联用户：按 visitor_id(+ip) 或按 ip
  const loadRelatedUsers = useCallback(async (type, visitorId, ip) => {
    setRelatedUsersLoading(true);
    try {
      let url = '/api/fingerprint/users';
      const params = { p: 1, page_size: 100 };

      if (type === 'ip') {
        url = '/api/fingerprint/users_by_ip';
        params.ip = ip;
      } else {
        params.visitor_id = visitorId;
        if (type === 'visitor_id_ip' && ip) {
          params.ip = ip;
        }
      }

      const res = await API.get(url, { params });
      const { success, data, message } = res.data;
      if (success) {
        setRelatedUsers(data?.items || []);
      } else {
        showError(message || '加载失败');
      }
    } catch (e) {
      showError(e.message || '请求失败');
    } finally {
      setRelatedUsersLoading(false);
    }
  }, []);

  // 初始加载
  useEffect(() => {
    if (activeTab === 'duplicates') {
      loadDuplicates(duplicatesPage);
    } else {
      loadFingerprints(fingerprintsPage, searchKeyword);
    }
  }, [activeTab]);

  // 查看关联用户
  const handleViewUsers = (type, visitorId, ip) => {
    setRelatedQueryType(type);
    setSelectedVisitorId(visitorId || '');
    setSelectedIp(ip || '');
    setUsersModalVisible(true);
    loadRelatedUsers(type, visitorId, ip);
  };

  // 重复指纹表格列
  const duplicatesColumns = useMemo(
    () => [
      {
        title: 'Visitor ID',
        dataIndex: 'visitor_id',
        key: 'visitor_id',
        width: 280,
        render: (v) => (
          <Typography.Text
            copyable
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 260 }}
          >
            {v}
          </Typography.Text>
        ),
      },
      {
        title: 'IP',
        dataIndex: 'ip',
        key: 'ip',
        width: 140,
      },
      {
        title: t('关联用户数'),
        dataIndex: 'user_count',
        key: 'user_count',
        width: 120,
        render: (v) => <Tag color='red'>{v} 人</Tag>,
      },
      {
        title: t('最后活跃'),
        dataIndex: 'last_seen',
        key: 'last_seen',
        width: 180,
        render: (v) => formatTime(v),
      },
      {
        title: t('操作'),
        key: 'action',
        width: 120,
        render: (_, record) => (
          <Button
            size='small'
            type='primary'
            onClick={() => handleViewUsers('visitor_id_ip', record.visitor_id, record.ip)}
          >
            {t('查看用户')}
          </Button>
        ),
      },
    ],
    [t],
  );

  // 所有指纹表格列
  const fingerprintsColumns = useMemo(
    () => [
      {
        title: 'ID',
        dataIndex: 'id',
        key: 'id',
        width: 80,
      },
      {
        title: t('用户名'),
        dataIndex: 'username',
        key: 'username',
        width: 120,
      },
      {
        title: 'Visitor ID',
        dataIndex: 'visitor_id',
        key: 'visitor_id',
        width: 240,
        render: (v) => (
          <Typography.Text
            copyable
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 220 }}
          >
            {v}
          </Typography.Text>
        ),
      },
      {
        title: 'IP',
        dataIndex: 'ip',
        key: 'ip',
        width: 140,
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        width: 80,
        render: renderStatus,
      },
      {
        title: t('角色'),
        dataIndex: 'role',
        key: 'role',
        width: 80,
        render: renderRole,
      },
      {
        title: t('记录时间'),
        dataIndex: 'record_time',
        key: 'record_time',
        width: 180,
        render: (v) => formatTime(v),
      },
      {
        title: t('操作'),
        key: 'action',
        width: 220,
        render: (_, record) => (
          <Space>
            <Button
              size='small'
              onClick={() => handleViewUsers('visitor_id', record.visitor_id, '')}
            >
              {t('按VisitorID')}
            </Button>
            <Button
              size='small'
              onClick={() => handleViewUsers('ip', '', record.ip)}
              disabled={!record.ip}
            >
              {t('按IP')}
            </Button>
          </Space>
        ),
      },
    ],
    [t],
  );

  // 关联用户表格列
  const usersColumns = useMemo(
    () => [
      {
        title: 'ID',
        dataIndex: 'id',
        key: 'id',
        width: 80,
      },
      {
        title: t('用户名'),
        dataIndex: 'username',
        key: 'username',
        width: 120,
      },
      {
        title: t('显示名'),
        dataIndex: 'display_name',
        key: 'display_name',
        width: 120,
      },
      {
        title: t('额度'),
        dataIndex: 'quota',
        key: 'quota',
        width: 100,
        render: (v) => (v / 500000).toFixed(2),
      },
      {
        title: t('已用额度'),
        dataIndex: 'used_quota',
        key: 'used_quota',
        width: 100,
        render: (v) => (v / 500000).toFixed(2),
      },
      {
        title: t('调用次数'),
        dataIndex: 'request_count',
        key: 'request_count',
        width: 100,
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        width: 80,
        render: renderStatus,
      },
      {
        title: t('角色'),
        dataIndex: 'role',
        key: 'role',
        width: 80,
        render: renderRole,
      },
      {
        title: t('记录时间'),
        dataIndex: 'record_time',
        key: 'record_time',
        width: 180,
        render: (v) => formatTime(v),
      },
    ],
    [t],
  );

  const handleSearch = () => {
    setFingerprintsPage(1);
    loadFingerprints(1, searchKeyword);
  };

  const handleRefresh = () => {
    if (activeTab === 'duplicates') {
      loadDuplicates(duplicatesPage);
    } else {
      loadFingerprints(fingerprintsPage, searchKeyword);
    }
  };

  return (
    <div className='mt-[60px] px-2'>
      <Card
        className='!rounded-2xl'
        title={t('关联追踪')}
        headerExtraContent={
          <Button
            icon={<IconRefresh />}
            type='tertiary'
            onClick={handleRefresh}
            loading={loading}
          >
            {t('刷新')}
          </Button>
        }
      >
        <Tabs activeKey={activeTab} onChange={setActiveTab}>
          <TabPane tab={t('重复指纹')} itemKey='duplicates'>
            <div className='mb-4'>
              <Typography.Text type='secondary'>
                {t('以下设备指纹被多个用户使用，可能存在关联账号')}
              </Typography.Text>
            </div>
            <Table
              bordered
              size='small'
              loading={loading}
              columns={duplicatesColumns}
              dataSource={(duplicates || []).map((r, idx) => ({
                ...r,
                key: `${r.visitor_id}-${r.ip || idx}`,
              }))}
              pagination={{
                currentPage: duplicatesPage,
                pageSize,
                total: duplicatesTotal,
                onPageChange: (page) => {
                  setDuplicatesPage(page);
                  loadDuplicates(page);
                },
              }}
            />
          </TabPane>

          <TabPane tab={t('全部记录')} itemKey='all'>
            <div className='mb-4'>
              <Space>
                <Input
                  prefix={<IconSearch />}
                  placeholder={t('搜索用户名、邮箱或Visitor ID')}
                  value={searchKeyword}
                  onChange={setSearchKeyword}
                  onEnterPress={handleSearch}
                  style={{ width: 300 }}
                />
                <Button type='primary' onClick={handleSearch}>
                  {t('搜索')}
                </Button>
              </Space>
            </div>
            <Table
              bordered
              size='small'
              loading={loading}
              columns={fingerprintsColumns}
              dataSource={(fingerprints || []).map((r, idx) => ({
                ...r,
                key: `${r.id}-${idx}`,
              }))}
              pagination={{
                currentPage: fingerprintsPage,
                pageSize,
                total: fingerprintsTotal,
                onPageChange: (page) => {
                  setFingerprintsPage(page);
                  loadFingerprints(page, searchKeyword);
                },
              }}
            />
          </TabPane>
        </Tabs>
      </Card>

      <Modal
        title={
          <span>
            {t('关联用户')}{' '}
            {relatedQueryType === 'ip' ? (
              <>
                - IP: <code>{selectedIp}</code>
              </>
            ) : (
              <>
                - <code>{selectedVisitorId}</code>
                {relatedQueryType === 'visitor_id_ip' && selectedIp && (
                  <span>
                    {' '}
                    IP: <code>{selectedIp}</code>
                  </span>
                )}
              </>
            )}
          </span>
        }
        visible={usersModalVisible}
        onCancel={() => setUsersModalVisible(false)}
        footer={null}
        width={900}
      >
        <Table
          bordered
          size='small'
          loading={relatedUsersLoading}
          columns={usersColumns}
          dataSource={(relatedUsers || []).map((r, idx) => ({
            ...r,
            key: `${r.id}-${idx}`,
          }))}
          pagination={false}
        />
      </Modal>
    </div>
  );
}
