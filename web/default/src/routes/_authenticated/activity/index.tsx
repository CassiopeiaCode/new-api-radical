/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { createFileRoute } from "@tanstack/react-router";

import { ActiveTaskUsage } from "@/features/active-task-usage";
export const Route = createFileRoute("/_authenticated/activity/")({
  component: ActiveTaskUsage,
});
