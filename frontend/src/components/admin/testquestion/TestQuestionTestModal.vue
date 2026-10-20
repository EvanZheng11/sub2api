<template>
  <BaseDialog
    :show="show"
    :title="t('admin.testQuestions.test.title')"
    width="normal"
    @close="handleClose"
  >
    <div class="space-y-4">
      <!-- 测试题目信息 -->
      <div
        v-if="question"
        class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-500 dark:bg-dark-700"
      >
        <div class="truncate font-semibold text-gray-900 dark:text-gray-100">
          {{ question.title }}
        </div>
        <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t(`admin.testQuestions.typeLabels.${question.type}`) }} · #{{ question.id }}
        </div>
      </div>

      <!-- 账号选择 -->
      <div class="space-y-1.5">
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.testQuestions.test.account') }}
        </label>
        <Select
          v-model="selectedAccountId"
          :options="accountOptions"
          :disabled="loadingAccounts || status === 'connecting'"
          :placeholder="t('admin.testQuestions.test.accountPlaceholder')"
        />
        <p v-if="loadingAccounts" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}...
        </p>
        <p v-else-if="accountLoadError" class="text-xs text-orange-600 dark:text-orange-400">
          {{ accountLoadError }}
        </p>
        <p v-else-if="accounts.length === 0" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.testQuestions.test.noAccount') }}
        </p>
      </div>

      <!-- 模型选择 -->
      <div class="space-y-1.5">
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.selectTestModel') }}
        </label>
        <Select
          v-model="selectedModelId"
          :options="modelOptions"
          :disabled="loadingModels || status === 'connecting'"
          value-key="id"
          label-key="display_name"
          :placeholder="t('admin.accounts.selectTestModel')"
        />
        <p v-if="loadingModels" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}...
        </p>
        <p v-else-if="modelLoadError" class="text-xs text-orange-600 dark:text-orange-400">
          {{ modelLoadError }}
        </p>
      </div>

      <!-- 思考强度 -->
      <div v-if="showReasoningEffort" class="space-y-1.5">
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.reasoningEffortLabel') }}
        </label>
        <Select
          v-model="selectedReasoningEffort"
          :options="reasoningEffortOptions"
          :disabled="status === 'connecting'"
        />
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.reasoningEffortHint') }}
        </p>
      </div>

      <!-- 输出区 -->
      <div class="relative">
        <div
          ref="terminalRef"
          class="max-h-[240px] min-h-[120px] overflow-y-auto rounded-xl border border-gray-700 bg-gray-900 p-4 font-mono text-sm dark:border-gray-800 dark:bg-black"
        >
          <div v-if="status === 'idle'" class="flex items-center gap-2 text-gray-500">
            <Icon name="play" size="sm" :stroke-width="2" />
            <span>{{ t('admin.accounts.readyToTest') }}</span>
          </div>
          <div v-else-if="status === 'connecting'" class="flex items-center gap-2 text-yellow-400">
            <Icon name="refresh" size="sm" class="animate-spin" :stroke-width="2" />
            <span>{{ t('admin.accounts.connectingToApi') }}</span>
          </div>

          <div v-for="(line, index) in outputLines" :key="index" :class="line.class">
            {{ line.text }}
          </div>

          <div v-if="streamingContent" class="text-green-400">
            {{ streamingContent }}<span class="animate-pulse">_</span>
          </div>

          <div
            v-if="status === 'success'"
            class="mt-3 flex items-center gap-2 border-t border-gray-700 pt-3 text-green-400"
          >
            <Icon name="check" size="sm" :stroke-width="2" />
            <span>{{ t('admin.accounts.testCompleted') }}</span>
          </div>
          <div
            v-else-if="status === 'error'"
            class="mt-3 flex items-center gap-2 border-t border-gray-700 pt-3 text-red-400"
          >
            <Icon name="x" size="sm" :stroke-width="2" />
            <span>{{ errorMessage }}</span>
          </div>
        </div>
      </div>

      <!-- 图片结果 -->
      <div v-if="generatedImages.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-gray-600 dark:text-gray-300">
          {{ t('admin.accounts.imagePreview') }}
        </div>
        <div class="flex flex-wrap justify-center gap-3">
          <img
            v-for="(image, index) in generatedImages"
            :key="`${image.url}-${index}`"
            :src="image.url"
            :alt="t('admin.accounts.imagePreviewAlt', { index: index + 1 })"
            class="max-h-[360px] rounded-xl border border-gray-200 object-contain dark:border-dark-500"
          />
        </div>
      </div>

      <!-- 音频 / 视频结果 -->
      <div v-if="generatedAudios.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-gray-600 dark:text-gray-300">
          {{ t('admin.accounts.audioPreview') }}
        </div>
        <audio
          v-for="(audio, index) in generatedAudios"
          :key="`audio-${index}`"
          :src="audio.url"
          controls
          class="w-full"
          :type="audio.mimeType"
        />
      </div>

      <div v-if="generatedVideos.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-gray-600 dark:text-gray-300">
          {{ t('admin.accounts.videoPreview') }}
        </div>
        <video
          v-for="(video, index) in generatedVideos"
          :key="`video-${index}`"
          :src="video.url"
          controls
          class="max-h-[360px] w-full rounded-xl border border-gray-200 dark:border-dark-500"
          :type="video.mimeType"
        />
      </div>

      <!-- 文件题结果 -->
      <div
        v-if="artifact"
        class="space-y-2 rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-500 dark:bg-dark-700"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="min-w-0">
            <div class="text-xs font-medium text-gray-600 dark:text-gray-300">
              {{ t('admin.accounts.fileArtifact.title') }}
            </div>
            <div class="truncate font-mono text-xs text-gray-500 dark:text-gray-400">
              {{ artifact.fileName }} · {{ artifact.mimeType }}
            </div>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="downloadArtifact">
            <Icon name="download" size="sm" class="mr-1" />
            {{ t('admin.accounts.fileArtifact.download') }}
          </button>
        </div>
        <img
          v-if="artifact.isSvg && artifact.objectUrl"
          :src="artifact.objectUrl"
          :alt="artifact.fileName"
          class="max-h-[360px] w-full rounded-lg border border-gray-200 bg-gray-50 object-contain dark:border-dark-500 dark:bg-dark-800"
        />
        <pre
          v-if="!artifact.isSvg"
          class="max-h-[300px] overflow-auto rounded-lg bg-gray-900 p-3 font-mono text-xs text-gray-100"
        >{{ artifact.content }}</pre>
        <details v-else class="rounded-lg border border-gray-200 dark:border-dark-500">
          <summary class="cursor-pointer select-none px-3 py-2 text-xs text-gray-600 dark:text-gray-300">
            {{ t('admin.accounts.fileArtifact.showSource') }}
          </summary>
          <pre
            class="max-h-[300px] overflow-auto border-t border-gray-200 bg-gray-900 p-3 font-mono text-xs text-gray-100 dark:border-dark-500"
          >{{ artifact.content }}</pre>
        </details>
      </div>
      <p v-else-if="artifactError" class="text-xs text-orange-600 dark:text-orange-400">
        {{ artifactError }}
      </p>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button
          v-if="status === 'connecting'"
          type="button"
          class="btn btn-secondary"
          @click="cancelTest"
        >
          {{ t('admin.accounts.cancelTest') }}
        </button>
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.close') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="!canStart" @click="startTest">
          <Icon
            v-if="status === 'connecting'"
            name="refresh"
            size="sm"
            class="mr-1 animate-spin"
            :stroke-width="2"
          />
          <Icon v-else name="play" size="sm" class="mr-1" :stroke-width="2" />
          {{
            status === 'connecting'
              ? t('admin.accounts.testing')
              : status === 'idle'
                ? t('admin.accounts.startTest')
                : t('admin.accounts.retry')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { Icon } from '@/components/icons'
import { testCenterAPI, type TestCenterAccount } from '@/api/testCenter'
import type { TestQuestion } from '@/api/admin/testQuestions'
import { accountTestReasoningEffortOptions } from '@/components/admin/account/accountTestReasoningEffort'
import {
  buildTestArtifact,
  downloadTestArtifact,
  revokeTestArtifact,
  type TestArtifact
} from '@/utils/testArtifact'
import type { Account, ClaudeModel } from '@/types'

const { t } = useI18n()

interface OutputLine {
  text: string
  class: string
}

interface PreviewMedia {
  url: string
  mimeType?: string
}

const props = defineProps<{
  show: boolean
  question: TestQuestion | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const terminalRef = ref<HTMLElement | null>(null)
const status = ref<'idle' | 'connecting' | 'success' | 'error'>('idle')
const outputLines = ref<OutputLine[]>([])
const streamingContent = ref('')
const responseText = ref('')
const errorMessage = ref('')

const accounts = ref<TestCenterAccount[]>([])
const loadingAccounts = ref(false)
const accountLoadError = ref('')
const selectedAccountId = ref('')

const models = ref<ClaudeModel[]>([])
const loadingModels = ref(false)
const modelLoadError = ref('')
const selectedModelId = ref('')

const selectedReasoningEffort = ref('')

const generatedImages = ref<PreviewMedia[]>([])
const generatedAudios = ref<PreviewMedia[]>([])
const generatedVideos = ref<PreviewMedia[]>([])
const artifact = ref<TestArtifact | null>(null)
const artifactError = ref('')

let abortController: AbortController | null = null
let runSequence = 0
let loadSequence = 0

const accountOptions = computed(() =>
  accounts.value.map((account) => ({
    value: String(account.id),
    label: `${account.name} · ${account.platform}`
  }))
)

const modelOptions = computed(() => models.value)
const selectedAccount = computed(
  () => accounts.value.find((account) => String(account.id) === selectedAccountId.value) ?? null
)

// 思考强度矩阵需要账号的平台/类型/model_mapping；用户侧账号列表只返回脱敏字段，
// 这里用最小对象驱动同一套矩阵，mapping 缺失时按原始模型名判断。
const effortAccount = computed<Account | null>(() => {
  const account = selectedAccount.value
  if (!account) return null
  return { platform: account.platform, type: account.type, credentials: {} } as unknown as Account
})

const reasoningEffortOptions = computed(() => {
  const options = accountTestReasoningEffortOptions(effortAccount.value, selectedModelId.value, 'default')
  if (options.length === 0) return []
  return [
    { value: '', label: t('admin.accounts.reasoningEffortDefault') },
    ...options.map((value) => ({ value, label: value }))
  ]
})
const showReasoningEffort = computed(() => reasoningEffortOptions.value.length > 0)

const canStart = computed(() => {
  if (status.value === 'connecting') return false
  if (!selectedAccount.value) return false
  return Boolean(selectedModelId.value)
})

const addLine = (text: string, className = 'text-gray-300') => {
  outputLines.value.push({ text, class: className })
  void scrollToBottom()
}

const scrollToBottom = async () => {
  await nextTick()
  if (terminalRef.value) terminalRef.value.scrollTop = terminalRef.value.scrollHeight
}

const clearArtifact = () => {
  revokeTestArtifact(artifact.value)
  artifact.value = null
  artifactError.value = ''
}

const resetState = () => {
  status.value = 'idle'
  outputLines.value = []
  streamingContent.value = ''
  responseText.value = ''
  errorMessage.value = ''
  generatedImages.value = []
  generatedAudios.value = []
  generatedVideos.value = []
  clearArtifact()
}

const abortStream = () => {
  if (abortController) {
    abortController.abort()
    abortController = null
  }
}

const loadAccounts = async (openId: number) => {
  loadingAccounts.value = true
  accountLoadError.value = ''
  try {
    const loaded = await testCenterAPI.listAccounts()
    if (openId !== loadSequence) return
    accounts.value = loaded
    if (loaded.length > 0) {
      selectedAccountId.value = String(loaded[0].id)
    }
  } catch (error) {
    if (openId !== loadSequence) return
    console.error('[题库测试] 加载可用账号失败', error)
    accounts.value = []
    accountLoadError.value = t('admin.testQuestions.test.accountLoadFailed')
  } finally {
    if (openId === loadSequence) loadingAccounts.value = false
  }
}

const loadModels = async (openId: number, accountId: string) => {
  if (!accountId) {
    models.value = []
    selectedModelId.value = ''
    return
  }
  loadingModels.value = true
  modelLoadError.value = ''
  models.value = []
  selectedModelId.value = ''
  try {
    const loaded = await testCenterAPI.listAccountModels(Number(accountId))
    if (openId !== loadSequence) return
    models.value = loaded
    if (loaded.length > 0) selectedModelId.value = loaded[0].id
    console.info('[题库测试] 已加载模型', loaded.length, '个')
  } catch (error) {
    if (openId !== loadSequence) return
    console.error('[题库测试] 加载模型失败', error)
    models.value = []
    selectedModelId.value = ''
    modelLoadError.value = t('admin.testQuestions.test.modelLoadFailed')
  } finally {
    if (openId === loadSequence) loadingModels.value = false
  }
}

watch(selectedAccountId, () => {
  if (status.value === 'connecting') return
  resetState()
  void loadModels(loadSequence, selectedAccountId.value)
})

watch(reasoningEffortOptions, (options) => {
  if (!selectedReasoningEffort.value) return
  if (options.some((option) => option.value === selectedReasoningEffort.value)) return
  selectedReasoningEffort.value = ''
})

watch(
  () => props.show,
  async (visible) => {
    const openId = ++loadSequence
    if (!visible) {
      runSequence += 1
      abortStream()
      clearArtifact()
      return
    }
    resetState()
    selectedReasoningEffort.value = ''
    selectedAccountId.value = ''
    models.value = []
    selectedModelId.value = ''
    await loadAccounts(openId)
    if (openId !== loadSequence || !props.show) return
    await loadModels(openId, selectedAccountId.value)
  }
)

const handleEvent = (event: {
  type: string
  text?: string
  model?: string
  success?: boolean
  error?: string
  image_url?: string
  audio_url?: string
  video_url?: string
  mime_type?: string
}) => {
  switch (event.type) {
    case 'test_start':
      addLine(t('admin.accounts.connectedToApi'), 'text-green-400')
      if (event.model) addLine(t('admin.accounts.usingModel', { model: event.model }), 'text-cyan-400')
      addLine(t('admin.accounts.response'), 'text-yellow-400')
      break
    case 'content':
      if (event.text) {
        streamingContent.value += event.text
        responseText.value += event.text
        void scrollToBottom()
      }
      break
    case 'image':
      if (event.image_url) {
        generatedImages.value.push({ url: event.image_url, mimeType: event.mime_type })
        addLine(t('admin.accounts.imageReceived', { count: generatedImages.value.length }), 'text-purple-300')
      }
      break
    case 'audio':
      if (event.audio_url) {
        generatedAudios.value.push({ url: event.audio_url, mimeType: event.mime_type })
        addLine(t('admin.accounts.audioReceived', { count: generatedAudios.value.length }), 'text-purple-300')
      }
      break
    case 'video':
      if (event.video_url) {
        generatedVideos.value.push({ url: event.video_url, mimeType: event.mime_type })
        addLine(t('admin.accounts.videoReceived', { count: generatedVideos.value.length }), 'text-purple-300')
      }
      break
    case 'status':
      if (event.text) addLine(event.text, 'text-cyan-300')
      break
    case 'test_complete':
      if (streamingContent.value) {
        addLine(streamingContent.value, 'text-green-300')
        streamingContent.value = ''
      }
      if (event.success) {
        status.value = 'success'
        if (props.question?.type === 'file') {
          const built = buildTestArtifact(responseText.value, props.question?.title || 'test-result')
          if (built) {
            artifact.value = built
            console.info('[题库测试] 已从响应中提取文件', built.fileName)
          } else {
            artifactError.value = t('admin.accounts.fileArtifact.none')
          }
        }
      } else {
        status.value = 'error'
        errorMessage.value = event.error || t('admin.accounts.testFailed')
      }
      break
    case 'error':
      status.value = 'error'
      errorMessage.value = event.error || t('common.unknownError')
      if (streamingContent.value) {
        addLine(streamingContent.value, 'text-green-300')
        streamingContent.value = ''
      }
      break
  }
}

const startTest = async () => {
  const account = selectedAccount.value
  const question = props.question
  if (!account || !question || !canStart.value) return

  const runId = ++runSequence
  resetState()
  status.value = 'connecting'
  addLine(t('admin.testQuestions.test.starting', { account: account.name }), 'text-blue-400')
  addLine(t('admin.accounts.testAccountTypeLabel', { type: account.type }), 'text-gray-400')
  addLine('', 'text-gray-300')
  abortStream()

  const controller = new AbortController()
  abortController = controller

  // 发送前二次校验：模型/账号切换后旧档位可能已失效，避免上游 400。
  if (
    selectedReasoningEffort.value &&
    !reasoningEffortOptions.value.some((option) => option.value === selectedReasoningEffort.value)
  ) {
    selectedReasoningEffort.value = ''
  }

  const requestBody: {
    model_id: string
    prompt: string
    test_question_id: number
    mode?: string
    reasoning_effort?: string
  } = {
    model_id: selectedModelId.value,
    prompt: question.prompt,
    test_question_id: question.id
  }
  if (selectedReasoningEffort.value) {
    requestBody.reasoning_effort = selectedReasoningEffort.value
  }
  // Grok 的 mode=default 由后端按模型族推断文本/图片/视频，避免前端重复判断。
  if (account.platform === 'grok') {
    requestBody.mode = 'default'
  }

  try {
    const response = await fetch(testCenterAPI.buildTestUrl(account.id), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(requestBody),
      signal: controller.signal
    })

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`)
    }

    const reader = response.body?.getReader()
    if (!reader) throw new Error(t('admin.accounts.grok.noResponseBody'))

    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      if (runId !== runSequence) return

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const jsonStr = line.slice(6).trim()
        if (!jsonStr) continue
        try {
          const event = JSON.parse(jsonStr)
          if (runId !== runSequence) return
          handleEvent(event)
        } catch (error) {
          console.error('[题库测试] 解析 SSE 事件失败', error)
        }
      }
    }

    if (runId !== runSequence) return
    if (status.value === 'connecting') {
      status.value = 'error'
      errorMessage.value = t('admin.accounts.streamEnded')
      addLine(t('admin.accounts.errorPrefix', { message: errorMessage.value }), 'text-red-400')
    }
  } catch (error: unknown) {
    if (runId !== runSequence) return
    if (error instanceof DOMException && error.name === 'AbortError') {
      status.value = 'idle'
      return
    }
    status.value = 'error'
    const message = error instanceof Error ? error.message : t('common.unknownError')
    errorMessage.value = message
    addLine(t('admin.accounts.errorPrefix', { message }), 'text-red-400')
  }
}

const cancelTest = () => {
  runSequence += 1
  abortStream()
  resetState()
  addLine(t('admin.accounts.testCancelled'), 'text-yellow-400')
}

const downloadArtifact = () => {
  if (artifact.value) downloadTestArtifact(artifact.value)
}

const handleClose = () => {
  runSequence += 1
  abortStream()
  clearArtifact()
  emit('close')
}

onBeforeUnmount(() => {
  runSequence += 1
  abortStream()
  clearArtifact()
})
</script>
