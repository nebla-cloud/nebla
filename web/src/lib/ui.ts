// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Shared class strings. Tables stack into rows on mobile and become real tables from sm up.
export const tableWrap = 'overflow-hidden rounded-(--nb-radius-panel) border border-default bg-elevated'
export const table = 'block w-full text-left text-sm sm:table'
export const thead = 'hidden bg-(--nb-soft) text-muted sm:table-header-group'
export const th = 'px-4 py-2.5 font-medium'
export const tbody = 'block divide-y divide-default sm:table-row-group'
export const tr = 'flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3.5 sm:table-row sm:px-0 sm:py-0'
export const td = 'sm:table-cell sm:px-4 sm:py-3'

// The one native-select style; matches UInput's 36px height and --ui-border.
export const selectFullClass =
  'h-9 w-full rounded-[9px] border border-default bg-default px-2.5 text-sm text-highlighted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary'
export const selectClass = `${selectFullClass} sm:w-auto`

// The one style for checkboxes that stay native.
export const checkboxClass =
  'size-4 shrink-0 accent-(--ui-text-highlighted) focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--nb-focus)'
