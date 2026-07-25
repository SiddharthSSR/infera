# Self-hosted web fonts

These files are the Latin, normal-style WOFF2 subsets copied from the
Fontsource 5.3.0 npm packages. They replace the previous runtime Google Fonts
stylesheet request and are served from the same origin as the application.

## DM Sans

- Source package: `@fontsource/dm-sans@5.3.0`
- Upstream font version: v17
- Upstream project: https://github.com/googlefonts/dm-fonts
- Fontsource package: https://www.npmjs.com/package/@fontsource/dm-sans/v/5.3.0
- License: SIL Open Font License 1.1; see `LICENSE-DM-SANS.txt`
- Included weights: 400, 500, 600, and 700

## Space Mono

- Source package: `@fontsource/space-mono@5.3.0`
- Upstream font version: v17
- Upstream project: https://github.com/googlefonts/spacemono
- Fontsource package: https://www.npmjs.com/package/@fontsource/space-mono/v/5.3.0
- License: SIL Open Font License 1.1; see `LICENSE-SPACE-MONO.txt`
- Included weight: 400

Only the weights used by the application's former Google Fonts request are
included. SHA-256 checksums:

```text
4ab51eb2cd7305d177187908d6397474d4520663f6c6e572feb0a64f4fa80006  dm-sans-latin-400-normal.woff2
19bf1984956517c35c2bd35b6cdedac12a21d6fcd3596c614ecdfb88b648909d  dm-sans-latin-500-normal.woff2
6bb2b2645ba5eeaecf56322c543fa3a75b87b927977b9c03b1dabc4205089120  dm-sans-latin-600-normal.woff2
35c5efa0e5daa52ee5c6500f5be354bf751fb65c4e49e1d6806c6eb5883e8fe9  dm-sans-latin-700-normal.woff2
fb4a81a2d0a893e5c38c394a7e716a1cef0b24610a0af49c96f6d529bd66bf2b  space-mono-latin-400-normal.woff2
```
