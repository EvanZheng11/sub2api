export default {
  testQuestions: {
    title: '测试题库',
    description: '维护账号连接测试使用的题目，支持文本、图片和文件生成题。',
    searchPlaceholder: '搜索标题或提示词',
    typeFilterAll: '全部类型',
    createQuestion: '新建题目',
    editQuestion: '编辑题目',
    deleteQuestion: '删除题目',
    deleteConfirm: '确定删除该题目吗？此操作不可恢复。',
    empty: '暂无测试题目',
    emptyHint: '题库通常由后端种子数据初始化，也可以在此手动创建。',
    noMatch: '没有匹配的题目',
    noMatchHint: '请调整搜索关键词或类型筛选。',
    loadFailed: '加载测试题库失败',
    saveFailed: '保存题目失败',
    deleteFailed: '删除题目失败',
    created: '题目已创建',
    updated: '题目已更新',
    deleted: '题目已删除',
    titleRequired: '请输入题目标题',
    promptRequired: '请输入提示词',
    retry: '重试',
    columns: {
      title: '标题',
      type: '类型',
      prompt: '提示词',
      updatedAt: '更新时间',
      actions: '操作'
    },
    typeLabels: {
      text: '文本',
      image: '图片',
      file: '文件'
    },
    typeHints: {
      text: '文本题用于验证普通对话回复。',
      image: '图片题需要选择真实图片生成模型，不支持生图的平台会明确提示。',
      file: '文件题要求模型返回代码块，测试后可预览并下载提取出的文件。'
    },
    form: {
      title: '标题',
      titlePlaceholder: '例如：基础连通性测试',
      type: '类型',
      prompt: '提示词',
      promptPlaceholder: '填写发送给模型的完整提示词'
    },
    history: {
      action: '测试历史',
      title: '测试历史',
      loadFailed: '加载测试历史失败',
      detailFailed: '加载测试记录详情失败',
      empty: '暂无测试记录',
      emptyHint: '这道题目还没有执行过账号测试。',
      columns: {
        createdAt: '记录时间',
        account: '账号',
        model: '模型',
        reasoningEffort: '思考强度',
        latency: '测试时长',
        status: '状态',
        result: '结果'
      },
      status: {
        success: '成功',
        failed: '失败'
      },
      viewResult: '查看结果',
      viewText: '查看文本',
      viewImages: '查看图片',
      downloadFile: '下载文件',
      viewError: '查看错误',
      truncated: '内容较长，仅保留前 64KB。',
      noArtifact: '未从响应中提取到可下载文件。',
      textResult: '文本结果',
      errorDetail: '错误详情',
      imagesResult: '图片结果',
      downloadFailed: '下载结果失败',
      imageLoadFailed: '加载图片失败',
      mediaFiles: '结果文件',
      downloadMedia: '下载',
      mediaDownloadFailed: '下载结果文件失败'
    }
  }
}
