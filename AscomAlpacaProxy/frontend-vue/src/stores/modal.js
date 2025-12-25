import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useModalStore = defineStore('modal', () => {
    const isVisible = ref(false)
    const modalConfig = ref({
        icon: '',
        title: '',
        message: '',
        buttons: []
    })

    function show(config) {
        modalConfig.value = {
            icon: config.icon || '',
            title: config.title || '',
            message: config.message || '',
            buttons: config.buttons || [{ text: 'OK', action: close, primary: true }]
        }
        isVisible.value = true
    }

    function close() {
        isVisible.value = false
        modalConfig.value = { icon: '', title: '', message: '', buttons: [] }
    }

    // Convenience methods
    function success(message, title = 'Success') {
        show({
            icon: '✅',
            title,
            message,
            buttons: [{ text: 'OK', action: close, primary: true }]
        })
    }

    function error(message, title = 'Error') {
        show({
            icon: '❌',
            title,
            message,
            buttons: [{ text: 'OK', action: close, primary: true }]
        })
    }

    function info(message, title = 'Info') {
        show({
            icon: 'ℹ️',
            title,
            message,
            buttons: [{ text: 'OK', action: close, primary: true }]
        })
    }

    function confirm(message, { onConfirm, onCancel, title = 'Confirm', confirmText = 'Yes', cancelText = 'No' } = {}) {
        show({
            icon: '⚠️',
            title,
            message,
            buttons: [
                {
                    text: confirmText,
                    action: () => { close(); if (onConfirm) onConfirm(); },
                    primary: true,
                    danger: true
                },
                {
                    text: cancelText,
                    action: () => { close(); if (onCancel) onCancel(); }
                }
            ]
        })
    }

    return {
        isVisible,
        modalConfig,
        show,
        close,
        success,
        error,
        info,
        confirm
    }
})
