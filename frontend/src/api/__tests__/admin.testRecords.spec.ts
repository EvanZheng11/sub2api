import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { fetchFile, get as getRecord, listByQuestion } from '@/api/admin/testRecords'

describe('admin 测试历史 API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('分页查询题目测试历史：路径与分页参数正确', async () => {
    const page = { items: [], total: 0, page: 2, page_size: 50, pages: 0 }
    get.mockResolvedValue({ data: page })
    await expect(listByQuestion(9, 2, 50)).resolves.toBe(page)
    expect(get).toHaveBeenCalledWith('/admin/test-questions/9/records', {
      params: { page: 2, page_size: 50 }
    })
  })

  it('查询单条记录详情：路径正确', async () => {
    const record = { id: 3 }
    get.mockResolvedValue({ data: record })
    await expect(getRecord(3)).resolves.toBe(record)
    expect(get).toHaveBeenCalledWith('/admin/test-records/3')
  })

  it('读取结果文件：路径正确且以 blob 方式请求', async () => {
    const blob = new Blob(['x'])
    get.mockResolvedValue({ data: blob })
    await expect(fetchFile(3, 10)).resolves.toBe(blob)
    expect(get).toHaveBeenCalledWith('/admin/test-records/3/files/10', { responseType: 'blob' })
  })
})
