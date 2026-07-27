import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repositoryRoot = resolve(frontendRoot, '..')

const readFrontendFile = (path: string) =>
  readFileSync(resolve(frontendRoot, path), 'utf8')

describe('static font and favicon assets', () => {
  it('loads fonts only from local WOFF2 assets', () => {
    const html = readFrontendFile('index.html')
    const css = readFrontendFile('src/index.css')
    const remoteFontPattern = /fonts\.(?:googleapis|gstatic)\.com/i

    expect(html).not.toMatch(remoteFontPattern)
    expect(css).not.toMatch(remoteFontPattern)

    const expectedFaces = [
      ['DM Sans', '400', 'dm-sans-latin-400-normal.woff2'],
      ['DM Sans', '500', 'dm-sans-latin-500-normal.woff2'],
      ['DM Sans', '600', 'dm-sans-latin-600-normal.woff2'],
      ['DM Sans', '700', 'dm-sans-latin-700-normal.woff2'],
      ['Space Mono', '400', 'space-mono-latin-400-normal.woff2'],
    ]
    const fontFaceBlocks = css.match(/@font-face\s*{[^}]+}/g) ?? []

    for (const [family, weight, filename] of expectedFaces) {
      const fontPath = resolve(frontendRoot, 'public/fonts', filename)
      const fontFace = fontFaceBlocks.find(
        (block) =>
          block.includes(`font-family: "${family}"`) &&
          block.includes(`font-weight: ${weight}`),
      )

      expect(fontFace).toContain(
        `url("/fonts/${filename}") format("woff2")`,
      )
      expect(existsSync(fontPath)).toBe(true)
      expect(readFileSync(fontPath).subarray(0, 4).toString('ascii')).toBe('wOF2')
    }

    expect(fontFaceBlocks).toHaveLength(expectedFaces.length)
    expect(css.match(/font-display: swap/g)).toHaveLength(expectedFaces.length)
    expect(css).toContain(
      '--font-main: "DM Sans", "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    )
    expect(css).toContain('--font-mono: "Space Mono", monospace')
    expect(existsSync(resolve(frontendRoot, 'public/fonts/README.md'))).toBe(true)
    expect(
      existsSync(resolve(frontendRoot, 'public/fonts/LICENSE-DM-SANS.txt')),
    ).toBe(true)
    expect(
      existsSync(resolve(frontendRoot, 'public/fonts/LICENSE-SPACE-MONO.txt')),
    ).toBe(true)
  })

  it('declares both SVG and ICO icons and ships each fallback asset', () => {
    const html = readFrontendFile('index.html')

    expect(html).toMatch(
      /<link rel="icon" href="\/favicon\.ico" sizes="32x32" \/>/,
    )
    expect(html).toMatch(
      /<link rel="icon" type="image\/svg\+xml" href="\/favicon\.svg" sizes="any" \/>/,
    )
    expect(existsSync(resolve(frontendRoot, 'public/favicon.svg'))).toBe(true)
    expect(existsSync(resolve(frontendRoot, 'public/favicon.ico'))).toBe(true)
  })

  it('keeps the production font CSP self-hosted and caches font and icon assets', () => {
    const nginx = readFileSync(
      resolve(repositoryRoot, 'deploy/docker/nginx.conf'),
      'utf8',
    )

    expect(nginx).toContain("font-src 'self' data:")
    expect(nginx).not.toMatch(/font-src[^;]*https?:/i)

    const staticLocationStart = nginx.indexOf(
      'location ~* \\.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$ {',
    )
    const staticLocationEnd = nginx.indexOf('\n    }', staticLocationStart)

    expect(staticLocationStart).toBeGreaterThanOrEqual(0)
    expect(staticLocationEnd).toBeGreaterThan(staticLocationStart)

    const staticLocation = nginx.slice(staticLocationStart, staticLocationEnd)
    expect(staticLocation).toContain('expires 1y;')
    expect(staticLocation).toContain(
      'add_header Cache-Control "public, immutable";',
    )
  })
})
