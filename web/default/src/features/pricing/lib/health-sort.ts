/*
Copyright (C) 2023-2026 QuantumNous

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

export function sortModelsByHealth<T extends { model_name: string }>(
  models: T[],
  healthRates: ReadonlyMap<string, number>
): T[] {
  return [...models].sort((a, b) => {
    const aRate = healthRates.get(a.model_name)
    const bRate = healthRates.get(b.model_name)
    const aScore =
      typeof aRate === 'number' && Number.isFinite(aRate) ? aRate : -1
    const bScore =
      typeof bRate === 'number' && Number.isFinite(bRate) ? bRate : -1
    return bScore - aScore || a.model_name.localeCompare(b.model_name)
  })
}
