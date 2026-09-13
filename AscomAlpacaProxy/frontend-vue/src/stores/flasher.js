import { defineStore } from 'pinia'
import { ref } from 'vue'

// useFlasherStore holds only the firmware-flasher modal's open/closed state - the same role
// stores/modal.js plays for the generic AppModal. Needed because three unrelated sibling
// components (SystemSettings.vue's Danger Zone, OnboardingWizard.vue, UpdateBanner.vue) all need
// to be able to open the flasher, but none of them are each other's parent - a shared store is
// the natural way to open a modal that's mounted once, elsewhere, in App.vue (see
// FirmwareFlasher.vue).
export const useFlasherStore = defineStore('flasher', () => {
    const isOpen = ref(false)

    function open() {
        isOpen.value = true
    }

    function close() {
        isOpen.value = false
    }

    return { isOpen, open, close }
})
