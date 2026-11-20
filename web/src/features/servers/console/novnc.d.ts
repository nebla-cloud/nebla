// Minimal types for the parts of noVNC that VncScreen uses.
declare module '@novnc/novnc' {
  export interface RFBOptions {
    credentials?: { username?: string; password?: string; target?: string }
    wsProtocols?: string[]
    shared?: boolean
  }
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, url: string, options?: RFBOptions)
    scaleViewport: boolean
    resizeSession: boolean
    focusOnClick: boolean
    background: string
    disconnect(): void
    sendCtrlAltDel(): void
    sendKey(keysym: number, code: string | null, down?: boolean): void
    sendCredentials(credentials: { username?: string; password?: string }): void
    focus(options?: FocusOptions): void
  }
}
