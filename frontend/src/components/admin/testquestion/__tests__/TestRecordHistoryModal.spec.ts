import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import TestRecordHistoryModal from '../TestRecordHistoryModal.vue'
import type { TestRecord } from '@/api/admin/testRecords'
import type { TestQuestion } from '@/api/admin/testQuestions'

const { listByQuestion, get, fetchFile, showError, copyToClipboard } = vi.hoisted(() => ({
  listByQuestion: vi.fn(),
  get: vi.fn(),
  fetchFile: vi.fn(),
  showError: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin/testRecords', () => ({
  testRecordsAPI: { listByQuestion, get, fetchFile }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    return () =>
      h('div', [
        ...((props.data as Array<Record<string, unknown>>).length === 0
          ? [slots.empty?.()]
          : (props.data as Array<Record<string, unknown>>).map((row) =>
              h('div', { class: 'record-row' }, [
                h('span', { class: 'record-account' }, String(row.account_name)),
                slots['cell-actions']?.({ row })
              ])
            ))
      ])
  }
})

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  setup(props, { slots }) {
    return () =>
      props.show
        ? h('div', { class: 'dialog-stub' }, [
            slots.default?.(),
            slots.footer?.()
          ])
        : null
  }
})

const PaginationStub = defineComponent({
  props: { page: Number, total: Number, pageSize: Number },
  emits: ['update:page'],
  setup(props, { emit }) {
    return () =>
      h('button', { class: 'next-page', onClick: () => emit('update:page', (props.page ?? 1) + 1) }, 'next')
  }
})

function question(): TestQuestion {
  return {
    id: 9,
    title: '鹈鹕骑自行车',
    prompt: '画一只骑自行车的鹈鹕',
    type: 'file',
    created_at: '2026-10-20T00:00:00Z',
    updated_at: '2026-10-20T00:00:00Z'
  }
}

function record(overrides: Partial<TestRecord> = {}): TestRecord {
  return {
    id: 1,
    test_question_id: 9,
    question_title: '鹈鹕骑自行车',
    account_id: 3,
    account_name: 'grok-账号',
    platform: 'grok',
    model: 'grok-4.7',
    reasoning_effort: 'high',
    status: 'success',
    error_message: '',
    has_text: true,
    response_truncated: false,
    image_count: 1,
    latency_ms: 1234,
    started_at: '2026-10-21T12:00:00Z',
    finished_at: '2026-10-21T12:00:01Z',
    created_at: '2026-10-21T12:00:01Z',
    ...overrides
  }
}

function mountModal() {
  return mount(TestRecordHistoryModal, {
    props: { show: true, question: question() },
    global: {
      stubs: {
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Pagination: PaginationStub,
        EmptyState: { template: '<div class="empty-stub"></div>' },
        Icon: true
      }
    }
  })
}

describe('测试历史弹框', () => {
  beforeEach(() => {
    listByQuestion.mockReset().mockResolvedValue({
      items: [
        record({ id: 2, account_name: '成功账号', image_count: 1 }),
        record({
          id: 1,
          account_name: '失败账号',
          status: 'failed',
          error_message: '上游 502',
          has_text: false,
          image_count: 0
        })
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    get.mockReset().mockResolvedValue(
      record({
        id: 2,
        response_text: '```svg\n<svg></svg>\n```',
        files: [{ id: 10, kind: 'image', mime_type: 'image/png', size_bytes: 3 }]
      })
    )
    fetchFile.mockReset().mockResolvedValue(new Blob(['img']))
    showError.mockReset()
    copyToClipboard.mockReset()
    Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(() => 'blob:mock'), configurable: true })
    Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true })
  })

  it('打开时按题目倒序加载第一页并渲染结果按钮', async () => {
    const wrapper = mountModal()
    await flushPromises()

    expect(listByQuestion).toHaveBeenCalledWith(9, 1, 20)
    expect(wrapper.text()).toContain('成功账号')
    expect(wrapper.text()).toContain('失败账号')
    expect(wrapper.findAll('.record-row')).toHaveLength(2)

    const titles = wrapper.findAll('button').map((b) => b.attributes('title'))
    expect(titles).toContain('admin.testQuestions.history.viewText')
    expect(titles).toContain('admin.testQuestions.history.viewImages')
    expect(titles).toContain('admin.testQuestions.history.downloadFile')
    expect(titles).toContain('admin.testQuestions.history.viewError')
  })

  it('翻页时按新页码重新请求', async () => {
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.find('.next-page').trigger('click')
    await flushPromises()

    expect(listByQuestion).toHaveBeenLastCalledWith(9, 2, 20)
  })

  it('点击查看文本打开详情并可复制与下载提取文件', async () => {
    const wrapper = mountModal()
    await flushPromises()

    const viewText = wrapper
      .findAll('button')
      .find((b) => b.attributes('title') === 'admin.testQuestions.history.viewText')
    await viewText!.trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(2)
    expect(wrapper.find('.detail-text').text()).toContain('<svg>')

    await wrapper.find('.copy-text').trigger('click')
    expect(copyToClipboard).toHaveBeenCalled()

    // 下载是否真的发生：内部会创建 <a> 并调用 link.click()，只有走到下载逻辑才会触发
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await wrapper.find('.detail-download').trigger('click')
    expect(clickSpy).toHaveBeenCalled()
    clickSpy.mockRestore()
  })

  it('点击查看错误展示错误文本', async () => {
    get.mockResolvedValueOnce(
      record({ status: 'failed', error_message: '上游 502', response_text: '' })
    )
    const wrapper = mountModal()
    await flushPromises()

    const viewError = wrapper
      .findAll('button')
      .find((b) => b.attributes('title') === 'admin.testQuestions.history.viewError')
    await viewError!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.detail-text').text()).toContain('上游 502')
  })

  it('点击查看图片加载结果文件', async () => {
    const wrapper = mountModal()
    await flushPromises()

    const viewImages = wrapper
      .findAll('button')
      .find((b) => b.attributes('title') === 'admin.testQuestions.history.viewImages')
    await viewImages!.trigger('click')
    await flushPromises()

    expect(fetchFile).toHaveBeenCalledWith(2, 10)
    expect(wrapper.find('.detail-images img').exists()).toBe(true)
  })

  it('没有记录时展示空态', async () => {
    listByQuestion.mockResolvedValueOnce({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.find('.empty-stub').exists()).toBe(true)
  })

  it('加载失败时展示错误并可重试', async () => {
    listByQuestion.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountModal()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.testQuestions.history.loadFailed')
    expect(wrapper.text()).toContain('admin.testQuestions.history.loadFailed')
    expect(listByQuestion).toHaveBeenCalledTimes(1)

    // 点击错误横幅里的重试按钮：mockRejectedValueOnce 只失败一次，重试应拿到成功数据
    const retryButton = wrapper
      .findAll('button')
      .find((b) => b.text() === 'admin.testQuestions.retry')
    expect(retryButton).toBeTruthy()
    await retryButton!.trigger('click')
    await flushPromises()

    expect(listByQuestion).toHaveBeenCalledTimes(2)
    expect(listByQuestion).toHaveBeenLastCalledWith(9, 1, 20)
    expect(wrapper.findAll('.record-row')).toHaveLength(2)
    expect(wrapper.find('.empty-stub').exists()).toBe(false)
  })
})
