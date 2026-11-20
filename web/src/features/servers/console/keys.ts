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
