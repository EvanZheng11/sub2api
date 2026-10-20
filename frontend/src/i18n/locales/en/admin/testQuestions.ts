export default {
  testQuestions: {
    title: 'Test Question Bank',
    description: 'Maintain questions used by account connection tests, covering text, image and file generation.',
    searchPlaceholder: 'Search title or prompt',
    typeFilterAll: 'All types',
    createQuestion: 'New question',
    editQuestion: 'Edit question',
    deleteQuestion: 'Delete question',
    deleteConfirm: 'Delete this question? This action cannot be undone.',
    empty: 'No test questions yet',
    emptyHint: 'The bank is usually seeded by the backend, but you can also create questions here.',
    noMatch: 'No matching questions',
    noMatchHint: 'Try a different search keyword or type filter.',
    loadFailed: 'Failed to load test questions',
    saveFailed: 'Failed to save question',
    deleteFailed: 'Failed to delete question',
    created: 'Question created',
    updated: 'Question updated',
    deleted: 'Question deleted',
    titleRequired: 'Enter a question title',
    promptRequired: 'Enter a prompt',
    retry: 'Retry',
    columns: {
      title: 'Title',
      type: 'Type',
      prompt: 'Prompt',
      updatedAt: 'Updated at',
      actions: 'Actions'
    },
    typeLabels: {
      text: 'Text',
      image: 'Image',
      file: 'File'
    },
    typeHints: {
      text: 'Text questions verify a normal chat response.',
      image: 'Image questions need a real image-generation model; unsupported platforms show an explicit warning.',
      file: 'File questions expect fenced code in the reply, which can be previewed and downloaded after the test.'
    },
    form: {
      title: 'Title',
      titlePlaceholder: 'e.g. Basic connectivity test',
      type: 'Type',
      prompt: 'Prompt',
      promptPlaceholder: 'Enter the full prompt sent to the model'
    },
    history: {
      action: 'Test History',
      title: 'Test History',
      loadFailed: 'Failed to load test history',
      detailFailed: 'Failed to load test record detail',
      empty: 'No test records',
      emptyHint: 'This question has not been used for an account test yet.',
      columns: {
        createdAt: 'Recorded At',
        account: 'Account',
        model: 'Model',
        reasoningEffort: 'Reasoning Effort',
        latency: 'Duration',
        status: 'Status',
        result: 'Result'
      },
      status: {
        success: 'Success',
        failed: 'Failed'
      },
      viewText: 'View Text',
      viewImages: 'View Images',
      downloadFile: 'Download File',
      viewError: 'View Error',
      truncated: 'Content is long, only the first 64KB is kept.',
      noArtifact: 'No downloadable file was found in the response.',
      textResult: 'Text Result',
      errorDetail: 'Error Detail',
      imagesResult: 'Images',
      downloadFailed: 'Failed to download the result',
      imageLoadFailed: 'Failed to load the image'
    }
  }
}
