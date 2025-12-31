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

import { useEffect, useRef } from 'react';
import { API } from '../../helpers';

// 上报间隔：1小时（毫秒）
const REPORT_INTERVAL = 60 * 60 * 1000;

// localStorage key
const LAST_REPORT_KEY = 'fp_last_report';
const VISITOR_ID_KEY = 'fp_visitor_id';

// 动态加载 FingerprintJS
const loadFingerprintJS = async () => {
  try {
    const FingerprintJS = await import('@fingerprintjs/fingerprintjs');
    return FingerprintJS.default || FingerprintJS;
  } catch (e) {
    console.warn('FingerprintJS not available:', e);
    return null;
  }
};

// 检查是否需要上报（距离上次上报超过1小时）
const shouldReport = () => {
  const lastReport = localStorage.getItem(LAST_REPORT_KEY);
  if (!lastReport) return true;

  const lastTime = parseInt(lastReport, 10);
  if (isNaN(lastTime)) return true;

  return Date.now() - lastTime >= REPORT_INTERVAL;
};

// 上报指纹到服务器
const reportFingerprint = async (visitorId) => {
  try {
    const res = await API.post('/api/fingerprint/record', {
      visitor_id: visitorId,
    });
    if (res.data?.success) {
      // 记录上报时间
      localStorage.setItem(LAST_REPORT_KEY, Date.now().toString());
    }
  } catch (e) {
    // 静默失败，不影响用户体验
    console.warn('Failed to report fingerprint:', e);
  }
};

// 获取并上报指纹
export const collectAndReportFingerprint = async (force = false) => {
  // 检查是否需要上报
  if (!force && !shouldReport()) {
    return localStorage.getItem(VISITOR_ID_KEY);
  }

  const FingerprintJS = await loadFingerprintJS();
  if (!FingerprintJS) return null;

  try {
    const fp = await FingerprintJS.load();
    const result = await fp.get();
    const visitorId = result.visitorId;

    // 缓存visitor id
    localStorage.setItem(VISITOR_ID_KEY, visitorId);

    // 上报指纹
    await reportFingerprint(visitorId);

    return visitorId;
  } catch (e) {
    console.warn('Failed to collect fingerprint:', e);
    return null;
  }
};

// React Hook
export const useFingerprint = (isLoggedIn = false) => {
  const hasInitialized = useRef(false);

  useEffect(() => {
    if (isLoggedIn && !hasInitialized.current) {
      hasInitialized.current = true;
      collectAndReportFingerprint();
    }
  }, [isLoggedIn]);
};

export default useFingerprint;
