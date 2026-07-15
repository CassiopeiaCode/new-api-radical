/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { SectionPageLayout } from "@/components/layout";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";

type Stats = {
  global_active_slots: number;
  global_limit: number;
  user_limit: number;
  lease_seconds: number;
  active_users: number;
  rank: Array<{ user_id: number; username: string; active_slots: number }>;
};
type History = {
  id: number;
  created_at: number;
  user_id: number;
  username: string;
  active_slots: number;
  global_active_slots: number;
};

export function ActiveTasks() {
  const { i18n, t } = useTranslation();
  const [stats, setStats] = useState<Stats | null>(null);
  const [history, setHistory] = useState<History[]>([]);
  const [loading, setLoading] = useState(false);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [statsResponse, historyResponse] = await Promise.all([
        api.get("/api/active-task/stats"),
        api.get("/api/active-task/history?p=1&page_size=100"),
      ]);
      setStats(statsResponse.data?.data ?? null);
      setHistory(historyResponse.data?.data?.items ?? []);
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => void load(), [load]);
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t("Active tasks")}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button onClick={() => void load()} disabled={loading}>
          {loading ? t("Loading...") : t("Refresh")}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className="space-y-6">
          <div className="grid gap-3 sm:grid-cols-3">
            <Metric
              label={t("Active slots")}
              value={`${stats?.global_active_slots ?? 0} / ${stats?.global_limit ?? 0}`}
            />
            <Metric
              label={t("Active users")}
              value={stats?.active_users ?? 0}
            />
            <Metric
              label={t("Per-user limit")}
              value={stats?.user_limit ?? 0}
            />
          </div>
          <section>
            <h2 className="mb-2 text-lg font-semibold">{t("Current usage")}</h2>
            <DataTable
              rows={stats?.rank ?? []}
              locale={i18n.language}
              labels={{
                time: t('Time'),
                user: t('User'),
                userID: t('User ID'),
                activeSlots: t('Active slots'),
                globalSlots: t('Global slots'),
              }}
            />
          </section>
          <section>
            <h2 className="mb-2 text-lg font-semibold">
              {t("Active task history")}
            </h2>
            <DataTable
              rows={history}
              history
              locale={i18n.language}
              labels={{
                time: t('Time'),
                user: t('User'),
                userID: t('User ID'),
                activeSlots: t('Active slots'),
                globalSlots: t('Global slots'),
              }}
            />
          </section>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  );
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md border p-4">
      <p className="text-muted-foreground text-sm">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
    </div>
  );
}

function DataTable({
  rows,
  history = false,
  locale,
  labels,
}: {
  rows: Array<Record<string, unknown>>;
  history?: boolean;
  locale: string;
  labels: {
    time: string;
    user: string;
    userID: string;
    activeSlots: string;
    globalSlots: string;
  };
}) {
  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-sm">
        <thead className="bg-muted/40 text-left">
          <tr>
            {history && <th className="p-2">{labels.time}</th>}
            <th className="p-2">{labels.user}</th>
            <th className="p-2">{labels.userID}</th>
            <th className="p-2">{labels.activeSlots}</th>
            {history && <th className="p-2">{labels.globalSlots}</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr
              key={`${row.user_id}-${row.created_at ?? index}`}
              className="border-t"
            >
              {history && (
                <td className="p-2">
                  {new Date(Number(row.created_at) * 1000).toLocaleString(
                    locale
                  )}
                </td>
              )}
              <td className="p-2">{String(row.username ?? "")}</td>
              <td className="p-2">{String(row.user_id)}</td>
              <td className="p-2">{String(row.active_slots)}</td>
              {history && (
                <td className="p-2">{String(row.global_active_slots)}</td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
