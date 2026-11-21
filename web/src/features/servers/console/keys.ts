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

// Paste is limited to what a US keyboard can type: printable ASCII, Return and Tab.
export const XK_SHIFT_L = 0xffe1
const XK_RETURN = 0xff0d
const XK_TAB = 0xff09
const SHIFTED = new Set('~!@#$%^&*()_+{}|:"<>?')

export interface Keystroke {
  keysym: number
  shift: boolean
}

function normalize(text: string): string {
  return text.replace(/\r\n?/g, '\n')
}

function typeable(ch: string): boolean {
  return ch === '\n' || ch === '\t' || (ch >= ' ' && ch <= '~')
}

// Distinct characters that can't be typed, in the order they first appear.
export function untypeable(text: string): string[] {
  return [...new Set([...normalize(text)].filter((ch) => !typeable(ch)))]
}

// Null when any character can't be typed.
export function keystrokes(text: string): Keystroke[] | null {
  const t = normalize(text)
  if (untypeable(t).length) return null
  return [...t].map((ch) => {
    if (ch === '\n') return { keysym: XK_RETURN, shift: false }
    if (ch === '\t') return { keysym: XK_TAB, shift: false }
    return { keysym: ch.charCodeAt(0), shift: (ch >= 'A' && ch <= 'Z') || SHIFTED.has(ch) }
  })
}
