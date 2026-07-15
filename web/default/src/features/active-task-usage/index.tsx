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

type Usage = { model_name: string; token_used: number; request_count: number };

export function ActiveTaskUsage() {
  const { t } = useTranslation();
  const [items, setItems] = useState<Usage[]>([]);
  const [loading, setLoading] = useState(false);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const response = await api.get("/api/active-task/usage/self");
      setItems(response.data?.data?.items ?? []);
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => void load(), [load]);
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t("My model usage")}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button onClick={() => void load()} disabled={loading}>
          {loading ? t("Loading...") : t("Refresh")}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <p className="text-muted-foreground mb-4 text-sm">
          {t("Token usage by model for the last 24 hours.")}
        </p>
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-left">
              <tr>
                <th className="p-2">{t("Model")}</th>
                <th className="p-2">{t("Tokens")}</th>
                <th className="p-2">{t("Requests")}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr className="border-t" key={item.model_name}>
                  <td className="p-2">{item.model_name}</td>
                  <td className="p-2">{item.token_used.toLocaleString()}</td>
                  <td className="p-2">{item.request_count.toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  );
}
