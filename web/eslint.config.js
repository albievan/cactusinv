import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'src/lib/api/schema.d.ts'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: { ecmaVersion: 2022, globals: globals.browser },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // Undefined over plain HTTP (not a secure context), e.g. a phone on the LAN. Use lib/uuid.
      'no-restricted-properties': ['error', { object: 'crypto', property: 'randomUUID', message: 'Use uuid() from lib/uuid; randomUUID is missing on plain HTTP.' }],
    },
  },
)
