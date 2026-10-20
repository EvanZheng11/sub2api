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
    }
  }
}
