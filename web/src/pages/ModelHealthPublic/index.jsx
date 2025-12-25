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

import React, { useEffect, useMemo, useState } from 'react';
import { Card, Spin, Typography, Button, Avatar, Tooltip, Input } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch, IconTickCircle, IconAlertTriangle, IconClose } from '@douyinfe/semi-icons';
import { API, showError, timestamp2string } from '../../helpers';

function formatRate(rate) {
  if (!Number.isFinite(rate)) return '0.00%';
  return `${(rate * 100).toFixed(2)}%`;
}

function hourLabel(tsSec) {
  const full = timestamp2string(tsSec);
  return full.slice(11, 13) + ':00';
}

function getRateLevel(rate) {
  const v = Number(rate) || 0;
  if (v >= 0.99) return { level: 'excellent', color: '#4dd0e1', bg: 'rgba(77, 208, 225, 0.15)', text: '优秀' };
  if (v >= 0.95) return { level: 'good', color: '#66bb6a', bg: 'rgba(102, 187, 106, 0.15)', text: '良好' };
  if (v >= 0.8) return { level: 'warning', color: '#aed581', bg: 'rgba(174, 213, 129, 0.15)', text: '警告' };
  if (v >= 0.5) return { level: 'poor', color: '#ffb74d', bg: 'rgba(255, 183, 77, 0.15)', text: '较差' };
  return { level: 'critical', color: '#ff8a65', bg: 'rgba(255, 138, 101, 0.15)', text: '严重' };
}

function HealthCell({ cell, isLatest }) {
  const rate = Number(cell?.success_rate) || 0;
  const total = Number(cell?.total_slices) || 0;
  const success = Number(cell?.success_slices) || 0;
  const isFilled = cell?.is_filled;
  const { color, bg } = getRateLevel(rate);

  return (
    <Tooltip
      content={
        <div className='text-xs p-1'>
          <div className='font-semibold mb-1.5 text-sm'>{hourLabel(cell?.hour_start_ts)}</div>
          <div className='space-y-1'>
            <div>成功率: <span className='font-medium'>{isFilled ? `~${formatRate(rate)}` : formatRate(rate)}</span></div>
            {!isFilled && <div>成功/总计: <span className='font-medium'>{success}/{total}</span></div>}
            {isFilled && <div className='text-gray-400 italic'>无数据，使用平均值</div>}
          </div>
        </div>
      }
    >
      <div
        className={`w-7 h-7 sm:w-8 sm:h-8 rounded-lg cursor-pointer transition-all duration-200 hover:scale-110 hover:shadow-lg ${isLatest ? 'ring-2 ring-offset-2' : ''}`}
        style={{
          backgroundColor: isFilled ? `${bg}` : bg,
          borderColor: color,
          boxShadow: isFilled ? 'none' : `inset 0 0 0 2.5px ${color}`,
          opacity: isFilled ? 0.5 : 1,
          '--tw-ring-color': isLatest ? color : 'transparent',
        }}
      />
    </Tooltip>
  );
}


function StatCard({ icon, title, value, subtitle, color, bgGradient, iconBg }) {
  return (
    <div
      className='relative overflow-hidden rounded-2xl p-5 sm:p-6 min-h-[140px] flex flex-col justify-between shadow-lg hover:shadow-xl transition-shadow duration-300'
      style={{
        background: bgGradient,
      }}
    >
      {/* 背景装饰 */}
      <div
        className='absolute -right-8 -top-8 w-32 h-32 rounded-full opacity-20'
        style={{ backgroundColor: 'rgba(255,255,255,0.3)' }}
      />
      <div
        className='absolute -right-4 -bottom-4 w-24 h-24 rounded-full opacity-15'
        style={{ backgroundColor: 'rgba(255,255,255,0.4)' }}
      />
      
      {/* 顶部：图标和标题 */}
      <div className='flex items-center justify-between relative z-10'>
        <div className='text-sm font-medium text-white/90 tracking-wide'>{title}</div>
        <div
          className='w-10 h-10 rounded-xl flex items-center justify-center'
          style={{ backgroundColor: iconBg || 'rgba(255,255,255,0.2)' }}
        >
          {icon}
        </div>
      </div>
      
      {/* 底部：数值和副标题 */}
      <div className='relative z-10 mt-3'>
        <div className='text-3xl sm:text-4xl font-bold text-white tracking-tight'>{value}</div>
        {subtitle && (
          <div className='text-xs sm:text-sm text-white/70 mt-1.5 font-medium'>{subtitle}</div>
        )}
      </div>
    </div>
  );
}

function LegendItem({ color, label }) {
  return (
    <div className='flex items-center gap-2 px-3 py-1.5 rounded-lg bg-gray-50 dark:bg-gray-800/50'>
      <div
        className='w-4 h-4 rounded-md shadow-sm'
        style={{ backgroundColor: color }}
      />
      <span className='text-xs font-medium text-gray-600 dark:text-gray-300'>{label}</span>
    </div>
  );
}

export default function ModelHealthPublicPage() {
  const [loading, setLoading] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [payload, setPayload] = useState(null);
  const [searchText, setSearchText] = useState('');

  async function load() {
    setLoading(true);
    setErrorText('');
    try {
      const res = await API.get('/api/public/model_health/hourly_last24h', {
        skipErrorHandler: true,
      });
      const { success, message, data } = res.data || {};
      if (!success) {
        const errMsg = message || '加载失败';
        setErrorText(errMsg);
        showError(errMsg);
        return;
      }

      if (!data || typeof data !== 'object') {
        const errMsg = '接口返回结构异常';
        setErrorText(errMsg);
        showError(errMsg);
        return;
      }

      setPayload(data);
    } catch (e) {
      setErrorText('加载失败');
      showError(e);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load().catch(console.error);
  }, []);

  const hourStarts = useMemo(() => {
    const start = Number(payload?.start_hour);
    const end = Number(payload?.end_hour);
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return [];
    const hours = [];
    for (let ts = start; ts < end; ts += 3600) {
      hours.push(ts);
    }
    return hours;
  }, [payload?.start_hour, payload?.end_hour]);

  const { modelData, stats } = useMemo(() => {
    const rows = Array.isArray(payload?.rows) ? payload.rows : [];

    const byModel = new Map();
    for (const r of rows) {
      const name = r?.model_name || '';
      if (!name) continue;
      if (!byModel.has(name)) byModel.set(name, new Map());
      byModel.get(name).set(Number(r.hour_start_ts), r);
    }

    const models = Array.from(byModel.keys());
    let totalModels = models.length;
    let healthyModels = 0;
    let warningModels = 0;
    let criticalModels = 0;
    let totalSuccessSlices = 0;
    let totalSlices = 0;

    const modelData = models.map((modelName) => {
      const hourMap = byModel.get(modelName);
      let modelTotalSuccess = 0;
      let modelTotalSlices = 0;

      for (const ts of hourStarts) {
        const stat = hourMap?.get(ts);
        if (stat && Number(stat.total_slices) > 0) {
          modelTotalSuccess += Number(stat.success_slices) || 0;
          modelTotalSlices += Number(stat.total_slices) || 0;
        }
      }

      const avgRate = modelTotalSlices > 0 ? modelTotalSuccess / modelTotalSlices : 0;
      totalSuccessSlices += modelTotalSuccess;
      totalSlices += modelTotalSlices;

      const { level } = getRateLevel(avgRate);
      if (level === 'excellent' || level === 'good') healthyModels++;
      else if (level === 'warning') warningModels++;
      else criticalModels++;

      const hourlyData = hourStarts.map((ts) => {
        const stat = hourMap?.get(ts);
        if (stat && Number(stat.total_slices) > 0) {
          return stat;
        }
        return {
          hour_start_ts: ts,
          model_name: modelName,
          success_slices: 0,
          total_slices: 0,
          success_rate: avgRate,
          is_filled: true,
        };
      });

      return {
        model_name: modelName,
        avg_rate: avgRate,
        total_success: modelTotalSuccess,
        total_slices: modelTotalSlices,
        hourly: hourlyData.reverse(),
      };
    });

    modelData.sort((a, b) => b.total_success - a.total_success);

    const overallRate = totalSlices > 0 ? totalSuccessSlices / totalSlices : 0;

    return {
      modelData,
      stats: {
        totalModels,
        healthyModels,
        warningModels,
        criticalModels,
        overallRate,
        totalSuccessSlices,
        totalSlices,
      },
    };
  }, [payload?.rows, hourStarts]);

  const filteredModelData = useMemo(() => {
    if (!searchText.trim()) return modelData;
    const keyword = searchText.toLowerCase().trim();
    return modelData.filter((m) => m.model_name.toLowerCase().includes(keyword));
  }, [modelData, searchText]);

  const latestHour = hourStarts.length > 0 ? hourStarts[hourStarts.length - 1] : null;


  return (
    <div className='mt-[60px] px-3 sm:px-6 lg:px-8 pb-10 max-w-[1800px] mx-auto'>
      {/* Header */}
      <div className='mb-8'>
        <div className='flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4'>
          <div>
            <h1 className='text-2xl sm:text-3xl lg:text-4xl font-bold bg-gradient-to-r from-purple-600 via-blue-600 to-cyan-500 bg-clip-text text-transparent'>
              模型健康度监控
            </h1>
            <p className='text-sm sm:text-base text-gray-500 mt-2'>
              最近 24 小时各模型运行状态一览
            </p>
          </div>
          <Button
            icon={<IconRefresh />}
            onClick={load}
            loading={loading}
            theme='solid'
            size='large'
            style={{
              background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
              border: 'none',
              borderRadius: '12px',
              padding: '0 24px',
              height: '44px',
            }}
          >
            刷新数据
          </Button>
        </div>
      </div>

      {errorText && (
        <div className='mb-6 p-4 rounded-xl bg-red-50 border border-red-200'>
          <Typography.Text type='danger'>{errorText}</Typography.Text>
        </div>
      )}

      <Spin spinning={loading}>
        {/* Stats Cards - 更大气的统计卡片 */}
        <div className='grid grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-6 mb-8'>
          <StatCard
            icon={<IconTickCircle className='text-white' size='large' />}
            title='监控模型数'
            value={stats.totalModels}
            subtitle={`${stats.healthyModels} 个健康`}
            color='#4dd0e1'
            bgGradient='linear-gradient(135deg, #4dd0e1 0%, #3ba8b6 100%)'
            iconBg='rgba(255,255,255,0.25)'
          />
          <StatCard
            icon={<IconTickCircle className='text-white' size='large' />}
            title='整体成功率'
            value={formatRate(stats.overallRate)}
            subtitle={`${stats.totalSuccessSlices}/${stats.totalSlices} 时间片`}
            color='#66bb6a'
            bgGradient='linear-gradient(135deg, #66bb6a 0%, #4a9c5d 100%)'
            iconBg='rgba(255,255,255,0.25)'
          />
          <StatCard
            icon={<IconAlertTriangle className='text-white' size='large' />}
            title='警告模型'
            value={stats.warningModels}
            subtitle='成功率 80-95%'
            color='#aed581'
            bgGradient='linear-gradient(135deg, #aed581 0%, #8fb86a 100%)'
            iconBg='rgba(255,255,255,0.25)'
          />
          <StatCard
            icon={<IconClose className='text-white' size='large' />}
            title='异常模型'
            value={stats.criticalModels}
            subtitle='成功率 < 80%'
            color='#ff8a65'
            bgGradient='linear-gradient(135deg, #ff8a65 0%, #d97350 100%)'
            iconBg='rgba(255,255,255,0.25)'
          />
        </div>

        {/* Legend - 更精致的图例 */}
        <Card 
          className='!rounded-2xl mb-6 shadow-sm' 
          bodyStyle={{ padding: '16px 20px' }}
        >
          <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4'>
            <div className='flex flex-wrap items-center gap-3'>
              <span className='text-sm font-semibold text-gray-700 dark:text-gray-200 mr-2'>状态图例</span>
              <div className='flex flex-wrap items-center gap-2'>
                <LegendItem color='#4dd0e1' label='优秀 (≥99%)' />
                <LegendItem color='#66bb6a' label='良好 (95-99%)' />
                <LegendItem color='#aed581' label='警告 (80-95%)' />
                <LegendItem color='#ffb74d' label='较差 (50-80%)' />
                <LegendItem color='#ff8a65' label='严重 (<50%)' />
              </div>
            </div>
            <Input
              prefix={<IconSearch />}
              placeholder='搜索模型...'
              value={searchText}
              onChange={setSearchText}
              showClear
              style={{ width: 220, borderRadius: '10px' }}
            />
          </div>
        </Card>

        {/* Time Labels - 更清晰的时间标签 */}
        {hourStarts.length > 0 && (
          <div className='mb-3 pl-[200px] sm:pl-[260px] overflow-x-auto'>
            <div className='flex gap-1 min-w-max'>
              {[...hourStarts].reverse().map((ts, idx) => (
                <div
                  key={ts}
                  className='w-7 sm:w-8 text-center'
                >
                  {idx % 3 === 0 && (
                    <div className='text-[11px] font-medium text-gray-400'>
                      {hourLabel(ts)}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Model Health Grid - 更整齐的健康度网格 */}
        <div className='space-y-3'>
          {filteredModelData.map((model) => {
            const { color, bg } = getRateLevel(model.avg_rate);
            return (
              <Card
                key={model.model_name}
                className='!rounded-xl hover:shadow-lg transition-all duration-300 border-l-4'
                style={{ borderLeftColor: color }}
                bodyStyle={{ padding: '14px 18px' }}
              >
                <div className='flex items-center gap-4'>
                  {/* Model Info - 更宽的模型信息区 */}
                  <div className='w-[180px] sm:w-[240px] flex-shrink-0'>
                    <div className='flex items-center gap-3'>
                      <div
                        className='w-3 h-10 rounded-full flex-shrink-0 shadow-sm'
                        style={{ backgroundColor: color }}
                      />
                      <div className='min-w-0 flex-1'>
                        <Tooltip content={model.model_name}>
                          <div className='font-semibold text-sm sm:text-base truncate text-gray-800 dark:text-gray-100'>
                            {model.model_name}
                          </div>
                        </Tooltip>
                        <div className='flex items-center gap-3 text-xs sm:text-sm mt-1'>
                          <span 
                            className='font-bold px-2 py-0.5 rounded-md'
                            style={{ color, backgroundColor: bg }}
                          >
                            {formatRate(model.avg_rate)}
                          </span>
                          <span className='text-gray-400 font-medium'>
                            {model.total_success}/{model.total_slices}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Health Grid - 更大的健康度方块 */}
                  <div className='flex-1 overflow-x-auto'>
                    <div className='flex gap-1 min-w-max'>
                      {model.hourly.map((cell) => (
                        <HealthCell
                          key={cell.hour_start_ts}
                          cell={cell}
                          isLatest={cell.hour_start_ts === latestHour}
                        />
                      ))}
                    </div>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>

        {!loading && filteredModelData.length === 0 && (
          <Card className='!rounded-2xl shadow-sm'>
            <div className='text-center py-16'>
              <div className='text-7xl mb-6'>📊</div>
              <Typography.Title heading={4} type='tertiary'>
                {searchText ? '未找到匹配的模型' : '暂无数据'}
              </Typography.Title>
              <Typography.Text type='tertiary' className='text-base'>
                {searchText ? '请尝试其他搜索关键词' : '请稍后刷新重试'}
              </Typography.Text>
            </div>
          </Card>
        )}
      </Spin>
    </div>
  );
}
