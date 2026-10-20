import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import TestQuestionsView from '../TestQuestionsView.vue'

const { list, create, update, remove, showSuccess, showError } = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    testQuestions: {
      list,
      create,
      update,
      remove
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] }
  },
  setup(props, { slots }) {
    return () =>
      h('div', [
        ...(props.data.length === 0
          ? [slots.empty?.()]
          : (props.data as Array<Record<string, unknown>>).map((row) =>
              h('div', { class: 'question-row' }, [
                h('span', { class: 'question-title' }, String(row.title)),
                slots['cell-actions']?.({ row })
              ])
            ))
      ])
  }
})

const BaseDialogStub = defineComponent({
  props: {
    show: Boolean,
    title: String
  },
  setup(props, { slots }) {
    return () =>
      props.show
        ? h('div', { class: 'dialog-stub' }, [slots.default?.(), slots.footer?.()])
        : null
  }
})

const ConfirmDialogStub = defineComponent({
  props: {
    show: Boolean
  },
  emits: ['confirm', 'cancel'],
  setup(props, { emit }) {
    return () =>
      props.show
        ? h('button', { class: 'confirm-delete', onClick: () => emit('confirm') }, 'confirm')
        : null
  }
})

function question(id: number, title: string) {
  return {
    id,
    title,
    prompt: `${title} prompt`,
    type: 'text' as const,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z'
  }
}

function mountView() {
  return mount(TestQuestionsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        Select: { template: '<div class="select-stub"></div>' },
        EmptyState: { template: '<div class="empty-stub"></div>' },
        Icon: true,
        TestRecordHistoryModal: {
          props: { show: Boolean, question: Object },
          emits: ['close'],
          template: '<div v-if="show" class="history-modal-stub">{{ question ? question.id : "" }}</div>'
        }
      }
    }
  })
}

describe('管理员测试题库页面', () => {
  beforeEach(() => {
    list.mockResolvedValue([question(1, '基础连通性'), question(2, '文件输出')])
    create.mockReset().mockResolvedValue(question(3, '新题目'))
    update.mockReset().mockResolvedValue(question(1, '更新后的题目'))
    remove.mockReset().mockResolvedValue(undefined)
    showSuccess.mockReset()
    showError.mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('加载题库并创建题目时提交完整请求体', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(list).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('基础连通性')

    const createButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.testQuestions.createQuestion'))
    expect(createButton).toBeTruthy()
    await createButton!.trigger('click')

    await wrapper.find('#test-question-title').setValue('新题目')
    await wrapper.find('#test-question-prompt').setValue('请输出一个 JSON 对象')
    await wrapper.find('form#test-question-form').trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledWith({
      title: '新题目',
      prompt: '请输出一个 JSON 对象',
      type: 'text'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.testQuestions.created')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('编辑与删除题目会调用对应 API', async () => {
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper
      .findAll('button')
      .find((button) => button.attributes('title') === 'common.edit')
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await wrapper.find('#test-question-title').setValue('更新后的题目')
    await wrapper.find('form#test-question-form').trigger('submit')
    await flushPromises()

    expect(update).toHaveBeenCalledWith(1, {
      title: '更新后的题目',
      prompt: '基础连通性 prompt',
      type: 'text'
    })

    const deleteButton = wrapper
      .findAll('button')
      .find((button) => button.attributes('title') === 'common.delete')
    expect(deleteButton).toBeTruthy()
    await deleteButton!.trigger('click')
    await wrapper.find('.confirm-delete').trigger('click')
    await flushPromises()

    expect(remove).toHaveBeenCalledWith(1)
    expect(showSuccess).toHaveBeenCalledWith('admin.testQuestions.deleted')
  })

  it('加载失败时展示错误并允许重试', async () => {
    list.mockRejectedValueOnce(new Error('network down'))
    const wrapper = mountView()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.testQuestions.loadFailed')
    expect(wrapper.text()).toContain('admin.testQuestions.loadFailed')

    list.mockResolvedValueOnce([question(1, '恢复后的题目')])
    const retryButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.testQuestions.retry'))
    expect(retryButton).toBeTruthy()
    await retryButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('恢复后的题目')
  })

  it('操作列的测试历史按钮打开历史弹框并带上当前题目', async () => {
    const wrapper = mountView()
    await flushPromises()

    const historyButton = wrapper
      .findAll('button')
      .find((button) => button.attributes('title') === 'admin.testQuestions.history.action')
    expect(historyButton).toBeTruthy()
    await historyButton!.trigger('click')
    await flushPromises()

    const modal = wrapper.find('.history-modal-stub')
    expect(modal.exists()).toBe(true)
    expect(modal.text()).toBe('1')
  })
})
