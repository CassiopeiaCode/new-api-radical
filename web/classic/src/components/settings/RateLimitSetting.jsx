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
import { Card, Spin, Switch, Toast } from '@douyinfe/semi-ui';

import { API, showError, toBoolean } from '../../helpers';
import { useTranslation } from 'react-i18next';
import RequestRateLimit from '../../pages/Setting/RateLimit/SettingsRequestRateLimit';

const RateLimitSetting = () => {
  const { t } = useTranslation();
  let [inputs, setInputs] = useState({
    ModelRequestRateLimitEnabled: false,
    ModelRequestRateLimitCount: 0,
    ModelRequestRateLimitSuccessCount: 1000,
    ModelRequestRateLimitDurationMinutes: 1,
    ModelRequestRateLimitGroup: '',
  });

  let [loading, setLoading] = useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = {};
      data.forEach((item) => {
        if (item.key === 'ModelRequestRateLimitGroup') {
          item.value = JSON.stringify(JSON.parse(item.value), null, 2);
        }

        if (item.key.endsWith('Enabled')) {
          newInputs[item.key] = toBoolean(item.value);
        } else {
          newInputs[item.key] = item.value;
        }
      });

      setInputs(newInputs);
    } else {
      showError(message);
    }
  };
  async function onRefresh() {
    try {
      setLoading(true);
      await getOptions();
      // showSuccess('刷新成功');
    } catch (error) {
      showError('刷新失败');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    onRefresh();
  }, []);

  const updateForceLeakProtection = async (checked) => {
    setLoading(true);
    try {
      const res = await API.put('/api/option/', {
        key: 'LeakProtectionBalancedForceEnabled',
        value: checked,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      Toast.success({ content: t('更新成功') });
      await getOptions();
    } catch (error) {
      showError(t('更新失败'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <Spin spinning={loading} size='large'>
        {/* AI请求速率限制 */}
        <Card style={{ marginTop: '10px' }}>
          <RequestRateLimit options={inputs} refresh={onRefresh} />
        </Card>
        <Card title={t('出站凭据泄漏防护')} style={{ marginTop: '10px' }}>
          <div className='flex items-center justify-between gap-4'>
            <div className='text-secondary text-sm'>
              {t('开启后在转发前扫描疑似 API 密钥，用户无法在个人设置中关闭；扫描器异常时会安全阻断并记录脱敏错误。')}
            </div>
            <Switch
              checked={!!inputs.LeakProtectionBalancedForceEnabled}
              disabled={loading}
              onChange={updateForceLeakProtection}
            />
          </div>
        </Card>
      </Spin>
    </>
  );
};

export default RateLimitSetting;
