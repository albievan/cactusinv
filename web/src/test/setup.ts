import '@testing-library/jest-dom/vitest'
import { configure } from '@testing-library/react'

// The whole suite runs in parallel jsdom workers; on a busy machine a screen can take more
// than the default 1 s to appear, which failed tests at random.
configure({ asyncUtilTimeout: 5000 })
