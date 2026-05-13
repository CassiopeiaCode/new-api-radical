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
  balancedEnabled,
  onBalancedEnabledChange,
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
            {t('基于默认规则扫描最近消息中的疑似凭据，并在命中时拦截请求')}
          </div>
        </div>
      </div>

      <Card className='!rounded-xl border dark:border-gray-700'>
        <div className='flex flex-col gap-5'>
          <div className='flex items-start gap-4'>
            <div className='w-12 h-12 rounded-full bg-red-50 dark:bg-red-900/20 flex items-center justify-center flex-shrink-0'>
              <ShieldAlert size={20} className='text-red-500' />
            </div>
            <div className='min-w-0 flex-1'>
              <Typography.Title heading={6} className='mb-1'>
                {t('平衡模式')}
              </Typography.Title>
              <Typography.Text type='tertiary' className='text-sm'>
                {t(
                  '默认开启。扫描最后 3 条 user/tool 等价消息，命中默认规则中的疑似泄漏内容时直接拦截。',
                )}
              </Typography.Text>
            </div>
          </div>

          <div className='flex flex-col gap-3 rounded-xl bg-gray-50/80 dark:bg-zinc-900/40 p-4 sm:flex-row sm:items-center sm:justify-between'>
            <div className='min-w-0'>
              <Typography.Text className='block font-medium'>
                {t('启用平衡模式')}
              </Typography.Text>
              <Typography.Text type='tertiary' className='text-sm'>
                {balancedEnabled
                  ? t('当前会自动拦截疑似泄漏请求')
                  : t('当前不执行防泄漏扫描')}
              </Typography.Text>
            </div>
            <div className='flex-shrink-0'>
              <Switch
                checked={balancedEnabled}
                checkedText={t('开')}
                uncheckedText={t('关')}
                onChange={onBalancedEnabledChange}
              />
            </div>
          </div>

          <div className='space-y-2 text-xs text-gray-500 dark:text-gray-400'>
            <div>{t('• 只扫描最近 3 条 user/tool 等价消息')}</div>
            <div>{t('• 使用默认内置规则识别常见密钥、令牌与私钥内容')}</div>
            <div>
              {t('• 如有需要，可在个人设置中关闭该保护')}
            </div>
          </div>

          <div className='flex justify-stretch sm:justify-end'>
            <Button type='primary' onClick={onSave} className='w-full sm:w-auto'>
              {t('保存防泄漏设置')}
            </Button>
          </div>
        </div>
      </Card>
    </Card>
  );
};

export default LeakProtectionSettings;
