<template>
  <BaseDialog :show="show" :title="dialogTitle" width="extra-wide" @close="emit('close')">
    <div class="space-y-3">
      <div
        v-if="loadError"
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
      >
        <span>{{ loadError }}</span>
        <button type="button" class="btn btn-secondary btn-sm" @click="loadRecords">
          {{ t('admin.testQuestions.retry') }}
        </button>
      </div>

      <DataTable
        :columns="columns"
        :data="records"
        :loading="loading"
        :sticky-actions-column="false"
      >
        <template #cell-created_at="{ value }">
          <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
        </template>

        <template #cell-account_name="{ row }">
          <div class="min-w-0">
            <div class="truncate font-medium text-gray-900 dark:text-white">
              {{ row.account_name || '-' }}
            </div>
            <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ row.platform || '-' }}</div>
          </div>
        </template>

        <template #cell-model="{ value }">
          <span class="font-mono text-xs text-gray-600 dark:text-gray-300">{{ value || '-' }}</span>
        </template>

        <template #cell-reasoning_effort="{ value }">
          <span class="text-sm text-gray-600 dark:text-gray-300">{{ value || '-' }}</span>
        </template>

        <template #cell-latency_ms="{ value }">
          <span class="text-sm text-gray-600 dark:text-gray-300">{{ formatLatency(value) }}</span>
        </template>

        <template #cell-status="{ row }">
          <span
            :class="['badge', row.status === 'success' ? 'badge-success' : 'badge-danger']"
            :title="row.status === 'failed' ? row.error_message : ''"
          >
            {{ row.status === 'success'
              ? t('admin.testQuestions.history.status.success')
              : t('admin.testQuestions.history.status.failed') }}
          </span>
        </template>

        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1">
            <button
              v-if="row.has_text"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewText')"
              @click="openDetail(row, 'text')"
            >
              <Icon name="document" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewText') }}
            </button>
            <button
              v-if="!row.has_text && row.image_count === 0"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewResult')"
              @click="openDetail(row, 'text')"
            >
              <Icon name="document" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewResult') }}
            </button>
            <button
              v-if="row.image_count > 0"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewImages')"
              @click="openDetail(row, 'images')"
            >
              <Icon name="eye" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewImages') }}
            </button>
            <button
              v-if="row.has_text"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.downloadFile')"
              @click="downloadFromRecord(row)"
            >
              <Icon name="download" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.downloadFile') }}
            </button>
            <button
              v-if="row.status === 'failed' && row.error_message"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewError')"
              @click="openDetail(row, 'error')"
            >
              <Icon name="x" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewError') }}
            </button>
          </div>
        </template>

        <template #empty>
          <EmptyState
            :title="t('admin.testQuestions.history.empty')"
            :description="t('admin.testQuestions.history.emptyHint')"
          />
        </template>
      </DataTable>

      <Pagination
        v-if="total > 0"
        :page="page"
        :total="total"
        :page-size="pageSize"
        @update:page="onPageChange"
        @update:pageSize="onPageSizeChange"
      />
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="detailVisible"
    :title="detailTitle"
    width="wide"
    :z-index="60"
    @close="closeDetail"
  >
    <div class="space-y-3">
      <p v-if="detailLoading" class="text-sm text-gray-500 dark:text-dark-400">…</p>

      <template v-else-if="detailMode === 'text'">
        <!-- 纯音频/视频等无文本记录：隐藏复制与文本/文件提取区，只留公共结果文件区 -->
        <template v-if="detailHasText">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <p v-if="detailRecord?.response_truncated" class="text-xs text-orange-600 dark:text-orange-400">
            {{ t('admin.testQuestions.history.truncated') }}
          </p>
          <div class="ml-auto flex items-center gap-2">
            <button type="button" class="btn btn-secondary btn-sm copy-text" @click="copyDetailText">
              <Icon name="copy" size="sm" class="mr-1" />
              {{ t('common.copy') }}
            </button>
            <button
              v-if="detailArtifact"
              type="button"
              class="btn btn-secondary btn-sm detail-download"
              @click="downloadDetailArtifact"
            >
              <Icon name="download" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.downloadFile') }}
            </button>
          </div>
        </div>
        <img
          v-if="detailArtifact && detailArtifact.isSvg && detailArtifact.objectUrl"
          :src="detailArtifact.objectUrl"
          :alt="detailArtifact.fileName"
          class="max-h-[320px] w-full rounded-lg border border-gray-200 bg-gray-50 object-contain dark:border-dark-500 dark:bg-dark-800"
        />
        <pre
          class="detail-text max-h-[360px] overflow-auto rounded-lg bg-gray-900 p-3 font-mono text-xs text-gray-100"
        >{{ detailRecord?.response_text || '' }}</pre>
        </template>
      </template>

      <template v-else-if="detailMode === 'error'">
        <pre
          class="detail-text max-h-[360px] overflow-auto rounded-lg bg-gray-900 p-3 font-mono text-xs text-gray-100"
        >{{ detailRecord?.error_message || '' }}</pre>
      </template>

      <template v-else-if="detailMode === 'images'">
        <p v-if="detailImageError" class="text-xs text-orange-600 dark:text-orange-400">
          {{ detailImageError }}
        </p>
        <div class="detail-images flex flex-wrap gap-3">
          <button
            v-for="(image, index) in detailImages"
            :key="image.url"
            type="button"
            class="overflow-hidden rounded-lg border"
            :class="index === activeImageIndex ? 'border-primary-500' : 'border-gray-200 dark:border-dark-500'"
            @click="activeImageIndex = index"
          >
            <img :src="image.url" :alt="image.mimeType" class="h-20 w-20 object-cover" />
          </button>
        </div>
        <img
          v-if="detailImages[activeImageIndex]"
          :src="detailImages[activeImageIndex].url"
          :alt="detailImages[activeImageIndex].mimeType"
          class="max-h-[360px] w-full rounded-lg border border-gray-200 bg-gray-50 object-contain dark:border-dark-500 dark:bg-dark-800"
        />
      </template>

      <!-- 结果文件区：与查看模式无关，音频/视频等非图片文件统一在这里提供下载入口 -->
      <div
        v-if="detailMediaFiles.length > 0"
        class="detail-media space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-500"
      >
        <p class="text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t('admin.testQuestions.history.mediaFiles') }}
        </p>
        <div
          v-for="file in detailMediaFiles"
          :key="file.id"
          class="flex flex-wrap items-center justify-between gap-2"
        >
          <span class="text-xs text-gray-600 dark:text-gray-300">
            {{ file.mime_type || file.kind }} · {{ formatBytes(file.size_bytes) }}
          </span>
          <button
            type="button"
            class="btn btn-secondary btn-sm detail-media-download"
            :title="mediaFileName(file)"
            @click="downloadDetailMedia(file)"
          >
            <Icon name="download" size="sm" class="mr-1" />
            {{ t('admin.testQuestions.history.downloadMedia') }}
          </button>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="closeDetail">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import { testRecordsAPI, type TestRecord, type TestRecordFile } from '@/api/admin/testRecords'
import { testCenterAPI } from '@/api/testCenter'
import type { TestQuestion } from '@/api/admin/testQuestions'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { formatBytes, formatDateTime } from '@/utils/format'
import {
  buildTestArtifact,
  downloadTestArtifact,
  revokeTestArtifact,
  type TestArtifact
} from '@/utils/testArtifact'

import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'

type DetailMode = 'text' | 'error' | 'images'

interface DetailImage {
  url: string
  mimeType: string
}

const props = defineProps<{
  show: boolean
  question: TestQuestion | null
  // userMode=true 时走普通用户测试中心接口（管理员不受影响）
  userMode?: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const records = ref<TestRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const loadError = ref('')

const detailVisible = ref(false)
const detailMode = ref<DetailMode>('text')
const detailLoading = ref(false)
const detailRecord = ref<TestRecord | null>(null)
const detailArtifact = ref<TestArtifact | null>(null)
const detailImages = ref<DetailImage[]>([])
const detailImageError = ref('')
const activeImageIndex = ref(0)
// 详情请求的代次号：每次打开/关闭详情都递增。在途请求返回后先比对序号，不匹配就丢弃，
// 避免旧请求把已释放的 objectUrl 覆盖成孤儿资源（与账号测试弹框的 runSequence 同一思路）。
let detailSequence = 0

const questionID = computed(() => props.question?.id ?? 0)
const dialogTitle = computed(() => {
  const title = props.question?.title
  return title
    ? `${t('admin.testQuestions.history.title')} · ${title}`
    : t('admin.testQuestions.history.title')
})
const detailTitle = computed(() => {
  if (detailMode.value === 'text') return t('admin.testQuestions.history.textResult')
  if (detailMode.value === 'error') return t('admin.testQuestions.history.errorDetail')
  return t('admin.testQuestions.history.imagesResult')
})

const detailHasText = computed(() => Boolean(detailRecord.value?.response_text))

// 结果文件区只列非图片文件：图片已在 images 模式给过预览，不再重复列出
const detailMediaFiles = computed<TestRecordFile[]>(() =>
  (detailRecord.value?.files ?? []).filter((file) => file.kind !== 'image')
)

// 与后端落盘命名（test-<record>-<file>.<ext>）保持一致，扩展名优先由 MIME 推导
const mediaExtensions: Record<string, string> = {
  'audio/mpeg': 'mp3',
  'audio/mp4': 'm4a',
  'audio/wav': 'wav',
  'audio/x-wav': 'wav',
  'audio/ogg': 'ogg',
  'audio/webm': 'weba',
  'video/mp4': 'mp4',
  'video/webm': 'webm',
  'video/quicktime': 'mov'
}

function mediaExtension(mimeType: string): string {
  const mime = (mimeType || '').toLowerCase().trim()
  if (mediaExtensions[mime]) return mediaExtensions[mime]
  // 未知类型退回 MIME 子类型（去掉 +xml 之类的后缀），再不行用 bin
  const subtype = (mime.split('/')[1] ?? '').split('+')[0].replace(/[^a-z0-9]/g, '')
  return subtype || 'bin'
}

function mediaFileName(file: TestRecordFile): string {
  return `test-${detailRecord.value?.id ?? 0}-${file.id}.${mediaExtension(file.mime_type)}`
}

const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('admin.testQuestions.history.columns.createdAt') },
  { key: 'account_name', label: t('admin.testQuestions.history.columns.account') },
  { key: 'model', label: t('admin.testQuestions.history.columns.model') },
  { key: 'reasoning_effort', label: t('admin.testQuestions.history.columns.reasoningEffort') },
  { key: 'latency_ms', label: t('admin.testQuestions.history.columns.latency') },
  { key: 'status', label: t('admin.testQuestions.history.columns.status') },
  { key: 'actions', label: t('admin.testQuestions.history.columns.result') }
])

function formatLatency(latencyMs: number): string {
  const value = Number(latencyMs) || 0
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(1)} s`
}

async function loadRecords() {
  if (!questionID.value) return
  loading.value = true
  loadError.value = ''
  try {
    const result = props.userMode
      ? await testCenterAPI.listRecords(questionID.value, page.value, pageSize.value)
      : await testRecordsAPI.listByQuestion(questionID.value, page.value, pageSize.value)
    records.value = result.items ?? []
    total.value = result.total ?? 0
  } catch (error) {
    console.error('[测试历史] 加载失败', error)
    records.value = []
    total.value = 0
    loadError.value = t('admin.testQuestions.history.loadFailed')
    appStore.showError(loadError.value)
  } finally {
    loading.value = false
  }
}

function onPageChange(next: number) {
  page.value = next
  void loadRecords()
}

function onPageSizeChange(next: number) {
  pageSize.value = next
  page.value = 1
  void loadRecords()
}

// 详情是否已过期：序号被 closeDetail/新的 openDetail 递增后，旧请求就算完成也要丢弃。
function isStaleDetail(sequence: number): boolean {
  return sequence !== detailSequence
}

function releaseImageUrls(images: DetailImage[]) {
  for (const image of images) {
    if (typeof URL.revokeObjectURL === 'function') URL.revokeObjectURL(image.url)
  }
}

function releaseDetailResources() {
  revokeTestArtifact(detailArtifact.value)
  detailArtifact.value = null
  releaseImageUrls(detailImages.value)
  detailImages.value = []
}

function closeDetail() {
  // 递增序号，让所有在途的详情请求失效
  detailSequence += 1
  detailVisible.value = false
  detailRecord.value = null
  detailImageError.value = ''
  releaseDetailResources()
}

async function openDetail(row: TestRecord, mode: DetailMode) {
  closeDetail()
  // closeDetail 已经把序号递增过一次，这里再取一个新序号代表本次请求
  const sequence = ++detailSequence
  detailMode.value = mode
  detailVisible.value = true
  detailLoading.value = true
  try {
    const record = props.userMode
      ? await testCenterAPI.getRecord(row.id)
      : await testRecordsAPI.get(row.id)
    // 请求返回时详情可能已关闭或已切到别的记录：直接丢弃，避免覆盖新详情并泄漏本次 URL
    if (isStaleDetail(sequence)) {
      console.log('[测试历史] 详情请求已过期，丢弃返回结果', row.id)
      return
    }
    detailRecord.value = record
    if (mode === 'text') {
      detailArtifact.value = buildTestArtifact(
        record.response_text ?? '',
        record.question_title || props.question?.title || 'test-result'
      )
    }
    if (mode === 'images') {
      await loadDetailImages(record, sequence)
    }
  } catch (error) {
    console.error('[测试历史] 加载详情失败', error)
    if (isStaleDetail(sequence)) return
    appStore.showError(t('admin.testQuestions.history.detailFailed'))
    closeDetail()
  } finally {
    // 过期请求不能收尾 loading，否则会提前结束新请求的加载态
    if (!isStaleDetail(sequence)) detailLoading.value = false
  }
}

async function loadDetailImages(record: TestRecord, sequence: number) {
  const files = (record.files ?? []).filter((file) => file.kind === 'image')
  if (files.length === 0) return
  const images: DetailImage[] = []
  for (const file of files) {
    let blob: Blob
    try {
      blob = props.userMode
        ? await testCenterAPI.fetchRecordFile(record.id, file.id)
        : await testRecordsAPI.fetchFile(record.id, file.id)
    } catch (error) {
      console.error('[测试历史] 加载结果图片失败', error)
      // 过期时把本次已创建的图片 URL 一并释放，再放弃
      if (isStaleDetail(sequence)) {
        releaseImageUrls(images)
        return
      }
      detailImageError.value = t('admin.testQuestions.history.imageLoadFailed')
      continue
    }
    // 每张图片返回后都确认序号：详情已关闭/切换时不再创建新 URL，并释放本次已建的
    if (isStaleDetail(sequence)) {
      console.log('[测试历史] 图片加载已过期，释放本次已创建的 URL')
      releaseImageUrls(images)
      return
    }
    images.push({
      url: URL.createObjectURL(blob),
      mimeType: file.mime_type || 'image/*'
    })
  }
  detailImages.value = images
  activeImageIndex.value = 0
}

async function downloadFromRecord(row: TestRecord) {
  try {
    const record = detailRecord.value?.id === row.id
      ? detailRecord.value
      : props.userMode
        ? await testCenterAPI.getRecord(row.id)
        : await testRecordsAPI.get(row.id)
    const artifact = buildTestArtifact(
      record.response_text ?? '',
      record.question_title || props.question?.title || 'test-result'
    )
    if (!artifact) {
      appStore.showError(t('admin.testQuestions.history.noArtifact'))
      return
    }
    downloadTestArtifact(artifact)
    revokeTestArtifact(artifact)
  } catch (error) {
    console.error('[测试历史] 下载失败', error)
    appStore.showError(t('admin.testQuestions.history.downloadFailed'))
  }
}

async function downloadDetailMedia(file: TestRecordFile) {
  const record = detailRecord.value
  if (!record) return
  try {
    const blob = props.userMode
      ? await testCenterAPI.fetchRecordFile(record.id, file.id)
      : await testRecordsAPI.fetchFile(record.id, file.id)
    if (typeof URL.createObjectURL !== 'function') return
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = mediaFileName(file)
    link.rel = 'noopener'
    link.click()
    // 下载已触发，延后一拍释放 objectURL（与 utils/testArtifact 一致），避免下载还没开始就被回收
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  } catch (error) {
    console.error('[测试历史] 下载结果文件失败', file.id, error)
    appStore.showError(t('admin.testQuestions.history.mediaDownloadFailed'))
  }
}

function copyDetailText() {
  copyToClipboard(detailRecord.value?.response_text ?? '')
}

function downloadDetailArtifact() {
  if (detailArtifact.value) downloadTestArtifact(detailArtifact.value)
}

// 题库页常驻挂载本弹框，离开页面时详情可能还开着，需在这里释放 objectUrl
onBeforeUnmount(() => {
  console.log('[测试历史] 组件卸载，释放详情资源')
  detailSequence += 1
  releaseDetailResources()
})

watch(
  () => [props.show, props.question?.id] as const,
  ([visible]) => {
    if (!visible) {
      closeDetail()
      return
    }
    page.value = 1
    void loadRecords()
  },
  { immediate: true }
)
</script>
