<script setup>
import { useModalStore } from '../stores/modal'
import { storeToRefs } from 'pinia'

const modalStore = useModalStore()
const { isVisible, modalConfig } = storeToRefs(modalStore)

function handleButtonClick(button) {
    if (button.action) {
        button.action()
    } else {
        modalStore.close()
    }
}
</script>

<template>
    <Teleport to="body">
        <div v-if="isVisible" class="modal-overlay" @click.self="modalStore.close">
            <div class="modal-content glass-panel">
                <div class="modal-header">
                    <span v-if="modalConfig.icon" class="modal-icon">{{ modalConfig.icon }}</span>
                    <h3>{{ modalConfig.title }}</h3>
                </div>
                <p class="modal-message">{{ modalConfig.message }}</p>
                <div class="modal-actions">
                    <button 
                        v-for="(button, index) in modalConfig.buttons" 
                        :key="index"
                        @click="handleButtonClick(button)"
                        :class="[
                            button.primary ? (button.danger ? 'btn-danger' : 'btn-primary') : 'btn-secondary'
                        ]"
                    >
                        {{ button.text }}
                    </button>
                </div>
            </div>
        </div>
    </Teleport>
</template>

<style scoped>
.modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.7);
    display: flex;
    justify-content: center;
    align-items: center;
    z-index: 9999;
    backdrop-filter: blur(4px);
}

.modal-content {
    min-width: 350px;
    max-width: 500px;
    padding: 1.5rem;
    text-align: center;
    animation: modalSlideIn 0.2s ease-out;
}

@keyframes modalSlideIn {
    from {
        opacity: 0;
        transform: translateY(-20px);
    }
    to {
        opacity: 1;
        transform: translateY(0);
    }
}

.modal-header {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    margin-bottom: 1rem;
}

.modal-header h3 {
    margin: 0;
    color: var(--text-primary);
    font-weight: 600;
}

.modal-icon {
    font-size: 1.5rem;
}

.modal-message {
    color: var(--text-secondary);
    margin-bottom: 1.5rem;
    line-height: 1.5;
}

.modal-actions {
    display: flex;
    justify-content: center;
    gap: 0.75rem;
}

.modal-actions button {
    min-width: 100px;
}
</style>
