-- 账号测试历史记录：题目维度的测试结果。
-- 文本结果入库，图片/音频/视频由应用层落盘，库中只存相对路径。
-- 保留条数由应用层在每次入库后裁剪（每题保留最近 N 条），本迁移不做清理。
CREATE TABLE IF NOT EXISTS account_test_records (
    id BIGSERIAL PRIMARY KEY,
    test_question_id BIGINT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    question_title VARCHAR(200) NOT NULL DEFAULT '',
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    account_name VARCHAR(200) NOT NULL DEFAULT '',
    platform VARCHAR(50) NOT NULL DEFAULT '',
    model VARCHAR(200) NOT NULL DEFAULT '',
    reasoning_effort VARCHAR(20) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'success',
    error_message TEXT NOT NULL DEFAULT '',
    response_text TEXT NOT NULL DEFAULT '',
    response_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    image_count INTEGER NOT NULL DEFAULT 0,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_test_records_status_check
        CHECK (status IN ('success', 'failed')),
    CONSTRAINT account_test_records_latency_check
        CHECK (latency_ms >= 0)
);

-- 列表固定按题目过滤并按 created_at desc, id desc 排序。
CREATE INDEX IF NOT EXISTS idx_account_test_records_question_created
    ON account_test_records (test_question_id, created_at DESC, id DESC);

-- 媒体文件元数据，二进制内容不落库。
CREATE TABLE IF NOT EXISTS account_test_record_files (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT NOT NULL REFERENCES account_test_records(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    mime_type VARCHAR(100) NOT NULL DEFAULT '',
    path VARCHAR(500) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_test_record_files_kind_check
        CHECK (kind IN ('image', 'audio', 'video'))
);

CREATE INDEX IF NOT EXISTS idx_account_test_record_files_record
    ON account_test_record_files (record_id);
