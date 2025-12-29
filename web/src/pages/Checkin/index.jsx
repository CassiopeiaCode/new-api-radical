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

import React, { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Typography,
  Spin,
  Table,
  Empty,
} from '@douyinfe/semi-ui';
import { IconGift, IconHistory } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';

const { Title, Text } = Typography;

export default function Checkin() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [checkinLoading, setCheckinLoading] = useState(false);
  const [status, setStatus] = useState({
    enabled: false,
    has_checked: false,
    today_date: '',
    checkin_count: 0,
    total_quota: 0,
  });
  const [history, setHistory] = useState([]);

  const fetchStatus = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/user/checkin/status');
      if (res.data.success) {
        setStatus(res.data.data);
      }
    } catch (error) {
      showError(t('获取签到状态失败'));
    } finally {
      setLoading(false);
    }
  };

  const fetchHistory = async () => {
    try {
      const res = await API.get('/api/user/checkin/history');
      if (res.data.success) {
        setHistory(res.data.data || []);
      }
    } catch (error) {
      console.error('获取签到历史失败', error);
    }
  };

  const handleCheckin = async () => {
    setCheckinLoading(true);
    try {
      const res = await API.post('/api/user/checkin');
      if (res.data.success) {
        showSuccess(t('签到成功！获得 {{quota}} Token', { quota: res.data.data.quota }));
        fetchStatus();
        fetchHistory();
      } else {
        showError(res.data.message || t('签到失败'));
      }
    } catch (error) {
      showError(t('签到失败'));
    } finally {
      setCheckinLoading(false);
    }
  };

  useEffect(() => {
    fetchStatus();
    fetchHistory();
  }, []);

  const columns = [
    {
      title: t('日期'),
      dataIndex: 'checkin_date',
      key: 'checkin_date',
    },
    {
      title: t('获得额度'),
      dataIndex: 'quota',
      key: 'quota',
      render: (quota) => `${quota} Token`,
    },
  ];

  if (!status.enabled) {
    return (
      <div className='mt-[60px] px-4'>
        <Card>
          <Empty
            image={<IconGift size='extra-large' />}
            title={t('签到功能未启用')}
            description={t('管理员尚未开启签到功能')}
          />
        </Card>
      </div>
    );
  }

  return (
    <div className='mt-[60px] px-4'>
      <Spin spinning={loading}>
        <Card
          title={
            <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <IconGift />
              {t('每日签到')}
            </span>
          }
        >
          <div style={{ textAlign: 'center', padding: '20px 0' }}>
            <Title heading={4}>{t('今日日期')}: {status.today_date}</Title>
            <div style={{ margin: '20px 0' }}>
              <Text style={{ fontSize: '16px' }}>
                {t('累计签到')}: <strong>{status.checkin_count}</strong> {t('次')}
              </Text>
              <br />
              <Text style={{ fontSize: '16px' }}>
                {t('累计获得')}: <strong>{status.total_quota}</strong> Token
              </Text>
            </div>
            <Button
              theme='solid'
              type='primary'
              size='large'
              icon={<IconGift />}
              loading={checkinLoading}
              disabled={status.has_checked}
              onClick={handleCheckin}
              style={{ marginTop: '10px' }}
            >
              {status.has_checked ? t('今日已签到') : t('立即签到')}
            </Button>
          </div>
        </Card>

        <Card
          title={
            <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <IconHistory />
              {t('签到记录')}
            </span>
          }
          style={{ marginTop: '16px' }}
        >
          <Table
            columns={columns}
            dataSource={history}
            rowKey='id'
            pagination={{
              pageSize: 10,
            }}
            empty={<Empty description={t('暂无签到记录')} />}
          />
        </Card>
      </Spin>
    </div>
  );
}
