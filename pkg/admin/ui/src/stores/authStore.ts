import { create } from 'zustand'
import type { User } from '@/types'
import * as api from '@/services/api'

interface AuthState {
  user: User | null
  isLoading: boolean
  isAuthenticated: boolean
  setUser: (user: User | null) => void
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  checkAuth: () => Promise<void>
}

export const useAuth = create<AuthState>((set) => ({
  user: null,
  isLoading: true,
  isAuthenticated: false,

  setUser: (user) => set({ user, isAuthenticated: !!user }),

  login: async (username: string, password: string) => {
    set({ isLoading: true })
    try {
      const user = await api.login(username, password)
      set({ user, isLoading: false, isAuthenticated: true })
    } catch (error) {
      set({ isLoading: false })
      throw error
    }
  },

  logout: async () => {
    await api.logout()
    set({ user: null, isAuthenticated: false })
  },

  checkAuth: async () => {
    set({ isLoading: true })
    try {
      const user = await api.getCurrentUser()
      set({ user, isLoading: false, isAuthenticated: !!user })
    } catch {
      set({ user: null, isLoading: false, isAuthenticated: false })
    }
  },
}))
