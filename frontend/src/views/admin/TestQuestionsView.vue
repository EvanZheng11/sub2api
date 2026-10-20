<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="relative w-full sm:w-64">
            <Icon
              name="search"
              size="md"
              class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
            />
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.testQuestions.searchPlaceholder')"
              class="input pl-10"
            />
          </div>

          <div class="w-full sm:w-40">
            <Select v-model="typeFilter" :options="typeFilterOptions" />
          </div>

          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="loadQuestions"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.testQuestions.createQuestion') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div
            v-if="loadError"
            class="mx-4 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
          >
            <span>{{ loadError }}</span>
            <button type="button" class="btn btn-secondary btn-sm" @click="loadQuestions">
              {{ t('admin.testQuestions.retry') }}
            </button>
          </div>

          <DataTable :columns="columns" :data="filteredQuestions" :loading="loading">
            <template #cell-title="{ row }">
              <div class="min-w-0">
                <div class="truncate font-medium text-gray-900 dark:text-white">{{ row.title }}</div>
                <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">#{{ row.id }}</div>
              </div>
            </template>

            <template #cell-type="{ value }">
              <span :class="['badge', typeBadgeClass(value)]">{{ typeLabel(value) }}</span>
            </template>

            <template #cell-prompt="{ value }">
              <span
                class="block max-w-md truncate text-sm text-gray-600 dark:text-gray-300"
                :title="value"
              >
                {{ value }}
              </span>
            </template>

            <template #cell-updated_at="{ value }">
              <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
            </template>

            <template #cell-actions="{ row }">
              <div class="flex items-center space-x-1">
                <button
                  type="button"
                  class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                  :title="t('admin.testQuestions.test.action')"
                  @click="openTestDialog(row)"
                >
                  <Icon name="play" size="sm" />
                </button>
                <button
                  type="button"
                  class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                  :title="t('admin.testQuestions.history.action')"
                  @click="openHistoryDialog(row)"
                >
                  <Icon name="clock" size="sm" />
                </button>
                <button
                  v-if="isAdmin"
                  type="button"
                  class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                  :title="t('common.edit')"
                  @click="openEditDialog(row)"
                >
                  <Icon name="edit" size="sm" />
                </button>
                <button
                  v-if="isAdmin"
                  type="button"
                  class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                  :title="t('common.delete')"
                  @click="handleDelete(row)"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </template>

            <template #empty>
              <EmptyState
                v-if="questions.length === 0"
                :title="t('admin.testQuestions.empty')"
                :description="t('admin.testQuestions.emptyHint')"
                :action-text="t('admin.testQuestions.createQuestion')"
                @action="openCreateDialog"
              />
              <EmptyState
                v-else
                :title="t('admin.testQuestions.noMatch')"
                :description="t('admin.testQuestions.noMatchHint')"
              />
            </template>
          </DataTable>
        </div>
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showEditDialog"
      :title="isEditing ? t('admin.testQuestions.editQuestion') : t('admin.testQuestions.createQuestion')"
      width="wide"
      @close="closeEditDialog"
    >
      <form id="test-question-form" class="space-y-4" @submit.prevent="handleSave">
        <div>
          <label class="input-label" for="test-question-title">
            {{ t('admin.testQuestions.form.title') }}
          </label>
          <input
            id="test-question-title"
            v-model="form.title"
            type="text"
            class="input"
            maxlength="200"
            :placeholder="t('admin.testQuestions.form.titlePlaceholder')"
            required
          />
        </div>

        <div>
          <label class="input-label" for="test-question-type">
            {{ t('admin.testQuestions.form.type') }}
          </label>
          <Select id="test-question-type" v-model="form.type" :options="questionTypeOptions" />
          <p class="input-hint">{{ t(`admin.testQuestions.typeHints.${form.type}`) }}</p>
        </div>

        <div>
          <label class="input-label" for="test-question-prompt">
            {{ t('admin.testQuestions.form.prompt') }}
          </label>
          <textarea
            id="test-question-prompt"
            v-model="form.prompt"
            rows="8"
            class="input font-mono"
            :placeholder="t('admin.testQuestions.form.promptPlaceholder')"
            required
          ></textarea>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeEditDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="test-question-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.testQuestions.deleteQuestion')"
      :message="t('admin.testQuestions.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />

    <TestRecordHistoryModal
      :show="showHistoryDialog"
      :question="historyQuestion"
      :user-mode="!isAdmin"
      @close="showHistoryDialog = false"
    />

    <TestQuestionTestModal
      :show="showTestDialog"
      :question="testingQuestion"
      @close="showTestDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import type {
  TestQuestion,
  TestQuestionPayload,
  TestQuestionType
} from '@/api/admin/testQuestions'
import { adminAPI } from '@/api/admin'
import { testCenterAPI } from '@/api/testCenter'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/format'

import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import TestRecordHistoryModal from '@/components/admin/testquestion/TestRecordHistoryModal.vue'
import TestQuestionTestModal from '@/components/admin/testquestion/TestQuestionTestModal.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
// 普通用户可见题库，但只能新建、发起测试与查看结果，不能修改/删除题目。
const isAdmin = computed(() => authStore.isAdmin)

const questions = ref<TestQuestion[]>([])
const loading = ref(false)
const loadError = ref('')
const searchQuery = ref('')
const typeFilter = ref('')

const questionTypeOptions = computed(() => [
  { value: 'text', label: t('admin.testQuestions.typeLabels.text') },
  { value: 'image', label: t('admin.testQuestions.typeLabels.image') },
  { value: 'file', label: t('admin.testQuestions.typeLabels.file') }
])

const typeFilterOptions = computed(() => [
  { value: '', label: t('admin.testQuestions.typeFilterAll') },
  ...questionTypeOptions.value
])

const columns = computed<Column[]>(() => [
  { key: 'title', label: t('admin.testQuestions.columns.title') },
  { key: 'type', label: t('admin.testQuestions.columns.type') },
  { key: 'prompt', label: t('admin.testQuestions.columns.prompt') },
  { key: 'updated_at', label: t('admin.testQuestions.columns.updatedAt') },
  { key: 'actions', label: t('admin.testQuestions.columns.actions') }
])

const filteredQuestions = computed(() => {
  const keyword = searchQuery.value.trim().toLowerCase()
  return questions.value.filter((question) => {
    if (typeFilter.value && question.type !== typeFilter.value) return false
    if (!keyword) return true
    return (
      question.title.toLowerCase().includes(keyword) ||
      question.prompt.toLowerCase().includes(keyword)
    )
  })
})

function typeLabel(type: TestQuestionType): string {
  return t(`admin.testQuestions.typeLabels.${type}`)
}

function typeBadgeClass(type: TestQuestionType): string {
  if (type === 'image') return 'badge-primary'
  if (type === 'file') return 'badge-warning'
  return 'badge-gray'
}

async function loadQuestions() {
  loading.value = true
  loadError.value = ''
  try {
    questions.value = isAdmin.value
      ? await adminAPI.testQuestions.list()
      : await testCenterAPI.listQuestions()
    console.info('[测试题库] 已加载', questions.value.length, '道题目')
  } catch (error) {
    console.error('[测试题库] 加载失败', error)
    questions.value = []
    loadError.value = t('admin.testQuestions.loadFailed')
    appStore.showError(loadError.value)
  } finally {
    loading.value = false
  }
}

const showEditDialog = ref(false)
const editingQuestion = ref<TestQuestion | null>(null)
const saving = ref(false)
const isEditing = computed(() => editingQuestion.value !== null)
const form = reactive<TestQuestionPayload>({
  title: '',
  prompt: '',
  type: 'text'
})

function openCreateDialog() {
  editingQuestion.value = null
  form.title = ''
  form.prompt = ''
  form.type = 'text'
  showEditDialog.value = true
}

function openEditDialog(question: TestQuestion) {
  editingQuestion.value = question
  form.title = question.title
  form.prompt = question.prompt
  form.type = question.type
  showEditDialog.value = true
}

function closeEditDialog() {
  showEditDialog.value = false
  editingQuestion.value = null
}

async function handleSave() {
  const title = form.title.trim()
  const prompt = form.prompt.trim()
  if (!title) {
    appStore.showError(t('admin.testQuestions.titleRequired'))
    return
  }
  if (!prompt) {
    appStore.showError(t('admin.testQuestions.promptRequired'))
    return
  }

  const payload: TestQuestionPayload = { title, prompt, type: form.type }
  saving.value = true
  try {
    if (editingQuestion.value) {
      await adminAPI.testQuestions.update(editingQuestion.value.id, payload)
      appStore.showSuccess(t('admin.testQuestions.updated'))
    } else if (isAdmin.value) {
      await adminAPI.testQuestions.create(payload)
      appStore.showSuccess(t('admin.testQuestions.created'))
    } else {
      await testCenterAPI.createQuestion(payload)
      appStore.showSuccess(t('admin.testQuestions.created'))
    }
    closeEditDialog()
    await loadQuestions()
  } catch (error) {
    console.error('[测试题库] 保存失败', error)
    appStore.showError(t('admin.testQuestions.saveFailed'))
  } finally {
    saving.value = false
  }
}

const showDeleteDialog = ref(false)
const deletingQuestion = ref<TestQuestion | null>(null)

function handleDelete(question: TestQuestion) {
  deletingQuestion.value = question
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deletingQuestion.value) return
  try {
    await adminAPI.testQuestions.remove(deletingQuestion.value.id)
    appStore.showSuccess(t('admin.testQuestions.deleted'))
    showDeleteDialog.value = false
    deletingQuestion.value = null
    await loadQuestions()
  } catch (error) {
    console.error('[测试题库] 删除失败', error)
    appStore.showError(t('admin.testQuestions.deleteFailed'))
  }
}

const showHistoryDialog = ref(false)
const historyQuestion = ref<TestQuestion | null>(null)

function openHistoryDialog(question: TestQuestion) {
  historyQuestion.value = question
  showHistoryDialog.value = true
}

const showTestDialog = ref(false)
const testingQuestion = ref<TestQuestion | null>(null)

function openTestDialog(question: TestQuestion) {
  testingQuestion.value = question
  showTestDialog.value = true
}

onMounted(loadQuestions)
</script>
