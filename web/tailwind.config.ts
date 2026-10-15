import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'media',
  theme: {
    extend: {
      colors: {
        brand: { 50: '#eef5fc', 100: '#d6e7f7', 200: '#9dc3e6', 500: '#2e75b6', 600: '#245e93', 700: '#1c4a74' },
      },
    },
  },
} satisfies Config
