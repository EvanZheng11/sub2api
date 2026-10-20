/**
 * 管理员测试历史 API
 * 记录由账号测试在服务端落库，本模块只做只读查询与结果文件读取。
 */

import { apiClient } from '../client'

export type TestRecordStatus = 'success' | 'failed'
export type TestRecordFileKind = 'image' | 'audio' | 'video'

export interface TestRecordFile {
  id: number
  kind: TestRecordFileKind
  mime_type: string
  size_bytes: number
}

export interface TestRecord {
  id: number
  test_question_id: number
  question_title: string
  account_id: number | null
  account_name: string
  platform: string
  model: string
  reasoning_effort: string
  status: TestRecordStatus
  error_message: string
  response_text?: string
  has_text: boolean
  response_truncated: boolean
  image_count: number
  latency_ms: number
  started_at: string
  finished_at: string
  created_at: string
  files?: TestRecordFile[]
}

export interface TestRecordPage {
  items: TestRecord[]
  total: number
  page: number
  page_size: number
  pages: number
}

/** 分页查询某道题目的测试历史（按时间倒序） */
export async function listByQuestion(
  questionId: number,
  page = 1,
  pageSize = 20
): Promise<TestRecordPage> {
  const { data } = await apiClient.get<TestRecordPage>(
    `/admin/test-questions/${questionId}/records`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

/** 查询单条测试记录详情（含完整文本与文件元数据） */
export async function get(id: number): Promise<TestRecord> {
  const { data } = await apiClient.get<TestRecord>(`/admin/test-records/${id}`)
  return data
}

/** 读取结果文件二进制内容 */
export async function fetchFile(recordId: number, fileId: number): Promise<Blob> {
  const response = await apiClient.get(`/admin/test-records/${recordId}/files/${fileId}`, {
    responseType: 'blob'
  })
  return response.data as Blob
}

export const testRecordsAPI = {
  listByQuestion,
  get,
  fetchFile
}
