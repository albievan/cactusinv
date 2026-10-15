import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Cactus green brand on a warm cream canvas.
        brand: {
          50: '#eef4f0', 100: '#dbe8df', 200: '#b5cfbd', 300: '#8db296', 400: '#5f8c6c',
          500: '#3f6f50', 600: '#2f5d46', 700: '#244a38', 800: '#1b382b', 900: '#132a20', 950: '#0d1d16',
        },
        cream: { 50: '#fbf9f4', 100: '#f6f3ec', 200: '#efe9dc', 300: '#e4dbc8' },
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
      },
    },
  },
} satisfies Config
