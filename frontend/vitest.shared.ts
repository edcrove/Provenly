import { mergeConfig, type UserConfig } from 'vite'
import viteConfig from './vite.config'

export const sourceFiles = ['src/**/*.{ts,tsx}']
export const nonSourceFiles = [
  'src/**/*.test.{ts,tsx}',
  'src/test/**',
  'src/contract/**',
  'src/api/schema.d.ts',
  'src/vite-env.d.ts',
]

export function withBase(config: UserConfig) {
  return mergeConfig(viteConfig, config)
}
