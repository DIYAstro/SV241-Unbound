import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

export const useThemeStore = defineStore('theme', () => {
    // Available themes
    const THEMES = {
        DEFAULT: 'default',
        DARK: 'dark',
        RED: 'red'
    }

    // Initialize from localStorage or default to DEFAULT
    const currentTheme = ref(localStorage.getItem('theme') || THEMES.DARK)

    // Apply theme to document root
    const applyTheme = (theme) => {
        document.documentElement.setAttribute('data-theme', theme)
    }

    // Watch for theme changes and persist to localStorage
    watch(currentTheme, (newTheme) => {
        localStorage.setItem('theme', newTheme)
        applyTheme(newTheme)
    })

    // Set theme
    const setTheme = (theme) => {
        if (Object.values(THEMES).includes(theme)) {
            currentTheme.value = theme
        }
    }

    // Toggle between themes
    const toggleTheme = () => {
        const themeOrder = [THEMES.DARK, THEMES.DEFAULT, THEMES.RED]
        const currentIndex = themeOrder.indexOf(currentTheme.value)
        const nextIndex = (currentIndex + 1) % themeOrder.length
        currentTheme.value = themeOrder[nextIndex]
    }

    // Initialize theme on load
    applyTheme(currentTheme.value)

    return {
        currentTheme,
        THEMES,
        setTheme,
        toggleTheme
    }
})
