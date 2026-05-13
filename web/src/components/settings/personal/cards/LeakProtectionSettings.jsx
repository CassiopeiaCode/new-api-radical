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

import React from 'react';
import { Avatar, Button, Card, Switch, Typography } from '@douyinfe/semi-ui';
import { ShieldAlert } from 'lucide-react';

const LeakProtectionSettings = ({
  t,
  strictEnabled,
  onStrictEnabledChange,
  onSave,
}) => {
  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='red' className='mr-3 shadow-md'>
          <ShieldAlert size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t('防泄漏管理')}
          </Typography.Text>
          <div className='text-xs text-gray-600 dark:text-gray-400'>
            {t('扫描最近消息中的高熵凭据样式内容并在命中时拦截请求')}
          </div>
        </div>
      </div>

      <Card className='!rounded-xl border dark:border-gray-700'>
        <div className='flex flex-col gap-4'>
          <div className='flex items-start justify-between gap-4'>
            <div className='pr-4'>
              <Typography.Title heading={6} className='mb-1'>
                {t('严格模式')}
              </Typography.Title>
              <Typography.Text type='tertiary' className='text-sm'>
                {t(
                  '开启后，将扫描最后3条用户或工具消息；若发现高熵随机串、复合UUID样式凭据或其他疑似泄漏内容，则直接拒绝请求。',
                )}
              </Typography.Text>
            </div>
            <Switch
              checked={strictEnabled}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={onStrictEnabledChange}
            />
          </div>

          <div className='text-xs text-gray-500 dark:text-gray-400 space-y-2'>
            <div>{t('默认开启；关闭后不执行该项扫描。')}</div>
            <div>
              {t(
                '该模式优先按高熵随机串判定，不会因为单独出现 api_key、PRIVATE KEY 等说明性文字就误拦。',
              )}
            </div>
          </div>

          <div className='flex justify-end'>
            <Button type='primary' onClick={onSave}>
              {t('保存防泄漏设置')}
            </Button>
          </div>
        </div>
      </Card>
    </Card>
  );
};

export default LeakProtectionSettings;
