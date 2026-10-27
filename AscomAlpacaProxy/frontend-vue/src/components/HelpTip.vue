<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

defineProps({
    text: { type: String, required: true },
    warning: { type: Boolean, default: false }
})

const open = ref(false)
const rootRef = ref(null)

function toggle() {
    open.value = !open.value
}

function close() {
    open.value = false
}

// Click-based (not hover-only) so this works on touch devices too, with a click-outside listener
// to dismiss it - same tradeoff the rest of this app makes elsewhere (no floating-ui/popper
// dependency for something this small).
function onDocumentClick(e) {
    if (open.value && rootRef.value && !rootRef.value.contains(e.target)) {
        close()
    }
}

onMounted(() => document.addEventListener('click', onDocumentClick))
onUnmounted(() => document.removeEventListener('click', onDocumentClick))
</script>

<template>
    <span class="help-tip" ref="rootRef" @keydown.escape="close">
        <button
            type="button"
            class="help-tip-btn"
            :class="{ 'help-tip-btn--warning': warning }"
            @click.stop="toggle"
            :aria-expanded="open"
            :aria-label="text"
            aria-haspopup="true"
        >{{ warning ? '!' : '?' }}</button>
        <div v-if="open" class="help-tip-popover" @click.stop>{{ text }}</div>
    </span>
</template>
