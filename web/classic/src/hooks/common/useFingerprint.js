import { useEffect, useRef } from 'react';
import { API } from '../../helpers';

const REPORT_INTERVAL = 60 * 60 * 1000;

export function useFingerprint(userId) {
  const startedForUser = useRef(null);
  useEffect(() => {
    if (!userId || startedForUser.current === userId) return undefined;
    startedForUser.current = userId;
    const key = `new-api:fingerprint:last-report:${userId}`;
    const last = Number(localStorage.getItem(key));
    if (Number.isFinite(last) && Date.now() - last < REPORT_INTERVAL)
      return undefined;
    let cancelled = false;
    (async () => {
      try {
        const FingerprintJS = await import('@fingerprintjs/fingerprintjs');
        const agent = await FingerprintJS.load();
        const result = await agent.get();
        if (cancelled || !result.visitorId) return;
        const response = await API.post('/api/fingerprint/record', {
          visitor_id: result.visitorId,
        });
        if (response.data?.success)
          localStorage.setItem(key, String(Date.now()));
      } catch (_error) {
        // Optional risk signal: failures must not affect login or app usage.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [userId]);
}
