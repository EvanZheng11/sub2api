/**
 * 普通用户测试中心 API
 * 题库只读 + 新建、题目测试、测试历史；账号范围由后端限制在用户可用分组内。
 */

import { apiClient, buildApiUrl } from './client'
import type { TestQuestion, TestQuestionPayload } from './admin/testQuestions'
import type { TestRecordPage, TestRecord } from './admin/testRecords'
import type { ClaudeModel } from '@/types'

export interface TestCenterAccount {
  id: number
  name: string
  platform: string
  type: string
  status: string
}

/** 查询题库全部题目 */
export async function listQuestions(): Promise<TestQuestion[]> {
  const { data } = await apiClient.get<TestQuestion[]>('/test-center/questions')
  return data ?? []
}

/** 新建题目 */
export async function createQuestion(payload: TestQuestionPayload): Promise<TestQuestion> {
  const { data } = await apiClient.post<TestQuestion>('/test-center/questions', payload)
  return data
}

/** 查询可选账号（用户可用分组内的活跃账号） */
export async function listAccounts(): Promise<TestCenterAccount[]> {
  const { data } = await apiClient.get<TestCenterAccount[]>('/test-center/accounts')
  return data ?? []
}

/** 查询账号可测试的模型 */
export async function listAccountModels(accountId: number): Promise<ClaudeModel[]> {
  const { data } = await apiClient.get<ClaudeModel[]>(`/test-center/accounts/${accountId}/models`)
  return data ?? []
}

/** 分页查询某道题目的测试历史 */
export async function listRecords(
  questionId: number,
  page = 1,
  pageSize = 20
): Promise<TestRecordPage> {
  const { data } = await apiClient.get<TestRecordPage>(
    `/test-center/questions/${questionId}/records`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

/** 查询单条测试记录详情 */
export async function getRecord(id: number): Promise<TestRecord> {
  const { data } = await apiClient.get<TestRecord>(`/test-center/records/${id}`)
  return data
}

/** 读取测试结果文件二进制内容 */
export async function fetchRecordFile(recordId: number, fileId: number): Promise<Blob> {
  const response = await apiClient.get(`/test-center/records/${recordId}/files/${fileId}`, {
    responseType: 'blob'
  })
  return response.data as Blob
}

/** 题目测试的 SSE 接口地址 */
export function buildTestUrl(accountId: number): string {
  return buildApiUrl(`/test-center/accounts/${accountId}/test`)
}

export const testCenterAPI = {
  listQuestions,
  createQuestion,
  listAccounts,
  listAccountModels,
  listRecords,
  getRecord,
  fetchRecordFile,
  buildTestUrl
}
