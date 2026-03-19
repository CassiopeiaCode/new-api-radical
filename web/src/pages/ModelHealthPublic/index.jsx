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
import { Card, Spin, Typography, Tooltip, Input, Skeleton } from '@douyinfe/semi-ui';
import { IconSearch, IconTickCircle, IconClose } from '@douyinfe/semi-icons';
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
  if (v >= 0.95) return { level: 'excellent', color: '#4dd0e1', bg: 'rgba(77, 208, 225, 0.15)', text: '优秀' };
  if (v >= 0.8) return { level: 'good', color: '#66bb6a', bg: 'rgba(102, 187, 106, 0.15)', text: '良好' };
  if (v >= 0.6) return { level: 'warning', color: '#aed581', bg: 'rgba(174, 213, 129, 0.15)', text: '一般' };
  if (v >= 0.2) return { level: 'poor', color: '#ffb74d', bg: 'rgba(255, 183, 77, 0.15)', text: '欠佳' };
  return { level: 'critical', color: '#ff8a65', bg: 'rgba(255, 138, 101, 0.15)', text: '异常' };
}

function formatTokens(v) {
  const n0 = Number(v) || 0;
  const n = Math.abs(n0);
  const sign = n0 < 0 ? '-' : '';

  if (!Number.isFinite(n) || n === 0) return '0';

  // 统一使用科学计数法：xxxExxx（大写 E，去掉 e+ 的 + 号）
  // 用 3 位有效数字：toExponential(2) => 1.23e+6
  const raw = n.toExponential(2);
  const normalized = raw
    .replace('e+', 'E')
    .replace('e-', 'E-')
    .replace(/E(\d+)/, 'E$1');

  return `${sign}${normalized}`;
}

function percentileNearestRank(values, p) {
  const arr = (values || []).filter((v) => Number.isFinite(v)).slice().sort((a, b) => a - b);
  if (arr.length === 0) return 0;
  const pp = Math.max(0, Math.min(1, Number(p) || 0));
  const idx = Math.floor((arr.length - 1) * pp);
  return Number(arr[idx]) || 0;
}

function HealthCell({ cell, isLatest }) {
  const rate = Number(cell?.success_rate) || 0;
  const isFilled = cell?.is_filled;
  const tokens = Number(cell?.success_tokens) || 0;
  const { color, bg } = getRateLevel(rate);

  return (
    <Tooltip
      content={
        <div className='text-xs p-1'>
          <div className='font-semibold mb-1.5 text-sm'>{hourLabel(cell?.hour_start_ts)}</div>
          <div className='space-y-1'>
            <div>成功率: <span className='font-medium'>{isFilled ? `~${formatRate(rate)}` : formatRate(rate)}</span></div>
            <div>总Token: <span className='font-medium'>{formatTokens(tokens)}</span></div>
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

function StatCardSkeleton({ icon, title, bgGradient, iconBg, valueWidth = 110 }) {
  return (
    <div
      className='relative overflow-hidden rounded-2xl p-5 sm:p-6 min-h-[140px] flex flex-col justify-between shadow-lg'
      style={{
        background: bgGradient,
      }}
    >
      <div
        className='absolute -right-8 -top-8 w-32 h-32 rounded-full opacity-20'
        style={{ backgroundColor: 'rgba(255,255,255,0.3)' }}
      />
      <div
        className='absolute -right-4 -bottom-4 w-24 h-24 rounded-full opacity-15'
        style={{ backgroundColor: 'rgba(255,255,255,0.4)' }}
      />
      <div className='flex items-center justify-between relative z-10'>
        <div className='text-sm font-medium text-white/90 tracking-wide'>{title}</div>
        <div
          className='w-10 h-10 rounded-xl flex items-center justify-center'
          style={{ backgroundColor: iconBg || 'rgba(255,255,255,0.2)' }}
        >
          {icon}
        </div>
      </div>
      <div className='relative z-10 mt-3'>
        <Skeleton.Title
          style={{
            width: valueWidth,
            height: 34,
            marginBottom: 10,
            borderRadius: 10,
          }}
        />
        <Skeleton.Title
          style={{
            width: 80,
            height: 12,
            marginBottom: 0,
            borderRadius: 8,
          }}
        />
      </div>
    </div>
  );
}

function LegendSkeleton() {
  const tagWidths = [86, 92, 78, 96, 82];

  return (
    <Card className='!rounded-2xl mb-6 shadow-sm' bodyStyle={{ padding: '16px 20px' }}>
      <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4'>
        <div className='flex flex-wrap items-center gap-3'>
          <Skeleton.Title style={{ width: 72, height: 14, marginBottom: 0, borderRadius: 8 }} />
          <div className='flex flex-wrap items-center gap-2'>
            {tagWidths.map((w, idx) => (
              <div
                key={idx}
                className='flex items-center gap-2 px-3 py-1.5 rounded-lg bg-gray-50 dark:bg-gray-800/50'
              >
                <Skeleton.Avatar size='extra-small' shape='square' />
                <Skeleton.Button
                  size='small'
                  style={{ width: w, height: 20, borderRadius: 8 }}
                />
              </div>
            ))}
          </div>
        </div>

        {/* 模拟搜索框 */}
        <Skeleton.Button style={{ width: 220, height: 32, borderRadius: 10 }} />
      </div>
    </Card>
  );
}

function TimeLabelsSkeleton() {
  return (
    <div className='mb-3 pl-[200px] sm:pl-[260px] overflow-x-auto'>
      <div className='flex gap-1 min-w-max'>
        {Array.from({ length: 24 }).map((_, idx) => (
          <div key={idx} className='w-7 sm:w-8 text-center'>
            {idx % 3 === 0 && (
              <Skeleton.Title style={{ width: 26, height: 12, marginBottom: 0, borderRadius: 6 }} />
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function ModelListSkeleton() {
  return (
    <div className='space-y-3'>
      {Array.from({ length: 6 }).map((_, idx) => (
        <Card
          key={idx}
          className='!rounded-xl shadow-sm border-l-4'
          style={{ borderLeftColor: 'rgba(148, 163, 184, 0.7)' }}
          bodyStyle={{ padding: '14px 18px' }}
        >
          <div className='flex items-center gap-4'>
            <div className='w-[180px] sm:w-[240px] flex-shrink-0'>
              <div className='flex items-center gap-3'>
                <Skeleton.Title style={{ width: 12, height: 40, marginBottom: 0, borderRadius: 9999 }} />
                <div className='min-w-0 flex-1'>
                  <Skeleton.Title style={{ width: 160, height: 16, marginBottom: 10, borderRadius: 8 }} />
                  <div className='flex items-center gap-3 flex-wrap'>
                    <Skeleton.Title style={{ width: 70, height: 18, marginBottom: 0, borderRadius: 6 }} />
                    <Skeleton.Title style={{ width: 52, height: 14, marginBottom: 0, borderRadius: 6 }} />
                  </div>
                </div>
              </div>
            </div>

            <div className='flex-1 overflow-x-auto'>
              <div className='flex gap-1 min-w-max'>
                {Array.from({ length: 24 }).map((__, jdx) => (
                  <div
                    key={jdx}
                    className='w-7 h-7 sm:w-8 sm:h-8 rounded-lg bg-gray-200 animate-pulse'
                  />
                ))}
              </div>
            </div>
          </div>
        </Card>
      ))}
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
    let totalSuccessTokens = 0;

    const modelData = models.map((modelName) => {
      const hourMap = byModel.get(modelName);
      let modelTotalSuccess = 0;
      let modelTotalSlices = 0;
      let modelTotalTokens = 0;

      const hourlyTokens = hourStarts.map((ts) => Number(hourMap?.get(ts)?.success_tokens) || 0);
      const p10Tokens = percentileNearestRank(
        hourlyTokens.filter((t) => t > 0),
        0.1
      );

      for (const ts of hourStarts) {
        const stat = hourMap?.get(ts);
        const hourTokens = Number(stat?.success_tokens) || 0;
        const hasData = stat && Number(stat.total_slices) > 0;
        const isLowTraffic = p10Tokens > 0 && hourTokens < p10Tokens;
        if (hasData && !isLowTraffic) {
          modelTotalSuccess += Number(stat.success_slices) || 0;
          modelTotalSlices += Number(stat.total_slices) || 0;
        }
        modelTotalTokens += hourTokens;
      }

      const avgRate = modelTotalSlices > 0 ? modelTotalSuccess / modelTotalSlices : 0;
      totalSuccessSlices += modelTotalSuccess;
      totalSlices += modelTotalSlices;
      totalSuccessTokens += modelTotalTokens;

      const { level } = getRateLevel(avgRate);
      if (level === 'excellent' || level === 'good') healthyModels++;
      else if (level === 'warning') warningModels++;
      else if (level === 'critical') criticalModels++;

      const hourlyData = hourStarts.map((ts) => {
        const stat = hourMap?.get(ts);
        const hourTokens = Number(stat?.success_tokens) || 0;
        const hasData = stat && Number(stat.total_slices) > 0;
        const isLowTraffic = p10Tokens > 0 && hourTokens < p10Tokens;
        if (hasData && !isLowTraffic) {
          return stat;
        }
        return {
          hour_start_ts: ts,
          model_name: modelName,
          success_slices: 0,
          total_slices: 0,
          success_rate: avgRate,
          success_tokens: hourTokens,
          is_filled: true,
        };
      });

      return {
        model_name: modelName,
        avg_rate: avgRate,
        total_success: modelTotalSuccess,
        total_slices: modelTotalSlices,
        total_tokens: modelTotalTokens,
        hourly: hourlyData.reverse(),
      };
    });

    modelData.sort((a, b) => (b.total_tokens || 0) - (a.total_tokens || 0));

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
        totalSuccessTokens,
      },
    };
  }, [payload?.rows, hourStarts]);

  const filteredModelData = useMemo(() => {
    if (!searchText.trim()) return modelData;
    const keyword = searchText.toLowerCase().trim();
    return modelData.filter((m) => m.model_name.toLowerCase().includes(keyword));
  }, [modelData, searchText]);

  const latestHour = hourStarts.length > 0 ? hourStarts[hourStarts.length - 1] : null;
  const isInitialLoading = loading && !payload;
  const showSpin = loading && !!payload;

  return (
    <div className='mt-[60px] px-3 sm:px-6 lg:px-8 pb-10 max-w-6xl mx-auto'>
      {/* Header */}
      <div className='mb-8'>
        <div className='flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4'>
          <div>
            <p className='text-sm sm:text-base text-gray-500 mt-2'>
              最近 24 小时各模型运行状态一览，监测所有请求（包括格式不正确导致的错误）
            </p>
          </div>
        </div>
      </div>

      {errorText && (
        <div className='mb-6 p-4 rounded-xl bg-red-50 border border-red-200'>
          <Typography.Text type='danger'>{errorText}</Typography.Text>
        </div>
      )}

      <Spin spinning={showSpin}>
        {/* Stats Cards */}
        <div className='grid grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-6 mb-8'>
          {isInitialLoading ? (
            <>
              <StatCardSkeleton
                icon={<IconTickCircle className='text-white' size='large' />}
                title='监控模型数'
                bgGradient='var(--semi-color-primary-hover)'
                iconBg='rgba(255,255,255,0.25)'
                valueWidth={72}
              />
              <StatCardSkeleton
                icon={<IconTickCircle className='text-white' size='large' />}
                title='整体成功率'
                bgGradient='var(--semi-color-success)'
                iconBg='rgba(255,255,255,0.25)'
                valueWidth={96}
              />
              <StatCardSkeleton
                icon={<IconTickCircle className='text-white' size='large' />}
                title='Token总数'
                bgGradient='var(--semi-color-primary)'
                iconBg='rgba(255,255,255,0.25)'
                valueWidth={120}
              />
              <StatCardSkeleton
                icon={<IconTickCircle className='text-white' size='large' />}
                title='优良模型'
                bgGradient='var(--semi-color-success-active)'
                iconBg='rgba(255,255,255,0.25)'
                valueWidth={72}
              />
            </>
          ) : (
            <>
              <StatCard
                icon={<IconTickCircle className='text-white' size='large' />}
                title='监控模型数'
                value={stats.totalModels}
                subtitle={`${stats.healthyModels} 个健康`}
                color='#4dd0e1'
                bgGradient='var(--semi-color-primary-hover)'
                iconBg='rgba(255,255,255,0.25)'
              />
              <StatCard
                icon={<IconTickCircle className='text-white' size='large' />}
                title='整体成功率'
                value={formatRate(stats.overallRate)}
                subtitle='过去24小时'
                color='#66bb6a'
                bgGradient='var(--semi-color-success)'
                iconBg='rgba(255,255,255,0.25)'
              />
              <StatCard
                icon={<IconTickCircle className='text-white' size='large' />}
                title='Token总数'
                value={formatTokens(stats.totalSuccessTokens)}
                subtitle='过去24小时'
                color='#60a5fa'
                bgGradient='var(--semi-color-primary)'
                iconBg='rgba(255,255,255,0.25)'
              />
              <StatCard
                icon={<IconTickCircle className='text-white' size='large' />}
                title='优良模型'
                value={stats.healthyModels}
                subtitle='成功率 ≥80%'
                color='#0f766e'
                bgGradient='var(--semi-color-success-active)'
                iconBg='rgba(255,255,255,0.25)'
              />
            </>
          )}
        </div>

        {/* Legend */}
        {isInitialLoading ? (
          <LegendSkeleton />
        ) : (
          <Card className='!rounded-2xl mb-6 shadow-sm' bodyStyle={{ padding: '16px 20px' }}>
            <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4'>
              <div className='flex flex-wrap items-center gap-3'>
                <span className='text-sm font-semibold text-gray-700 dark:text-gray-200 mr-2'>状态图例</span>
                <div className='flex flex-wrap items-center gap-2'>
                  <LegendItem color='#4dd0e1' label='优秀 (≥95%)' />
                  <LegendItem color='#66bb6a' label='良好 (80-95%)' />
                  <LegendItem color='#aed581' label='一般 (60-80%)' />
                  <LegendItem color='#ffb74d' label='欠佳 (20-60%)' />
                  <LegendItem color='#ff8a65' label='异常 (<20%)' />
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
        )}

        {/* Time Labels */}
        {isInitialLoading ? (
          <TimeLabelsSkeleton />
        ) : (
          hourStarts.length > 0 && (
            <div className='mb-3 pl-[200px] sm:pl-[260px] overflow-x-auto'>
              <div className='flex gap-1 min-w-max'>
                {[...hourStarts].reverse().map((ts, idx) => (
                  <div key={ts} className='w-7 sm:w-8 text-center'>
                    {idx % 3 === 0 && (
                      <div className='text-[11px] font-medium text-gray-400'>
                        {hourLabel(ts)}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )
        )}

        {/* Model Health Grid */}
        {isInitialLoading ? (
          <ModelListSkeleton />
        ) : (
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
                          <div className='flex items-center gap-3 text-xs sm:text-sm mt-1 flex-wrap'>
                            <span className='font-bold px-2 py-0.5 rounded-md' style={{ color, backgroundColor: bg }}>
                              {formatRate(model.avg_rate)}
                            </span>
                            <span className='text-gray-400 font-medium'>
                              {formatTokens(model.total_tokens)}
                            </span>
                          </div>
                        </div>
                      </div>
                    </div>

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
        )}

        {!loading && !isInitialLoading && filteredModelData.length === 0 && (
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
