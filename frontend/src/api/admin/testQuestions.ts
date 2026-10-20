/**
 * 管理员题库 API
 * 用于账号测试时选择题目，题目由后端种子数据初始化。
 */

import { apiClient } from '../client'

export type TestQuestionType = 'text' | 'image' | 'file'

export interface TestQuestion {
  id: number
  title: string
  prompt: string
  type: TestQuestionType
  created_at: string
  updated_at: string
}

export interface TestQuestionPayload {
  title: string
  prompt: string
  type: TestQuestionType
}

/** 查询全部测试题目 */
export async function list(): Promise<TestQuestion[]> {
  const { data } = await apiClient.get<TestQuestion[]>('/admin/test-questions')
  return data ?? []
}

/** 新建测试题目 */
export async function create(payload: TestQuestionPayload): Promise<TestQuestion> {
  const { data } = await apiClient.post<TestQuestion>('/admin/test-questions', payload)
  return data
}

/** 更新测试题目 */
export async function update(id: number, payload: TestQuestionPayload): Promise<TestQuestion> {
  const { data } = await apiClient.put<TestQuestion>(`/admin/test-questions/${id}`, payload)
  return data
}

/** 删除测试题目 */
export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/test-questions/${id}`)
}

export const testQuestionsAPI = {
  list,
  create,
  update,
  remove
}
