-- 题库：管理员维护的测试题目，独立表，不影响账号相关结构与数据。
-- 类型约束与标题/提示词非空约束在数据库层兜底，应用层另有 trim 与长度校验。
CREATE TABLE IF NOT EXISTS test_questions (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    prompt TEXT NOT NULL,
    type VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT test_questions_type_check
        CHECK (type IN ('text', 'image', 'file')),
    CONSTRAINT test_questions_title_not_blank
        CHECK (btrim(title) <> ''),
    CONSTRAINT test_questions_prompt_not_blank
        CHECK (btrim(prompt) <> '')
);

-- 列表默认按 updated_at desc, id desc 排序。
CREATE INDEX IF NOT EXISTS idx_test_questions_updated_at
    ON test_questions (updated_at DESC, id DESC);

-- 默认题目只在迁移时插入一次；迁移由 schema_migrations 校验和记录，不会在每次启动重复执行，
-- 因此不会覆盖已有题目，也不会复活管理员已删除的题目。
INSERT INTO test_questions (title, prompt, type)
SELECT '鹈鹕骑自行车 SVG 动画',
       '不使用技能、不测试，一次性生成一段高质量的 SVG 代码，绘制一只正在骑自行车的鹈鹕的动画，画面美观且结构清晰。',
       'file'
WHERE NOT EXISTS (SELECT 1 FROM test_questions);
