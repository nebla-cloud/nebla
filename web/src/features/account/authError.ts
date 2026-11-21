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

import { reportUnauthorized } from '@/api/client'
import { ApiError, errorMessage } from '@/api/errors'

// Reports a lost session and returns null; otherwise returns the message to show inline.
export function inlineError(e: unknown): string | null {
  if (e instanceof ApiError && e.code === 'unauthorized') {
    reportUnauthorized()
    return null
  }
  return errorMessage(e)
}
