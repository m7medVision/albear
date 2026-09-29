// The build target this bundle was built for, injected by vite's define from
// ALBEAR_ENV / ALBEAR_VERSION (see build/target.ts).
import type { RuntimeBuild } from '../build/target'

declare const __ALBEAR_BUILD__: RuntimeBuild

export const BUILD: RuntimeBuild = __ALBEAR_BUILD__
