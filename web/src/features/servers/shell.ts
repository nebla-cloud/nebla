import type { ComputedRef, InjectionKey } from 'vue'

// True while the shell is following an action for this server.
export const serverBusyKey: InjectionKey<ComputedRef<boolean>> = Symbol('serverBusy')
